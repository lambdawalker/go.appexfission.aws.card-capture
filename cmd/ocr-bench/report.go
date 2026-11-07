package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"

	"github.com/lambdawalker/go.appexfission.aws.card-capture/ocrbench"
)

//go:embed report.html
var reportHTML string

var reportTemplate = template.Must(template.New("report").Funcs(template.FuncMap{
	"ms":   func(ms int64) string { return fmt.Sprintf("%.2f s", float64(ms)/1000) },
	"next": func(i int) int { return i + 1 },
}).Parse(reportHTML))

type reportRun struct {
	ocrbench.Result
	ClientMS   int64       `json:"client_ms"`
	Diagnostic *diagnostic `json:"diagnostic"`
}

type reportFixture struct {
	Name      string      `json:"fixture"`
	Runs      []reportRun `json:"runs"`
	UploadMS  int64       `json:"upload_ms"`
	PrepareMS int64       `json:"image_prepare_ms"`
	Width     int         `json:"upload_width_px"`
	Height    int         `json:"upload_height_px"`
	Bytes     int64       `json:"upload_bytes"`
	P50       int64       `json:"client_p50_ms"`
	P95       int64       `json:"client_p95_ms"`
}

type reportPage struct {
	Fixtures            []reportFixture `json:"fixtures"`
	ValidationPasses    int             `json:"validation_passes"`
	ValidationFailures  int             `json:"validation_failures"`
	Dataset             bool
	Attempts, Succeeded int
	P50, P95            int64
	Scale               int64
}

func (r reportRun) Status() string {
	switch {
	case !r.Success:
		return "Invocation failed"
	case r.Validation == nil:
		return "Extracted"
	case !r.Validation.Passed:
		return "Mismatch"
	default:
		return "Passed"
	}
}

func writeReport(path string, data []byte) error {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return os.WriteFile(path, data, 0600)
	case ".html":
		var page reportPage
		if err := json.Unmarshal(data, &page); err != nil {
			return err
		}
		page.Dataset = len(page.Fixtures) > 0
		if !page.Dataset {
			var fixture reportFixture
			if err := json.Unmarshal(data, &fixture); err != nil {
				return err
			}
			fixture.Name = "Image pair"
			page.Fixtures = []reportFixture{fixture}
		}
		var successful []int64
		for _, fixture := range page.Fixtures {
			for _, run := range fixture.Runs {
				page.Attempts++
				page.Scale = max(page.Scale, run.ClientMS, run.InferenceMS)
				if run.Success {
					page.Succeeded++
					successful = append(successful, run.ClientMS)
				}
			}
		}
		page.Scale = max(1, page.Scale)
		page.P50 = percentile(successful, .5)
		page.P95 = percentile(successful, .95)
		var output bytes.Buffer
		if err := reportTemplate.Execute(&output, page); err != nil {
			return err
		}
		return os.WriteFile(path, output.Bytes(), 0600)
	default:
		return fmt.Errorf("report path must end in .html or .json")
	}
}
