package capture

import (
	"context"
	"time"
)

type RetakeRequest struct {
	OperationKey string `json:"operation_key"`
	Revision     int64  `json:"expected_revision"`
}

// Retake invalidates jobs for the old image pair within the same capture.
func (s Service) Retake(ctx context.Context, owner, id string, in RetakeRequest) (Record, error) {
	if !ValidOperation(in.OperationKey) {
		return Record{}, ErrInvalid
	}
	r, err := s.Get(ctx, owner, id)
	if err != nil {
		return r, err
	}
	if r.State == "cancelled" || r.State == "expired" || s.now().Unix() >= r.ExpiresAt {
		return r, ErrExpired
	}
	if r.RetakeKey == in.OperationKey {
		return r, nil
	}
	if r.Reviewed != nil || r.Revision != in.Revision {
		return r, ErrConflict
	}
	switch r.State {
	case "uploading", "finalizing", "ready", "failed", "requires_recapture":
	default:
		return r, ErrConflict
	}
	if len(r.Uploads) >= 20 {
		return r, ErrLimited
	}
	before := r
	r.RetakeKey = in.OperationKey
	r.State = "uploading"
	r.ImageRevision = 0
	r.Slots = map[string]string{}
	r.Assets, r.Processed = nil, nil
	r.Extraction, r.Reviewed = nil, nil
	r.FinalizeKey, r.RetryKey, r.ProcessingVersion, r.Error = "", "", "", ""
	r.ExtractionAttempts, r.Attempts = 0, 0
	r.Generation++
	r.Revision++
	r.LeaseUntil = 0
	r.Work = "cleanup"
	r.Due = r.ExpiresAt + int64((UploadLifetime + time.Minute).Seconds())
	if err := s.Store.Swap(ctx, before, r, false); err != nil {
		return Record{}, err
	}
	return r, nil
}
