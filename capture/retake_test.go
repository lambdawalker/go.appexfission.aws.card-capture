package capture

import (
	"context"
	"testing"
)

func TestRetakeFencesExtractionAndPreservesQuota(t *testing.T) {
	s, m, _, _, r := extractionFixture(t)
	account, _ := m.Account(context.Background(), r.Owner)
	if r.ImageRevision <= 0 || r.ImageRevision >= r.Revision {
		t.Fatal("worker revisions must preserve the finalized image revision")
	}
	oldJob := Job{r.Owner, r.ID, r.Generation, r.Work}
	request := RetakeRequest{OperationKey: "retake_operation_1", Revision: r.Revision}
	next, err := s.Retake(context.Background(), r.Owner, r.ID, request)
	if err != nil || next.ID != r.ID || next.Generation <= r.Generation || next.ImageRevision != 0 || next.Extraction != nil || next.State != "uploading" {
		t.Fatal(next, err)
	}
	if _, err = s.Retake(context.Background(), r.Owner, r.ID, request); err != nil {
		t.Fatal(err)
	}
	if err = s.Work(context.Background(), oldJob); err != nil {
		t.Fatal(err)
	}
	if m.records[r.ID].Extraction != nil {
		t.Fatal("obsolete job published")
	}
	after, _ := m.Account(context.Background(), r.Owner)
	if account.CreatedToday != after.CreatedToday {
		t.Fatal("retake consumed quota")
	}
	if _, err = s.Retake(context.Background(), r.Owner, r.ID, RetakeRequest{OperationKey: "retake_operation_2", Revision: r.Revision}); err != ErrConflict {
		t.Fatal(err)
	}
}
