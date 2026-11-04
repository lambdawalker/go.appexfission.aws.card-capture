package awscapture

import (
	"bytes"
	"image/color"

	"image"
	"image/jpeg"
	"testing"
)

// Synthetic color edges and gradients exercise interpolation without identity data.
func syntheticJPEG(t testing.TB) []byte {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, 2400, 1600))
	for y := 0; y < 1600; y++ {
		for x := 0; x < 2400; x++ {
			i := im.PixOffset(x, y)
			im.Pix[i], im.Pix[i+1], im.Pix[i+2], im.Pix[i+3] = byte(x/3), byte(y/4), byte((x^y)/7), 255
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, im, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func BenchmarkInferenceJPEG(b *testing.B) {
	data := syntheticJPEG(b)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := inferenceJPEG(data); err != nil {
			b.Fatal(err)
		}
	}
}

func TestFastSamplerPreservesStandardJPEGColors(t *testing.T) {
	src, err := jpeg.Decode(bytes.NewReader(syntheticJPEG(t)))
	if err != nil {
		t.Fatal(err)
	}
	sample := rgbaSampler(src)
	for y := 0; y < src.Bounds().Dy(); y += 17 {
		for x := 0; x < src.Bounds().Dx(); x += 19 {
			if got, want := sample(x, y), color.RGBAModel.Convert(src.At(x, y)).(color.RGBA); got != want {
				t.Fatalf("pixel (%d,%d): got %v, want %v", x, y, got, want)
			}
		}
	}
}
