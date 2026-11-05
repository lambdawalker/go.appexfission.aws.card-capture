package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestInvokeDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, meta, body, stderr, code string
		cliFail                        bool
	}{
		{"yaml metadata", "StatusCode: 200", "", "", "invalid_invoke_metadata", false},
		{"runtime entrypoint", `{"StatusCode":200,"FunctionError":"Unhandled"}`, `{"errorType":"Runtime.InvalidEntrypoint","errorMessage":"private detail"}`, "", "invalid_entrypoint", false},
		{"access denied", "", "", "An error occurred (AccessDeniedException): private detail", "access_denied", true},
		{"function missing", "", "", "An error occurred (ResourceNotFoundException): private detail", "function_not_found", true},
		{"unexpected response", `{"StatusCode":200}`, `{"errorMessage":"private detail"}`, "", "invalid_response", false},
		{"read failure", `{"StatusCode":200}`, `{"success":false,"error":"read_failed"}`, "", "read_failed", false},
		{"extraction failure", `{"StatusCode":200}`, `{"success":false,"error":"extraction_failed"}`, "", "extraction_failed", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var e error
			if tc.cliFail {
				e = errors.New("failed")
			}
			r, d := diagnoseInvoke([]byte(tc.meta), []byte(tc.body), []byte(tc.stderr), e, "ocr")
			if r.Success || d == nil || d.Code != tc.code {
				t.Fatal(r, d)
			}
			b, _ := json.Marshal(d)
			if strings.Contains(string(b), "private detail") {
				t.Fatal("raw diagnostic leaked")
			}
		})
	}
}
func TestInvokeSuccess(t *testing.T) {
	r, d := diagnoseInvoke([]byte(`{"StatusCode":200}`), []byte(`{"success":true,"total_ms":123}`), nil, nil, "ocr")
	if !r.Success || r.TotalMS != 123 || d != nil {
		t.Fatal(r, d)
	}
}
