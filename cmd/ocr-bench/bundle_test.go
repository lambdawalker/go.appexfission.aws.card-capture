package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSafeCloudLogs(t *testing.T) {
	events, omitted := safeLogEvents([]string{`{"msg":"capture.inference_failed","stage":"request","elapsed_ms":123,"secret":"PRIVATE"}`, `{"msg":"unrecognized","name":"PRIVATE"}`, `raw PRIVATE`, "REPORT RequestId: abc Duration: 12.5 ms Memory Size: 1024 MB"})
	b, _ := json.Marshal(events)
	if strings.Contains(string(b), "PRIVATE") || len(events) != 2 || omitted != 2 {
		t.Fatal(string(b), omitted)
	}
}
func TestCloudRequestIsolation(t *testing.T) {
	id := "12345678-1234-1234-1234-123456789012"
	calls := 0
	call := func(_ context.Context, args ...string) ([]byte, []byte, error) {
		calls++
		if calls == 1 {
			return []byte(`{"events":[{"logStreamName":"stream"}]}`), nil, nil
		}
		return []byte(`{"events":[{"message":"START RequestId: other"},{"message":"PRIVATE OTHER"},{"message":"START RequestId: ` + id + `"},{"message":"{\"msg\":\"capture.inference_prepared\",\"elapsed_ms\":20}"},{"message":"REPORT RequestId: ` + id + ` Duration: 21 ms"},{"message":"PRIVATE OTHER"}]}`), nil, nil
	}
	r := collectCloud(context.Background(), call, "group", id, time.Now(), time.Now(), nil)
	if r.Status != "collected" || len(r.Events) != 3 || r.Omitted != 0 {
		t.Fatal(r)
	}
}
func TestCloudPermissionFailureKeepsTail(t *testing.T) {
	r := collectCloud(context.Background(), func(context.Context, ...string) ([]byte, []byte, error) {
		return nil, []byte("PRIVATE"), errors.New("denied")
	}, "group", "request", time.Now(), time.Now(), []byte("REPORT RequestId: request Duration: 5 ms"))
	if r.Status != "cloudwatch_unavailable" || len(r.Events) != 1 {
		t.Fatal(r)
	}
}
