package capture

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"testing"
	"time"
)

type extractorFunc func(context.Context, []CardImage) (CardData, error)

func (f extractorFunc) Extract(ctx context.Context, images []CardImage) (CardData, error) {
	return f(ctx, images)
}
func extractionFixture(t *testing.T) (Service, *memoryStore, *memoryObjects, *time.Time, Record) {
	s, m, o, now := fixture(t)
	s.Extractor = extractorFunc(func(_ context.Context, images []CardImage) (CardData, error) {
		if len(images) != 2 || images[0].Slot != "front" || images[1].Slot != "back" {
			t.Fatal("missing ordered images")
		}
		return CardData{FullName: "Test Person", DocumentNumber: "000123"}, nil
	})
	r := freeze(t, s, o)
	if e := s.Work(context.Background(), Job{r.Owner, r.ID, r.Generation, r.Work}); e != nil {
		t.Fatal(e)
	}
	return s, m, o, now, m.records[r.ID]
}
func TestExtractionPublishesSeparateDataAndReview(t *testing.T) {
	s, m, _, _, r := extractionFixture(t)
	if r.State != "ready" || r.Extraction == nil || r.Extraction.Status != "pending" || r.Work != "extract" {
		t.Fatal("extraction was not scheduled")
	}
	job := Job{r.Owner, r.ID, r.Generation, r.Work}
	if e := s.Work(context.Background(), job); e != nil {
		t.Fatal(e)
	}
	r = m.records[r.ID]
	if r.Extraction.Status != "complete" || r.Extraction.Data.DocumentNumber != "000123" || r.Reviewed != nil || m.events != 1 {
		t.Fatal("bad extraction result")
	}
	if e := s.Work(context.Background(), job); e != nil {
		t.Fatal(e)
	}
	data := r.Extraction.Data
	data.FullName = "Corrected Person"
	reviewed, e := s.Review(context.Background(), r.Owner, r.ID, ReviewRequest{Revision: r.Revision, Data: data})
	if e != nil || reviewed.Reviewed.Data.FullName != data.FullName || reviewed.Extraction.Data.FullName != "Test Person" {
		t.Fatal("review overwrote evidence", e)
	}
	if _, e = s.Review(context.Background(), "other", r.ID, ReviewRequest{Revision: r.Revision, Data: data}); e != ErrNotFound {
		t.Fatal("cross-owner review", e)
	}
	data.FullName = "Stale"
	if _, e = s.Review(context.Background(), r.Owner, r.ID, ReviewRequest{Revision: r.Revision, Data: data}); e != ErrConflict {
		t.Fatal("stale review", e)
	}
}
func TestExtractionCancellationFencesPublication(t *testing.T) {
	s, m, _, _, r := extractionFixture(t)
	s.Extractor = extractorFunc(func(context.Context, []CardImage) (CardData, error) {
		_, e := s.Cancel(context.Background(), r.Owner, r.ID)
		if e != nil {
			t.Fatal(e)
		}
		return CardData{FullName: "secret"}, nil
	})
	if e := s.Work(context.Background(), Job{r.Owner, r.ID, r.Generation, r.Work}); e != ErrConflict {
		t.Fatal("stale extraction not fenced", e)
	}
	if m.records[r.ID].Extraction != nil {
		t.Fatal("cancelled data exposed")
	}
}
func TestExtractionFailureRetryAndExpiry(t *testing.T) {
	s, m, _, now, r := extractionFixture(t)
	s.Extractor = extractorFunc(func(context.Context, []CardImage) (CardData, error) { return CardData{}, errors.New("provider secret") })
	for i := 0; i < 2; i++ {
		r = m.records[r.ID]
		*now = time.Unix(r.Due, 0)
		if e := s.Work(context.Background(), Job{r.Owner, r.ID, r.Generation, r.Work}); e != nil {
			t.Fatal(e)
		}
	}
	r = m.records[r.ID]
	if r.State != "ready" || r.Extraction.Status != "failed" || r.Extraction.Error != "extraction_unavailable" {
		t.Fatal("failure did not preserve images")
	}
	r, e := s.Retry(context.Background(), r.Owner, r.ID, "retry_extraction_1")
	if e != nil || r.Work != "extract" {
		t.Fatal("retry failed", e)
	}
	*now = time.Unix(r.DeleteAfter, 0)
	if e := s.Work(context.Background(), Job{r.Owner, r.ID, r.Generation, r.Work}); e != nil {
		t.Fatal(e)
	}
	if m.records[r.ID].Extraction != nil {
		t.Fatal("expired extraction retained")
	}
}

func TestExpiredAndCancelledFieldsNeverReturned(t *testing.T) {
	s, m, _, now, r := extractionFixture(t)
	if e := s.Work(context.Background(), Job{r.Owner, r.ID, r.Generation, r.Work}); e != nil {
		t.Fatal(e)
	}
	r = m.records[r.ID]
	r, e := s.Review(context.Background(), r.Owner, r.ID, ReviewRequest{Revision: r.Revision, Data: CardData{FullName: "Private"}})
	if e != nil {
		t.Fatal(e)
	}
	*now = time.Unix(r.DeleteAfter, 0)
	got, e := s.Get(context.Background(), r.Owner, r.ID)
	if e != nil || got.State != "expired" || got.Extraction != nil || got.Reviewed != nil {
		t.Fatal("expired PII returned", e)
	}
	if _, e = s.Review(context.Background(), r.Owner, r.ID, ReviewRequest{Revision: r.Revision, Data: CardData{FullName: "Other"}}); e != ErrExpired {
		t.Fatal("expired edit allowed", e)
	}
	if e = s.Work(context.Background(), Job{r.Owner, r.ID, r.Generation, r.Work}); e != nil {
		t.Fatal(e)
	}
	if m.records[r.ID].Reviewed != nil || m.records[r.ID].Extraction != nil {
		t.Fatal("cleanup retained fields")
	}
}

func TestOriginIsUserProvidedAndSeparateFromIssuer(t *testing.T) {
	s, m, _, _, r := extractionFixture(t)
	if e := s.Work(context.Background(), Job{r.Owner, r.ID, r.Generation, r.Work}); e != nil {
		t.Fatal(e)
	}
	r = m.records[r.ID]
	data := r.Extraction.Data
	data.IssuingCountry = "Canada"
	saved, e := s.Review(context.Background(), r.Owner, r.ID, ReviewRequest{Revision: r.Revision, Data: data, OriginCountry: "MX"})
	if e != nil || saved.Reviewed.OriginCountry != "MX" || saved.Reviewed.Data.IssuingCountry != "Canada" {
		t.Fatal(e)
	}
	if _, e = s.Review(context.Background(), r.Owner, r.ID, ReviewRequest{Revision: saved.Revision, Data: data, OriginCountry: "invalid"}); e != ErrInvalid {
		t.Fatal("invalid origin accepted")
	}
}

func TestFailedExtractionAllowsManualReview(t *testing.T) {
	s, m, _, _, r := extractionFixture(t)
	r.Extraction = &Extraction{Status: "failed", Error: "extraction_unavailable"}
	m.records[r.ID] = r
	input := ReviewRequest{Revision: r.Revision, Data: CardData{FullName: "Entered Manually", DocumentNumber: "000045"}, OriginCountry: "MX"}
	saved, err := s.Review(context.Background(), r.Owner, r.ID, input)
	if err != nil {
		t.Fatal("manual review rejected", err)
	}
	if saved.Reviewed == nil || saved.Reviewed.Data != input.Data || saved.Reviewed.OriginCountry != "MX" || saved.Extraction.Status != "failed" || !saved.Extraction.Data.Empty() {
		t.Fatal("manual review did not preserve extraction outcome")
	}
	replay, err := s.Review(context.Background(), r.Owner, r.ID, input)
	if err != nil || replay.Revision != saved.Revision {
		t.Fatal("lost response retry changed review", err)
	}
	input.Data.FullName = "Stale edit"
	if _, err = s.Review(context.Background(), r.Owner, r.ID, input); err != ErrConflict {
		t.Fatal("stale edit accepted", err)
	}
	if _, err = s.Review(context.Background(), "other", r.ID, input); err != ErrNotFound {
		t.Fatal("cross-owner review accepted", err)
	}
}

func TestManualReviewRequiresRetainedValidatedImagesAndTerminalExtraction(t *testing.T) {
	for _, tc := range []struct{ state, extraction string }{
		{"ready", "pending"}, {"ready", ""}, {"ready", "unknown"}, {"uploading", "failed"}, {"finalizing", "failed"}, {"failed", "failed"}, {"requires_recapture", "failed"}, {"cancelled", "failed"},
	} {
		t.Run(tc.state+"/"+tc.extraction, func(t *testing.T) {
			s, m, _, _, r := extractionFixture(t)
			r.State = tc.state
			r.Extraction = nil
			if tc.extraction != "" {
				r.Extraction = &Extraction{Status: tc.extraction}
			}
			m.records[r.ID] = r
			_, err := s.Review(context.Background(), r.Owner, r.ID, ReviewRequest{Revision: r.Revision, Data: CardData{FullName: "Manual"}})
			if err != ErrConflict {
				t.Fatal("review should be blocked", err)
			}
		})
	}
	s, m, _, now, r := extractionFixture(t)
	r.Extraction = &Extraction{Status: "failed"}
	m.records[r.ID] = r
	*now = time.Unix(r.DeleteAfter, 0)
	if _, err := s.Review(context.Background(), r.Owner, r.ID, ReviewRequest{Revision: r.Revision, Data: CardData{FullName: "Manual"}}); err != ErrExpired {
		t.Fatal("expired manual review accepted", err)
	}
}

func TestReadyWithoutExtractionCanBeReviewedManually(t *testing.T) {
	s, m, _, _, r := extractionFixture(t)
	r.Extraction = nil
	r.Work = "cleanup"
	r.Due = r.DeleteAfter
	m.records[r.ID] = r
	got, err := s.Get(context.Background(), r.Owner, r.ID)
	if err != nil || got.Extraction == nil || got.Extraction.Status != "failed" {
		t.Fatalf("ready evidence without an extraction job must open manual review: %+v %v", got.Extraction, err)
	}
	_, err = s.Review(context.Background(), r.Owner, r.ID, ReviewRequest{Revision: got.Revision, OriginCountry: "MX", Data: CardData{FullName: "Manual Person"}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOverdueExtractionBecomesManualReviewAndFencesWorker(t *testing.T) {
	s, m, _, now, r := extractionFixture(t)
	before := r
	*now = time.Unix(r.Due+301, 0)
	got, err := s.Get(context.Background(), r.Owner, r.ID)
	if err != nil || got.Extraction == nil || got.Extraction.Status != "failed" {
		t.Fatalf("abandoned extraction must terminate: %+v %v", got.Extraction, err)
	}
	if got.Work != "cleanup" || got.LeaseUntil != 0 || got.Revision <= before.Revision {
		t.Fatal("missing durable recovery/fence")
	}
	if err := s.finishExtraction(context.Background(), before, CardData{FullName: "Late"}, nil); err != ErrConflict {
		t.Fatal("late worker overwrote manual entry state", err)
	}
	if m.records[r.ID].Extraction.Status != "failed" {
		t.Fatal("not persisted")
	}
}

func TestActiveExtractionLeaseIsNotFailedByRead(t *testing.T) {
	s, m, _, now, r := extractionFixture(t)
	r.Due = now.Unix() - 400
	r.LeaseUntil = now.Unix() + 60
	m.records[r.ID] = r
	got, err := s.Get(context.Background(), r.Owner, r.ID)
	if err != nil || got.Extraction.Status != "pending" {
		t.Fatal("active extraction interrupted", err)
	}
}

type stitchedExtractor struct {
	extractorFunc
	stitched func([]byte) (CardData, error)
}

func (f stitchedExtractor) ExtractStitched(_ context.Context, jpeg []byte) (CardData, error) {
	return f.stitched(jpeg)
}

func TestStitchedCaptureRetainsBothAssetsAndUsesColorPairForOCR(t *testing.T) {
	s, m, o, _ := fixture(t)
	s.Extractor = stitchedExtractor{
		extractorFunc: func(context.Context, []CardImage) (CardData, error) {
			t.Fatal("stitched capture called the separate-side extractor")
			return CardData{}, nil
		},
		stitched: func(jpeg []byte) (CardData, error) {
			if len(jpeg) == 0 {
				t.Fatal("missing stitched JPEG")
			}
			return CardData{FullName: "Test Person"}, nil
		},
	}
	ctx := context.Background()
	r, err := s.Create(ctx, "owner", "create_operation_1", "sample_card", s.Policy().Version)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(o.data)
	for _, slot := range []string{"front", "stitched"} {
		reply, err := s.Upload(ctx, r.Owner, r.ID, UploadRequest{
			OperationKey: "upload_operation_" + slot, Revision: r.Revision, Slot: slot,
			SHA256: base64.StdEncoding.EncodeToString(sum[:]), Size: int64(len(o.data)),
		})
		if err != nil {
			t.Fatal(err)
		}
		r = reply.Capture
	}
	r, err = s.Finalize(ctx, r.Owner, r.ID, FinalizeRequest{
		OperationKey: "finalize_operation_1", Revision: r.Revision, UploadIDs: r.Slots,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"validate", "extract"} {
		if r.Work != phase {
			t.Fatalf("expected %s, got %s", phase, r.Work)
		}
		if err := s.Work(ctx, Job{r.Owner, r.ID, r.Generation, r.Work}); err != nil {
			t.Fatal(err)
		}
		r = m.records[r.ID]
	}
	if len(r.Assets) != 2 || len(r.Processed) != 2 || r.Processed[0].Slot != "front" ||
		r.Processed[1].Slot != "stitched" || r.Extraction.Status != "complete" {
		t.Fatal("both evidence files and successful extraction must be retained")
	}
}

func TestFrontOCRStartsBeforeFinalizeAndRetriesImmediately(t *testing.T) {
	s, m, o, now := fixture(t)
	ctx := context.Background()
	attempts := 0
	s.Extractor = extractorFunc(func(_ context.Context, images []CardImage) (CardData, error) {
		attempts++
		if len(images) != 1 || images[0].Slot != "front" {
			t.Fatal("OCR must only receive the front")
		}
		if attempts == 1 {
			return CardData{}, ErrInvalid
		}
		return CardData{FullName: "Sample"}, nil
	})
	r, err := s.Create(ctx, "owner", "create_front_ocr_1", "sample_card", s.Policy().Version)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(o.data)
	request := UploadRequest{OperationKey: "upload_front_ocr_1", Revision: r.Revision, Slot: "front",
		SHA256: base64.StdEncoding.EncodeToString(sum[:]), Size: int64(len(o.data))}
	front, err := s.Upload(ctx, r.Owner, r.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	r, err = s.ConfirmFront(ctx, r.Owner, r.ID, front.UploadID)
	if err != nil {
		t.Fatal(err)
	}
	job := Job{r.Owner, r.ID, r.FrontOCRGeneration, "front-ocr"}
	if err = s.Work(ctx, job); err != nil {
		t.Fatal(err)
	}
	r = m.records[r.ID]
	if r.Extraction.Status != "retrying" || r.FrontOCRDue != now.Unix() {
		t.Fatal("manual review must be available while immediate retry is queued")
	}
	// Finalize and validate the second upload while the retry runs.
	s.Extractor = extractorFunc(func(_ context.Context, images []CardImage) (CardData, error) {
		attempts++
		if len(images) != 1 || images[0].Slot != "front" {
			t.Fatal("retry must only receive the front")
		}
		current := m.records[r.ID]
		request.OperationKey, request.Revision, request.Slot = "upload_stitched_ocr_1", current.Revision, "stitched"
		stitched, err := s.Upload(ctx, r.Owner, r.ID, request)
		if err != nil {
			t.Fatal(err)
		}
		current, err = s.Finalize(ctx, r.Owner, r.ID, FinalizeRequest{OperationKey: "finalize_front_ocr_1",
			Revision: stitched.Capture.Revision, UploadIDs: stitched.Capture.Slots})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Work(ctx, Job{current.Owner, current.ID, current.Generation, current.Work}); err != nil {
			t.Fatal(err)
		}
		current = m.records[r.ID]
		_, err = s.Review(ctx, r.Owner, r.ID, ReviewRequest{Revision: current.Revision,
			Data: CardData{FullName: "Manual"}, OriginCountry: "MX"})
		if err != nil {
			t.Fatal(err)
		}
		return CardData{FullName: "Sample"}, nil
	})
	if err = s.Work(ctx, job); err != nil {
		t.Fatal(err)
	}
	r = m.records[r.ID]
	if r.State != "ready" || r.Extraction.Status != "complete" || attempts != 2 ||
		r.Reviewed == nil || r.Reviewed.Data.FullName != "Manual" {
		t.Fatal("front OCR did not complete independently of finalization and manual review")
	}
}
