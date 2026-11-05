// Package ocrbench runs the production extractor without onboarding services.
package ocrbench

import (
	"context"
	"errors"
	"github.com/lambdawalker/go.appexfission.aws.card-capture/capture"
	"strings"
	"time"
)

type Request struct {
	Stitched string `json:"stitched,omitempty"`
	Front    string `json:"front,omitempty"`
	Back     string `json:"back,omitempty"`
}
type Result struct {
	PrepareMS   int64  `json:"prepare_ms"`
	InferenceMS int64  `json:"inference_ms"`
	Success     bool   `json:"success"`
	ColdStart   bool   `json:"cold_start"`
	ReadMS      int64  `json:"read_ms"`
	ExtractMS   int64  `json:"extract_ms"`
	TotalMS     int64  `json:"total_ms"`
	Error       string `json:"error,omitempty"`
}
type Reader func(context.Context, string) ([]byte, error)

// Run deliberately returns timings only; document values never enter benchmark reports.
func Run(ctx context.Context, req Request, read Reader, extractor capture.Extractor) Result {
	start := time.Now()
	result := Result{}
	finish := func(code string) Result {
		result.Error = code
		result.TotalMS = time.Since(start).Milliseconds()
		return result
	}
	valid := func(k string) bool {
		return strings.HasPrefix(k, "inputs/") && !strings.Contains(k, "..") && len(k) < 512
	}
	keys, slots := []string{req.Front, req.Back}, []string{"front", "back"}
	if req.Stitched != "" {
		if !valid(req.Stitched) || req.Front != "" || req.Back != "" {
			return finish("invalid_input")
		}
		keys, slots = []string{req.Stitched}, []string{"stitched"}
	} else if !valid(req.Front) || !valid(req.Back) || req.Front == req.Back {
		return finish("invalid_input")
	}
	images := make([]capture.CardImage, 0, 2)
	defer func() {
		for _, im := range images {
			clear(im.JPEG)
		}
	}()
	for i, k := range keys {
		b, e := read(ctx, k)
		if e != nil {
			return finish("read_failed")
		}
		images = append(images, capture.CardImage{Slot: slots[i], JPEG: b})
	}
	result.ReadMS = time.Since(start).Milliseconds()
	started := time.Now()
	var e error
	if req.Stitched != "" {
		x, ok := extractor.(interface {
			ExtractStitched(context.Context, []byte) (capture.CardData, error)
		})
		if !ok {
			return finish("stitched_unsupported")
		}
		_, e = x.ExtractStitched(ctx, images[0].JPEG)
	} else {
		_, e = extractor.Extract(ctx, images)
	}
	result.ExtractMS = time.Since(started).Milliseconds()
	if e != nil {
		return finish("extraction_failed")
	}
	result.Success = true
	return finish("")
}

var ErrInput = errors.New("input_unavailable")
