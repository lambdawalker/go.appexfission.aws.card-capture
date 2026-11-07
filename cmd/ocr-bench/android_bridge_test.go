package main

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lambdawalker/go.appexfission.aws.card-capture/ocrbench"
)

func TestAndroidBridgeValidatesPairingAndInputBeforeInvoking(t *testing.T) {
	var jpegBody bytes.Buffer
	if err := jpeg.Encode(&jpegBody, image.NewRGBA(image.Rect(0, 0, 12, 18)), nil); err != nil {
		t.Fatal(err)
	}
	calls := 0
	handler := androidBridge("paired", func(_ context.Context, data []byte) (ocrbench.Result, error) {
		calls++
		if !bytes.Equal(data, jpegBody.Bytes()) {
			t.Fatal("unexpected image")
		}
		return ocrbench.Result{Success: true}, nil
	})
	for _, tc := range []struct {
		name, token, contentType string
		body                     []byte
		status                   int
	}{
		{"bad code", "wrong", "image/jpeg", jpegBody.Bytes(), http.StatusUnauthorized},
		{"bad type", "paired", "text/plain", jpegBody.Bytes(), http.StatusUnsupportedMediaType},
		{"too large", "paired", "image/jpeg", make([]byte, stitchedMaxBytes+1), http.StatusRequestEntityTooLarge},
		{"invalid image", "paired", "image/jpeg", []byte("not a JPEG"), http.StatusBadRequest},
		{"valid image", "paired", "image/jpeg", jpegBody.Bytes(), http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/ocr", bytes.NewReader(tc.body))
			request.Header.Set("Authorization", "Bearer "+tc.token)
			request.Header.Set("Content-Type", tc.contentType)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tc.status {
				t.Fatalf("status %d, want %d", response.Code, tc.status)
			}
			if tc.status == http.StatusOK && response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("sensitive response must not be cached")
			}
		})
	}
	if calls != 1 {
		t.Fatalf("invoked %d times, want 1", calls)
	}
}
