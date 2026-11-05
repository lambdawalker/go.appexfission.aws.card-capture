package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type logEvidence struct {
	Source    string           `json:"source"`
	RequestID string           `json:"request_id,omitempty"`
	Status    string           `json:"status"`
	Events    []map[string]any `json:"events"`
	Omitted   int              `json:"omitted"`
}

func bundleJSON(path string, v any) error {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(path, b, 0600)
}
func invocationTail(meta []byte) (string, []byte, int) {
	var m struct {
		LogResult  string
		StatusCode int
	}
	_ = json.Unmarshal(meta, &m)
	tail, _ := base64.StdEncoding.DecodeString(m.LogResult)
	id := ""
	if match := regexp.MustCompile(`RequestId: ([a-fA-F0-9-]{36})`).FindSubmatch(tail); len(match) > 1 {
		id = string(match[1])
	}
	return id, tail, m.StatusCode
}

// safeLogEvents deliberately uses an allowlist; unknown messages are counted, not exported.
func safeLogEvents(lines []string) ([]map[string]any, int) {
	events := []map[string]any{}
	omitted := 0
	allowed := map[string]bool{"capture.inference_prepared": true, "capture.inference_response": true, "capture.inference_failed": true, "capture.work_finished": true, "capture.extraction_result": true}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var raw map[string]json.RawMessage
		if json.Unmarshal([]byte(line), &raw) == nil {
			var msg string
			_ = json.Unmarshal(raw["msg"], &msg)
			if !allowed[msg] {
				omitted++
				continue
			}
			event := map[string]any{"message": msg}
			for _, key := range []string{"elapsed_ms", "headers_ms", "http_status", "attempt"} {
				var n float64
				if json.Unmarshal(raw[key], &n) == nil && n >= 0 && n < 1e12 {
					event[key] = n
				}
			}
			for _, key := range []string{"failed", "deadline_exceeded"} {
				var v bool
				if json.Unmarshal(raw[key], &v) == nil {
					event[key] = v
				}
			}
			var stage string
			if json.Unmarshal(raw["stage"], &stage) == nil {
				switch stage {
				case "request", "response_envelope", "card_json":
					event["stage"] = stage
				}
			}
			events = append(events, event)
			continue
		}
		if strings.HasPrefix(line, "START RequestId:") || strings.HasPrefix(line, "END RequestId:") || strings.HasPrefix(line, "REPORT RequestId:") {
			kind := strings.Fields(line)[0]
			event := map[string]any{"message": kind}
			for _, m := range regexp.MustCompile(`(Duration|Billed Duration|Memory Size|Max Memory Used|Init Duration):\s*([0-9.]+)`).FindAllStringSubmatch(line, -1) {
				event[m[1]] = m[2]
			}
			events = append(events, event)
			continue
		}
		for _, kind := range []string{"Runtime.InvalidEntrypoint", "Runtime.ExitError", "Runtime.ImportModuleError", "Sandbox.Timedout"} {
			if strings.Contains(line, kind) {
				events = append(events, map[string]any{"message": "runtime_error", "type": kind})
				line = ""
				break
			}
		}
		if line != "" {
			omitted++
		}
	}
	return events, omitted
}

type logsCall func(context.Context, ...string) ([]byte, []byte, error)

func collectCloud(ctx context.Context, call logsCall, group, id string, start, end time.Time, tail []byte) logEvidence {
	events, omitted := safeLogEvents(strings.Split(string(tail), "\n"))
	evidence := logEvidence{Source: "lambda-invocation-tail", RequestID: id, Status: "tail_only", Events: events, Omitted: omitted}
	if id == "" {
		b, _, err := call(ctx, "logs", "filter-log-events", "--log-group-name", group, "--start-time", fmt.Sprint(start.UnixMilli()), "--end-time", fmt.Sprint(end.Add(5*time.Second).UnixMilli()), "--limit", "100", "--no-paginate", "--output", "json", "--no-cli-pager")
		if err != nil {
			evidence.Status = "cloudwatch_unavailable"
			return evidence
		}
		var response struct{ Events []struct{ Message string } }
		if json.Unmarshal(b, &response) != nil {
			evidence.Status = "invalid_cloudwatch_response"
			return evidence
		}
		lines := []string{}
		for _, event := range response.Events {
			lines = append(lines, strings.Split(event.Message, "\n")...)
		}
		if len(lines) > 0 {
			evidence.Events, evidence.Omitted = safeLogEvents(lines)
			evidence.Source = "cloudwatch-time-window"
			evidence.Status = "uncorrelated_request_id_unavailable"
		} else {
			evidence.Status = "logs_not_visible_yet"
		}
		return evidence
	}

	var stream string
	for attempt := 0; attempt < 3; attempt++ {
		b, _, err := call(ctx, "logs", "filter-log-events", "--log-group-name", group, "--start-time", fmt.Sprint(start.Add(-5*time.Second).UnixMilli()), "--end-time", fmt.Sprint(end.Add(5*time.Second).UnixMilli()), "--filter-pattern", `"`+id+`"`, "--limit", "100", "--no-paginate", "--output", "json", "--no-cli-pager")
		if err != nil {
			evidence.Status = "cloudwatch_unavailable"
			return evidence
		}
		var response struct {
			Events []struct{ LogStreamName string }
		}
		if json.Unmarshal(b, &response) != nil {
			evidence.Status = "invalid_cloudwatch_response"
			return evidence
		}
		if len(response.Events) > 0 {
			stream = response.Events[0].LogStreamName
			break
		}
		if attempt < 2 {
			select {
			case <-ctx.Done():
				evidence.Status = "collection_timeout"
				return evidence
			case <-time.After(2 * time.Second):
			}
		}
	}
	if stream == "" {
		evidence.Status = "logs_not_visible_yet"
		return evidence
	}
	b, _, err := call(ctx, "logs", "get-log-events", "--log-group-name", group, "--log-stream-name", stream, "--start-time", fmt.Sprint(start.Add(-5*time.Second).UnixMilli()), "--end-time", fmt.Sprint(end.Add(5*time.Second).UnixMilli()), "--start-from-head", "--limit", "1000", "--no-paginate", "--output", "json", "--no-cli-pager")
	if err != nil {
		evidence.Status = "cloudwatch_unavailable"
		return evidence
	}
	var result struct{ Events []struct{ Message string } }
	if json.Unmarshal(b, &result) != nil {
		evidence.Status = "invalid_cloudwatch_response"
		return evidence
	}
	lines := []string{}
	inside := false
	complete := false
	for _, event := range result.Events {
		for _, line := range strings.Split(event.Message, "\n") {
			if strings.HasPrefix(line, "START RequestId: "+id) {
				inside = true
			}
			if inside {
				lines = append(lines, line)
			}
			if strings.HasPrefix(line, "REPORT RequestId: "+id) && inside {
				complete = true
				inside = false
			}
		}
	}
	if len(lines) == 0 {
		evidence.Status = "request_window_unavailable"
		return evidence
	}
	evidence.Events, evidence.Omitted = safeLogEvents(lines)
	evidence.Source = "cloudwatch-request-window"
	evidence.Status = "collected"
	if !complete {
		evidence.Status = "partial"
	}
	return evidence
}
