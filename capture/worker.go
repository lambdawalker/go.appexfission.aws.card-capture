package capture

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"image"
	"image/jpeg"
	"log/slog"
	"time"
)

func ValidateJPEG(data []byte, a Asset) error {
	_, err := decodeValidatedJPEG(data, a)
	return err
}

func decodeValidatedJPEG(data []byte, a Asset) (image.Image, error) {
	if int64(len(data)) != a.Size || a.Size > MaxBytes {
		return nil, ErrImage
	}
	sum := sha256.Sum256(data)
	if base64.StdEncoding.EncodeToString(sum[:]) != a.SHA256 {
		return nil, ErrImage
	}
	cfg, e := jpeg.DecodeConfig(bytes.NewReader(data))
	if e != nil || cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width)*int64(cfg.Height) > MaxPixels {
		return nil, ErrImage
	}
	img, e := jpeg.Decode(bytes.NewReader(data))
	if e != nil {
		return nil, ErrImage
	}
	return img, nil
}

// Work is restart-safe. Revision/generation fences stop old workers publishing after cancellation.
func (s Service) Work(ctx context.Context, job Job) (err error) {
	started := time.Now()
	slog.Info("capture.work_started", "capture_id", job.ID, "work", job.Work, "generation", job.Generation)
	defer func() {
		// Error text can include storage paths or provider output; only log success/failure.
		slog.Info("capture.work_finished", "capture_id", job.ID, "work", job.Work, "failed", err != nil, "elapsed_ms", time.Since(started).Milliseconds())
	}()
	r, e := s.Store.Get(ctx, job.Owner, job.ID)
	if e == ErrNotFound {
		return nil
	}
	if e != nil {
		return e
	}
	if job.Work == "front-ocr" {
		return s.workFrontOCR(ctx, r, job.Generation)
	}
	if r.Generation != job.Generation || r.Work != job.Work {
		return nil
	}
	now := s.now().Unix()
	slog.Info("capture.work_state", "capture_id", r.ID, "state", r.State, "revision", r.Revision, "due", r.Due, "lease_until", r.LeaseUntil, "extraction_attempts", r.ExtractionAttempts)
	if r.Due > now || r.LeaseUntil > now {
		return nil
	}
	if r.Work == "extract" {
		return s.extract(ctx, r)
	}
	if r.Work == "cleanup" {
		// Uploading sessions are expired by time, ready evidence only after its retention deadline.
		if r.State == "uploading" && now < r.ExpiresAt {
			return nil
		}
		before := r
		r.FrontOCRAsset, r.FrontOCRDue = nil, 0
		r.Extraction = nil
		r.Reviewed = nil
		r.State = "expired"
		r.Generation++
		r.Revision++
		r.LeaseUntil = now + 120
		if e = s.Store.Swap(ctx, before, r, false); e != nil {
			return e
		}
		if e = s.Objects.DeleteCapture(ctx, Prefix(r.Owner, r.ID)); e != nil {
			return e
		}
		before = r
		r.Uploads = nil
		r.Assets = nil
		r.Processed = nil
		r.Slots = map[string]string{}
		r.Work = ""
		r.Due = 0
		r.LeaseUntil = 0
		r.Revision++
		return s.Store.Swap(ctx, before, r, false)
	}
	if r.State != "finalizing" {
		return nil
	}
	// Never promote evidence after the session lifetime; this also bounds pinned
	// evidence retention relative to the bucket's ten-day orphan safety lifecycle.
	if now >= r.ExpiresAt {
		before := r
		r.FrontOCRAsset, r.FrontOCRDue = nil, 0
		r.Extraction = nil
		r.Reviewed = nil
		r.State = "expired"
		r.Work = "cleanup"
		r.Due = now + int64((UploadLifetime + time.Minute).Seconds())
		r.Generation++
		r.Revision++
		r.LeaseUntil = 0
		return s.Store.Swap(ctx, before, r, false)
	}
	if r.Attempts >= 3 {
		before := r
		r.State = "failed"
		r.Error = "validation_unavailable"
		r.Work = "cleanup"
		r.Due = r.ExpiresAt + int64(UploadLifetime.Seconds())
		r.LeaseUntil = 0
		r.Revision++
		return s.Store.Swap(ctx, before, r, false)
	}
	before := r
	r.Revision++
	r.Attempts++
	r.LeaseUntil = now + 120
	r.Due = r.LeaseUntil
	if e = s.Store.Swap(ctx, before, r, false); e != nil {
		return e
	}
	validationErr := error(nil)
	processed := []Asset{}
	if len(r.Assets) != 2 {
		validationErr = ErrImage
	}
	for _, a := range r.Assets {
		var b []byte
		b, e = s.Objects.Read(ctx, a)
		if e != nil {
			validationErr = e
			break
		}
		img, err := decodeValidatedJPEG(b, a)
		clear(b)
		if err != nil {
			validationErr = err
			break
		}
		var output bytes.Buffer
		e = jpeg.Encode(&output, img, &jpeg.Options{Quality: 90})
		if e != nil || int64(output.Len()) > MaxBytes {
			clear(output.Bytes())
			validationErr = ErrImage
			break
		}
		// Decode/re-encode strips EXIF and other metadata; Android supplies oriented pixels.
		normalized, e := s.Objects.Write(ctx, r, a.Slot, output.Bytes())
		clear(output.Bytes())
		if e != nil {
			validationErr = e
			break
		}
		processed = append(processed, normalized)
	}
	before = r
	r.Revision++
	r.LeaseUntil = 0
	if validationErr == nil && s.now().Unix() >= r.ExpiresAt {
		r.FrontOCRAsset, r.FrontOCRDue = nil, 0
		r.Extraction = nil
		r.Reviewed = nil
		r.State = "expired"
		r.Work = "cleanup"
		r.Due = s.now().Add(UploadLifetime + time.Minute).Unix()
		r.Generation++
		return s.Store.Swap(ctx, before, r, false)
	}
	if validationErr == nil {
		r.Processed = processed
		r.ProcessingVersion = "jpeg-raster-v1"
		r.State = "ready"
		r.Work = "cleanup"
		retention := s.Retention
		if retention <= 0 {
			retention = 7 * 24 * time.Hour
		}
		r.DeleteAfter = s.now().Add(retention).Unix()
		r.Due = r.DeleteAfter
		if s.Extractor != nil && r.FrontOCRAsset == nil {
			r.Extraction = &Extraction{Status: "pending"}
			r.Work = "extract"
			r.Due = s.now().Unix()
		}
		return s.finishValidation(ctx, before, r, true)
	}
	if validationErr == ErrImage {
		r.State = "requires_recapture"
		r.Error = "invalid_image"
		r.Work = "cleanup"
		r.Due = s.now().Add(UploadLifetime + time.Minute).Unix()
	} else if r.Attempts >= 3 {
		r.State = "failed"
		r.Error = "validation_unavailable"
		r.Work = "cleanup"
		r.Due = r.ExpiresAt + int64(UploadLifetime.Seconds())
	} else {
		r.Due = s.now().Add(time.Duration(r.Attempts) * time.Minute).Unix()
	}
	if r.State != "finalizing" {
		r.FrontOCRAsset, r.FrontOCRDue, r.Extraction = nil, 0, nil
	}
	return s.finishValidation(ctx, before, r, false)
}

// A front OCR update may land while the validation lease is active. Preserve
// the OCR lane while committing the validation result for the same image pair.
func (s Service) finishValidation(ctx context.Context, before, next Record, ready bool) error {
	for {
		err := s.Store.Swap(ctx, before, next, ready)
		if err != ErrConflict || before.FrontOCRAsset == nil {
			return err
		}
		current, err := s.Store.Get(ctx, before.Owner, before.ID)
		if err != nil {
			return err
		}
		if current.Generation != before.Generation || current.State != before.State ||
			current.LeaseUntil != before.LeaseUntil {
			return ErrConflict
		}
		before = current
		next.Revision = current.Revision + 1
		if next.FrontOCRAsset != nil {
			next.Extraction = current.Extraction
			next.ExtractionAttempts = current.ExtractionAttempts
			next.FrontOCRDue = current.FrontOCRDue
		}
	}
}
