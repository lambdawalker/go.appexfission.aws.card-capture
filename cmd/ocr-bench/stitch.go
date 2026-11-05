package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"os"
)

const stitchedMaxBytes = 500 * 1024

// decodeBenchImage checks dimensions before decoding an untrusted photo.
func decodeBenchImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return nil, fmt.Errorf("unsupported image: use PNG or JPEG")
	}
	if cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width)*int64(cfg.Height) > 40000000 {
		return nil, fmt.Errorf("image dimensions exceed benchmark limit")
	}
	if _, err = f.Seek(0, 0); err != nil {
		return nil, err
	}
	im, _, err := image.Decode(f)
	return im, err
}

// resizeBench uses area averaging when reducing images, preserving small text
// better than nearest-neighbor sampling. Transparency is composited onto white.
func resizeBench(src image.Image, w, h int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	b := src.Bounds()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			x0, x1 := float64(x*b.Dx())/float64(w), float64((x+1)*b.Dx())/float64(w)
			y0, y1 := float64(y*b.Dy())/float64(h), float64((y+1)*b.Dy())/float64(h)
			var rr, gg, bb, weight float64
			for sy := int(y0); sy < int(y1+0.999999); sy++ {
				for sx := int(x0); sx < int(x1+0.999999); sx++ {
					area := (min(x1, float64(sx+1)) - max(x0, float64(sx))) * (min(y1, float64(sy+1)) - max(y0, float64(sy)))
					r, g, bv, a := src.At(b.Min.X+min(sx, b.Dx()-1), b.Min.Y+min(sy, b.Dy()-1)).RGBA()
					rr += float64(r+65535-a) * area
					gg += float64(g+65535-a) * area
					bb += float64(bv+65535-a) * area
					weight += area
				}
			}
			dst.SetRGBA(x, y, color.RGBA{uint8(rr / weight / 257), uint8(gg / weight / 257), uint8(bb / weight / 257), 255})
		}
	}
	return dst
}

func prepareStitched(front, back, out string) error {
	// Decode and reduce one side at a time to avoid retaining two full-size photos.
	sides := make([]*image.RGBA, 0, 2)
	for _, path := range []string{front, back} {
		im, err := decodeBenchImage(path)
		if err != nil {
			return err
		}
		b := im.Bounds()
		scale := min(1.0, min(1600.0/float64(b.Dx()), 800.0/float64(b.Dy())))
		sides = append(sides, resizeBench(im, max(1, int(float64(b.Dx())*scale)), max(1, int(float64(b.Dy())*scale))))
	}
	canvas := image.NewRGBA(image.Rect(0, 0, max(sides[0].Bounds().Dx(), sides[1].Bounds().Dx()), sides[0].Bounds().Dy()+sides[1].Bounds().Dy()))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	y := 0
	for _, side := range sides {
		x := (canvas.Bounds().Dx() - side.Bounds().Dx()) / 2
		draw.Draw(canvas, image.Rect(x, y, x+side.Bounds().Dx(), y+side.Bounds().Dy()), side, image.Point{}, draw.Src)
		y += side.Bounds().Dy()
	}
	for {
		for _, quality := range []int{85, 75, 65} {
			var encoded bytes.Buffer
			if err := jpeg.Encode(&encoded, canvas, &jpeg.Options{Quality: quality}); err != nil {
				return err
			}
			if encoded.Len() <= stitchedMaxBytes {
				f, err := os.OpenFile(out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
				if err != nil {
					return err
				}
				_, err = f.Write(encoded.Bytes())
				closeErr := f.Close()
				if err != nil {
					return err
				}
				return closeErr
			}
		}
		w, h := canvas.Bounds().Dx(), canvas.Bounds().Dy()
		if max(w, h) <= 960 {
			return fmt.Errorf("stitched image exceeds 500 KiB; use tighter card crops")
		}
		canvas = resizeBench(canvas, max(1, w*4/5), max(1, h*4/5))
	}
}
