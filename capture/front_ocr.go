package capture

import (
	"context"
	"log/slog"
	"time"
)

func startFrontOCR(r *Record, asset Asset, now int64) {
	r.FrontOCRAsset = &asset
	r.FrontOCRDue = now
	r.FrontOCRGeneration = r.Revision + 1
	r.Extraction = &Extraction{Status: "pending"}
	r.ExtractionAttempts = 0
}

// ConfirmFront pins the selected upload after its PUT succeeds. Finalization
// independently checks the evidence pair and can run while OCR is in flight.
func (s Service) ConfirmFront(ctx context.Context, owner, id, uploadID string) (Record, error) {
	r, err := s.Get(ctx, owner, id)
	if err != nil {
		return r, err
	}
	if uploadID == "" || r.Slots["front"] != uploadID {
		return r, ErrConflict
	}
	if r.FrontOCRAsset != nil && r.FrontOCRAsset.UploadID == uploadID {
		return r, nil
	}
	if r.State != "uploading" || s.Extractor == nil {
		return r, ErrConflict
	}
	for _, u := range r.Uploads {
		if u.ID != uploadID {
			continue
		}
		asset, err := s.Objects.Head(ctx, u)
		if err != nil {
			return r, err
		}
		if asset.VersionID == "" || asset.VersionID == "null" || asset.Size != u.Size || asset.SHA256 != u.SHA256 {
			return r, ErrImage
		}
		asset.UploadID, asset.Slot, asset.Key = u.ID, u.Slot, u.Key
		before := r
		startFrontOCR(&r, asset, s.now().Unix())
		r.Revision++
		return r, s.Store.Swap(ctx, before, r, false)
	}
	return r, ErrIncomplete
}

func (r Record) frontOCRCurrent(generation, now int64) bool {
	return r.FrontOCRGeneration == generation && r.FrontOCRAsset != nil &&
		r.FrontOCRAsset.UploadID == r.Slots["front"] && r.FrontOCRDue != 0 &&
		((now < r.ExpiresAt && (r.State == "uploading" || r.State == "finalizing")) ||
			(now < r.DeleteAfter && r.State == "ready"))
}

func (s Service) workFrontOCR(ctx context.Context, r Record, generation int64) error {
	var before Record
	for {
		now := s.now().Unix()
		if r.FrontOCRGeneration != generation || r.FrontOCRDue == 0 || r.FrontOCRDue > now {
			return nil
		}
		if !r.frontOCRCurrent(generation, now) {
			before := r
			r.Revision++
			r.FrontOCRAsset, r.FrontOCRDue, r.Extraction = nil, 0, nil
			return s.Store.Swap(ctx, before, r, false)
		}
		if r.Extraction == nil {
			return nil
		}
		before = r
		r.Revision++
		// Due doubles as the lease deadline while an attempt is running.
		r.FrontOCRDue = now + 120
		if r.ExtractionAttempts < 2 {
			r.ExtractionAttempts++
		}
		if before.ExtractionAttempts == 1 && !r.Extraction.Retrying {
			r.Extraction = &Extraction{Status: "pending", Retrying: true}
		}
		switch err := s.Store.Swap(ctx, before, r, false); err {
		case ErrConflict:
			r, err = s.Store.Get(ctx, r.Owner, r.ID)
			if err != nil {
				return err
			}
			continue
		case nil:
		default:
			return err
		}
		break
	}
	attempt := r.ExtractionAttempts
	var data CardData
	extractionErr := ErrInvalid
	// An expired second-attempt lease is terminal. An expired first-attempt
	// lease permits the second attempt and exposes manual review immediately.
	if before.ExtractionAttempts < 2 {
		data, extractionErr = s.readFront(ctx, *r.FrontOCRAsset)
	}
	slog.Info("capture.extraction_result", "capture_id", r.ID, "attempt", attempt, "failed", extractionErr != nil)
	// Upload registration, validation, and manual review may all advance the
	// record revision while inference is running. Merge only into our lease.
	for {
		current, err := s.Store.Get(ctx, r.Owner, r.ID)
		if err != nil {
			return err
		}
		now := s.now().Unix()
		if !current.frontOCRCurrent(generation, now) ||
			current.FrontOCRDue != r.FrontOCRDue || current.ExtractionAttempts != attempt {
			return nil
		}
		next := current
		next.Revision++
		next.FrontOCRDue = 0
		switch {
		case extractionErr == nil:
			next.Extraction = &Extraction{Status: "complete", Data: data}
		case attempt == 2:
			next.Extraction = &Extraction{Status: "failed", Error: "extraction_unavailable"}
		default:
			next.Extraction = &Extraction{Status: "pending", Retrying: true}
			next.FrontOCRDue = now
		}
		if err := s.Store.Swap(ctx, current, next, false); err != ErrConflict {
			return err
		}
	}
}

func (s Service) readFront(ctx context.Context, asset Asset) (CardData, error) {
	jpeg, err := s.Objects.Read(ctx, asset)
	defer clear(jpeg)
	if err != nil {
		return CardData{}, err
	}
	if err := ValidateJPEG(jpeg, asset); err != nil {
		return CardData{}, err
	}
	if s.Extractor == nil {
		return CardData{}, ErrDisabled
	}
	bounded, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	data, err := s.Extractor.Extract(bounded, []CardImage{{Slot: "front", JPEG: jpeg}})
	if err == nil && (!data.Valid() || data.Empty()) {
		err = ErrInvalid
	}
	return data, err
}
