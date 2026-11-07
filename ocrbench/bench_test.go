package ocrbench

import (
	"context"
	"errors"
	"github.com/lambdawalker/go.appexfission.aws.card-capture/capture"
	"testing"
)

type extractor struct{ fail bool }

func (x extractor) Extract(_ context.Context, images []capture.CardImage) (capture.CardData, error) {
	if len(images) != 2 || images[0].Slot != "front" || images[1].Slot != "back" {
		panic("wrong pair")
	}
	if x.fail {
		return capture.CardData{}, errors.New("private provider detail")
	}
	return capture.CardData{FullName: "sensitive"}, nil
}
func TestRun(t *testing.T) {
	var held [][]byte
	read := func(context.Context, string) ([]byte, error) {
		b := []byte{1, 2, 3}
		held = append(held, b)
		return b, nil
	}
	r := Run(context.Background(), Request{Front: "inputs/f", Back: "inputs/b"}, read, extractor{})
	if !r.Success || r.Error != "" {
		t.Fatal(r)
	}
	for _, b := range held {
		for _, v := range b {
			if v != 0 {
				t.Fatal("image not cleared")
			}
		}
	}
}
func TestInvalidInputNeverReads(t *testing.T) {
	for _, req := range []Request{{Front: "../front", Back: "inputs/b"}, {Front: "inputs/f", Back: "inputs/f"}, {Front: "inputs/../f", Back: "inputs/b"}} {
		r := Run(context.Background(), req, func(context.Context, string) ([]byte, error) { t.Fatal("read"); return nil, nil }, extractor{})
		if r.Error != "invalid_input" {
			t.Fatal(r)
		}
	}
}
func TestFailureRedacted(t *testing.T) {
	r := Run(context.Background(), Request{Front: "inputs/f", Back: "inputs/b"}, func(context.Context, string) ([]byte, error) { return []byte{1}, nil }, extractor{true})
	if r.Success || r.Error != "extraction_failed" {
		t.Fatal(r)
	}
}

type stitchedExtractor struct{ t *testing.T }

func (x stitchedExtractor) Extract(context.Context, []capture.CardImage) (capture.CardData, error) {
	x.t.Fatal("pair extraction used")
	return capture.CardData{}, nil
}
func (x stitchedExtractor) ExtractStitched(_ context.Context, b []byte) (capture.CardData, error) {
	if len(b) != 3 {
		x.t.Fatal("missing image")
	}
	return capture.CardData{}, nil
}
func TestStitchedReadsOnceAndClears(t *testing.T) {
	calls := 0
	data := []byte{1, 2, 3}
	r := Run(context.Background(), Request{Stitched: "inputs/stitched.jpg"}, func(_ context.Context, key string) ([]byte, error) {
		calls++
		if key != "inputs/stitched.jpg" {
			t.Fatal(key)
		}
		return data, nil
	}, stitchedExtractor{t})
	if !r.Success || calls != 1 {
		t.Fatal(r, calls)
	}
	for _, b := range data {
		if b != 0 {
			t.Fatal("not cleared")
		}
	}
	for _, req := range []Request{{Stitched: "../bad"}, {Stitched: "inputs/s", Front: "inputs/f"}} {
		r = Run(context.Background(), req, func(context.Context, string) ([]byte, error) { t.Fatal("read invalid request"); return nil, nil }, stitchedExtractor{t})
		if r.Error != "invalid_input" {
			t.Fatal(r)
		}
	}
}

func TestExtractionRequiresExplicitOptInWithoutFixture(t *testing.T) {
	for _, include := range []bool{false, true} {
		r := Run(context.Background(), Request{Front: "inputs/f", Back: "inputs/b", IncludeExtraction: include},
			func(context.Context, string) ([]byte, error) { return []byte{1}, nil }, extractor{})
		if !r.Success || (r.Extraction != nil) != include {
			t.Fatalf("include=%v: %+v", include, r)
		}
	}
}
