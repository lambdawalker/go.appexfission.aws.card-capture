package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/jpeg"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/lambdawalker/go.appexfission.aws.card-capture/ocrbench"
)

func serveAndroidBridge(bucket, function string) error {
	key := make([]byte, 24)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	token := hex.EncodeToString(key)
	listener, err := net.Listen("tcp4", "127.0.0.1:8765")
	if err != nil {
		return err
	}
	fmt.Printf("Android OCR bridge listening on %s\nRun adb reverse tcp:8765 tcp:8765\nDemo pairing code: %s\nUse synthetic cards only. Stop the bridge with Ctrl+C.\n", listener.Addr(), token)
	server := &http.Server{
		Handler: androidBridge(token, func(ctx context.Context, image []byte) (ocrbench.Result, error) {
			return invokeAndroidOCR(ctx, bucket, function, image)
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      145 * time.Second,
		IdleTimeout:       15 * time.Second,
	}
	return server.Serve(listener)
}

func androidBridge(token string, invoke func(context.Context, []byte) (ocrbench.Result, error)) http.Handler {
	active := make(chan struct{}, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /ocr", func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
			http.Error(w, "invalid_pairing_code", http.StatusUnauthorized)
			return
		}
		select {
		case active <- struct{}{}:
			defer func() { <-active }()
		default:
			http.Error(w, "bridge_busy", http.StatusTooManyRequests)
			return
		}
		if r.Header.Get("Content-Type") != "image/jpeg" {
			http.Error(w, "jpeg_required", http.StatusUnsupportedMediaType)
			return
		}
		image, err := io.ReadAll(http.MaxBytesReader(w, r.Body, stitchedMaxBytes))
		defer clear(image)
		if err != nil {
			var limit *http.MaxBytesError
			if errors.As(err, &limit) {
				http.Error(w, "image_too_large", http.StatusRequestEntityTooLarge)
			} else {
				http.Error(w, "invalid_input", http.StatusBadRequest)
			}
			return
		}
		cfg, err := jpeg.DecodeConfig(bytes.NewReader(image))
		if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 1200 || cfg.Height > 1200 {
			http.Error(w, "invalid_image_dimensions", http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 115*time.Second)
		defer cancel()
		result, err := invoke(ctx, image)
		if err != nil {
			http.Error(w, "aws_request_failed", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(result)
	})
	return mux
}

func invokeAndroidOCR(ctx context.Context, bucket, function string, image []byte) (ocrbench.Result, error) {
	var result ocrbench.Result
	dir, err := os.MkdirTemp("", "card-capture-android-")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(dir)
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return result, err
	}
	key := "inputs/android-" + hex.EncodeToString(id) + "/stitched.jpg"
	imagePath := filepath.Join(dir, "stitched.jpg")
	if err := os.WriteFile(imagePath, image, 0600); err != nil {
		return result, err
	}
	// Clean up even when a cancelled PUT had an uncertain outcome.
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if _, err := aws(cleanup, "s3api", "delete-object", "--bucket", bucket, "--key", key); err != nil {
			fmt.Fprintln(os.Stderr, "Android demo input cleanup failed; bucket lifecycle will retry expiration.")
		}
	}()
	if _, err := aws(ctx, "s3api", "put-object", "--bucket", bucket, "--key", key,
		"--body", imagePath, "--content-type", "image/jpeg"); err != nil {
		return result, err
	}
	request, err := json.Marshal(ocrbench.Request{Stitched: key, IncludeExtraction: true})
	if err != nil {
		return result, err
	}
	requestPath := filepath.Join(dir, "request.json")
	if err := os.WriteFile(requestPath, request, 0600); err != nil {
		return result, err
	}
	resultPath := filepath.Join(dir, "result.json")
	meta, stderr, invokeErr := invokeAWS(ctx, "lambda", "invoke", "--function-name", function,
		"--cli-binary-format", "raw-in-base64-out", "--payload", "fileb://"+requestPath,
		"--cli-read-timeout", "100", "--output", "json", "--no-cli-pager", resultPath)
	body, _ := os.ReadFile(resultPath)
	defer clear(body)
	result, _ = diagnoseInvoke(meta, body, stderr, invokeErr, function)
	return result, nil
}
