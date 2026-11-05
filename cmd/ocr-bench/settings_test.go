package main

import (
	"bufio"
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSaved(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	write := func(p, s string) {
		os.MkdirAll(filepath.Dir(p), 0700)
		if e := os.WriteFile(p, []byte(s), 0600); e != nil {
			t.Fatal(e)
		}
	}
	write(filepath.Join(root, ".deploy/project.json"), `{"Version":1,"Name":"demo"}`)
	write(filepath.Join(root, ".deploy/flags.json"), `{"Environment":"qa"}`)
	dir := filepath.Join(home, ".infra-composer/demo/environments/qa")
	write(filepath.Join(dir, "settings.json"), `{"Version":1,"Environment":"qa","Profile":"attestra","Region":"us-east-2","Account":"123"}`)
	write(filepath.Join(dir, "outputs.json"), `{"bucket":"input-bucket","function":"ocr-function"}`)
	s, env, e := loadSaved(root, home, "")
	if e != nil || env != "qa" || s.Profile != "attestra" || s.Bucket != "input-bucket" || s.Function != "ocr-function" {
		t.Fatal(s, env, e)
	}
	if _, _, e = loadSaved(root, home, "../bad"); e == nil {
		t.Fatal("bad environment accepted")
	}
}
func TestAskOnlyMissing(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("image.png\n"))
	v, e := askMissing(reader, "known", "saved")
	if e != nil || v != "saved" {
		t.Fatal(v, e)
	}
	v, e = askMissing(reader, "path", "")
	if e != nil || v != "image.png" {
		t.Fatal(v, e)
	}
	if _, e = askMissing(reader, "missing", ""); e == nil {
		t.Fatal("EOF accepted")
	}
}
func TestPNGConvertedToJPEG(t *testing.T) {
	dir := t.TempDir()
	var b bytes.Buffer
	png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 12, 8)))
	src := filepath.Join(dir, "front.png")
	os.WriteFile(src, b.Bytes(), 0600)
	out := filepath.Join(dir, "front.jpg")
	if e := prepareImage(src, out); e != nil {
		t.Fatal(e)
	}
	f, e := os.Open(out)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	cfg, e := jpeg.DecodeConfig(f)
	if e != nil || cfg.Width != 12 || cfg.Height != 8 {
		t.Fatal(cfg, e)
	}
}
func TestImageDiscoveryAvoidsAmbiguity(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "front.png")
	os.WriteFile(p, []byte("x"), 0600)
	if chooseImage(dir, "front", "") != p {
		t.Fatal("not discovered")
	}
	os.WriteFile(filepath.Join(dir, "front.jpg"), []byte("x"), 0600)
	if chooseImage(dir, "front", "") != "" {
		t.Fatal("ambiguous selection")
	}
}
