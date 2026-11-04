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
	r := Run(context.Background(), Request{"inputs/f", "inputs/b"}, read, extractor{})
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
	for _, req := range []Request{{"../front", "inputs/b"}, {"inputs/f", "inputs/f"}, {"inputs/../f", "inputs/b"}} {
		r := Run(context.Background(), req, func(context.Context, string) ([]byte, error) { t.Fatal("read"); return nil, nil }, extractor{})
		if r.Error != "invalid_input" {
			t.Fatal(r)
		}
	}
}
func TestFailureRedacted(t *testing.T) {
	r := Run(context.Background(), Request{"inputs/f", "inputs/b"}, func(context.Context, string) ([]byte, error) { return []byte{1}, nil }, extractor{true})
	if r.Success || r.Error != "extraction_failed" {
		t.Fatal(r)
	}
}
