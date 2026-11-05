package main

import (
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestStitchedBoundsSizeAndOrder(t *testing.T) {
	dir := t.TempDir()
	for _, side := range []struct {
		name string
		c    color.RGBA
	}{{"front", color.RGBA{255, 0, 0, 255}}, {"back", color.RGBA{0, 0, 255, 255}}} {
		im := image.NewRGBA(image.Rect(0, 0, 2400, 1500))
		for y := 0; y < 1500; y++ {
			for x := 0; x < 2400; x++ {
				im.SetRGBA(x, y, side.c)
			}
		}
		f, _ := os.Create(filepath.Join(dir, side.name+".png"))
		png.Encode(f, im)
		f.Close()
	}
	out := filepath.Join(dir, "stitched.jpg")
	if err := prepareStitched(filepath.Join(dir, "front.png"), filepath.Join(dir, "back.png"), out); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	stat, _ := f.Stat()
	im, err := jpeg.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if stat.Size() > stitchedMaxBytes || im.Bounds().Dx() > 1200 || im.Bounds().Dy() > 1200 {
		t.Fatal(stat.Size(), im.Bounds())
	}
	if im.Bounds().Dx() != 960 || im.Bounds().Dy() != 1200 {
		t.Fatal("unexpected dimensions", im.Bounds())
	}
	cfg, err := uploadedImageConfig(out)
	if err != nil || cfg.Width != im.Bounds().Dx() || cfg.Height != im.Bounds().Dy() {
		t.Fatal("reported dimensions differ from uploaded JPEG", cfg, err)
	}
	frontGray := color.GrayModel.Convert(im.At(im.Bounds().Dx()/2, 10)).(color.Gray).Y
	backGray := color.GrayModel.Convert(im.At(im.Bounds().Dx()/2, im.Bounds().Dy()-10)).(color.Gray).Y
	if frontGray <= backGray {
		t.Fatal("front must be top, back bottom")
	}
	if _, ok := im.(*image.Gray); !ok {
		t.Fatal("JPEG must use grayscale encoding")
	}
	for y := 0; y < im.Bounds().Dy(); y += 37 {
		for x := 0; x < im.Bounds().Dx(); x += 37 {
			r, g, b, _ := im.At(x, y).RGBA()
			if r != g || g != b {
				t.Fatal("colored output pixel")
			}
		}
	}
	if err := prepareStitched(out, out, out); err == nil {
		t.Fatal("overwrote source")
	}
}

func TestStitchRejectsInvalidInput(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.png")
	os.WriteFile(bad, []byte("invalid"), 0600)
	out := filepath.Join(dir, "out.jpg")
	if err := prepareStitched(bad, bad, out); err == nil {
		t.Fatal("invalid image accepted")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("output created on failure")
	}
}

func TestStitchedNoisyImagesStayWithinByteLimit(t *testing.T) {
	dir := t.TempDir()
	// Deterministic high-frequency data exercises the quality/size fallback.
	im := image.NewRGBA(image.Rect(0, 0, 1600, 800))
	var n uint32 = 123
	for i := 0; i < len(im.Pix); i += 4 {
		for j := 0; j < 3; j++ {
			n = n*1664525 + 1013904223
			im.Pix[i+j] = byte(n >> 24)
		}
		im.Pix[i+3] = 255
	}
	source := filepath.Join(dir, "noise.png")
	f, _ := os.Create(source)
	png.Encode(f, im)
	f.Close()
	out := filepath.Join(dir, "stitched.jpg")
	if err := prepareStitched(source, source, out); err != nil {
		t.Fatal(err)
	}
	stat, err := os.Stat(out)
	if err != nil || stat.Size() > stitchedMaxBytes {
		t.Fatal(stat, err)
	}
}
