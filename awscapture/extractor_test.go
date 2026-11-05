package awscapture

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/lambdawalker/go.appexfission.aws.card-capture/capture"
)

func cardImages(t *testing.T) []capture.CardImage {
	t.Helper()
	var b bytes.Buffer
	if e := jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2400, 1600)), nil); e != nil {
		t.Fatal(e)
	}
	return []capture.CardImage{{Slot: "front", JPEG: b.Bytes()}, {Slot: "back", JPEG: b.Bytes()}}
}
func TestGemmaRequestUsesBoundedImagesAndSignedMantle(t *testing.T) {
	x := BedrockExtractor{Transport: &Transport{Config: aws.Config{Region: "us-east-2", Credentials: credentials{}}, HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://bedrock-mantle.us-east-2.api.aws/openai/v1/chat/completions" || !strings.Contains(r.Header.Get("Authorization"), "/bedrock-mantle/aws4_request") {
			t.Fatal("wrong endpoint/signing")
		}
		b, e := io.ReadAll(r.Body)
		if e != nil {
			t.Fatal(e)
		}
		if len(b) > 3500000 {
			t.Fatal("request exceeds model limit")
		}
		var v struct {
			Model    string `json:"model"`
			Messages []struct {
				Content []struct {
					Type     string `json:"type"`
					ImageURL struct {
						URL string `json:"url"`
					} `json:"image_url"`
				} `json:"content"`
			} `json:"messages"`
		}
		if e = json.Unmarshal(b, &v); e != nil {
			t.Fatal(e)
		}
		if v.Model != "google.gemma-4-e2b" {
			t.Fatal("wrong model")
		}
		count := 0
		for _, m := range v.Messages {
			for _, c := range m.Content {
				if c.Type == "image_url" {
					count++
					data, e := base64.StdEncoding.DecodeString(strings.TrimPrefix(c.ImageURL.URL, "data:image/jpeg;base64,"))
					if e != nil {
						t.Fatal(e)
					}
					cfg, e := jpeg.DecodeConfig(bytes.NewReader(data))
					if e != nil || cfg.Width > 1600 || cfg.Height > 1600 {
						t.Fatal("unbounded image")
					}
				}
			}
		}
		if count != 2 {
			t.Fatal("missing image")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"finish_reason":"stop","message":{"content":"{\"full_name\":\"Ana\",\"document_number\":\"000123\"}"}}]}`))}, nil
	})}}}
	d, e := x.Extract(context.Background(), cardImages(t))
	if e != nil || d.FullName != "Ana" || d.DocumentNumber != "000123" {
		t.Fatal("response rejected", e)
	}
}
func TestGemmaRejectsMalformedTruncatedAndOversizedResponses(t *testing.T) {
	for _, body := range []string{
		`{"choices":[{"finish_reason":"length","message":{"content":"{}"}}]}`,
		`{"choices":[{"finish_reason":"stop","message":{"content":"{\"unexpected\":true}"}}]}`,
		`{"choices":[{"finish_reason":"stop","message":{"content":"{} {}"}}]}`,
		strings.Repeat("x", 65537),
	} {
		t.Run(body[:min(len(body), 50)], func(t *testing.T) {
			x := BedrockExtractor{Transport: &Transport{Config: aws.Config{Region: "us-east-2", Credentials: credentials{}}, HTTP: &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}}}
			if _, e := x.Extract(context.Background(), cardImages(t)); e == nil {
				t.Fatal("unsafe response accepted")
			}
		})
	}
}

func TestStitchedBenchmarkSendsOneImage(t *testing.T) {
	calls := 0
	x := BedrockExtractor{Transport: &Transport{Config: aws.Config{Region: "us-east-2", Credentials: credentials{}}, HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		var payload struct {
			Messages []struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		count := 0
		layout := false
		for _, m := range payload.Messages {
			for _, c := range m.Content {
				if c.Type == "image_url" {
					count++
				}
				if strings.Contains(c.Text, "FRONT at the TOP and the BACK at the BOTTOM") {
					layout = true
				}
			}
		}
		if count != 1 || !layout {
			t.Fatal("incorrect stitched request", count, layout)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"finish_reason":"stop","message":{"content":"{}"}}]}`))}, nil
	})}}}
	data := cardImages(t)[0].JPEG
	if _, err := x.ExtractStitched(context.Background(), data); err != nil {
		t.Fatal(err)
	}
	if _, err := x.Extract(context.Background(), []capture.CardImage{{Slot: "stitched", JPEG: data}}); err == nil {
		t.Fatal("production pair contract changed")
	}
	if calls != 1 {
		t.Fatal(calls)
	}
}
