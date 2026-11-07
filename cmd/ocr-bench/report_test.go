package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lambdawalker/go.appexfission.aws.card-capture/capture"
)

func TestHTMLReportRendersBothModes(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		want       []string
	}{
		{"image pair", `{"runs":[{"success":true,"client_ms":1200,"inference_ms":800},{"success":false,"diagnostic":{"message":"failure <detail>"}}]}`, []string{"Image pair", "1.20 s", "failure &lt;detail&gt;"}},
		{"dataset", `{"validation_failures":1,"fixtures":[{"fixture":"0001","runs":[{"success":true,"validation":{"passed":false,"mismatches":["issued_identifier"]}}]}]}`, []string{"0001", "Validation failed", "issued_identifier"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "report.html")
			if err := writeReport(path, []byte(tc.data), nil); err != nil {
				t.Fatal(err)
			}
			html, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tc.want {
				if !strings.Contains(string(html), want) {
					t.Errorf("report missing %q", want)
				}
			}
		})
	}
}

func TestHTMLJSONComparison(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.html")
	pairs := map[string][]*jsonComparison{"0001": {{Expected: capture.CardData{FullName: "Élodie"}, Actual: &capture.CardData{FullName: "<script>alert(1)</script>"}}}}
	if err := writeReport(path, []byte(`{"fixtures":[{"fixture":"0001","runs":[{"success":true}]}]}`), pairs); err != nil {
		t.Fatal(err)
	}
	html, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(html)
	if strings.Count(body, `class="json-line json-changed"`) != 2 || !strings.Contains(body, "Élodie") || strings.Contains(body, "<script>alert(1)</script>") || !strings.Contains(body, "OCR result") {
		t.Fatal("comparison did not render both values and highlight only changed lines")
	}
}
