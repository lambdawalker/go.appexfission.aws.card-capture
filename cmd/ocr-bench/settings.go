package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type benchSettings struct{ Profile, Region, Account, Bucket, Function, Front, Back string }

func findRoot(start string) string {
	for {
		if _, e := os.Stat(filepath.Join(start, ".deploy", "project.json")); e == nil {
			return start
		}
		parent := filepath.Dir(start)
		if parent == start {
			return ""
		}
		start = parent
	}
}
func loadSaved(root, home, environment string) (benchSettings, string, error) {
	var s benchSettings
	var project struct {
		Name    string
		Version int
	}
	b, e := os.ReadFile(filepath.Join(root, ".deploy", "project.json"))
	if e != nil {
		return s, environment, e
	}
	if json.Unmarshal(b, &project) != nil || project.Version != 1 || !regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`).MatchString(project.Name) {
		return s, environment, fmt.Errorf("invalid deployment project settings")
	}
	if environment == "" {
		var flags struct{ Environment string }
		if b, e = os.ReadFile(filepath.Join(root, ".deploy", "flags.json")); e == nil {
			if json.Unmarshal(b, &flags) != nil {
				return s, environment, fmt.Errorf("invalid deployment flags")
			}
			environment = flags.Environment
		}
	}
	if environment == "" {
		environment = "dev"
	}
	if !regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}$`).MatchString(environment) {
		return s, environment, fmt.Errorf("invalid environment")
	}
	central := filepath.Join(home, ".infra-composer", project.Name, "environments", environment)
	b, e = os.ReadFile(filepath.Join(central, "settings.json"))
	if os.IsNotExist(e) {
		b, e = os.ReadFile(filepath.Join(root, ".deploy", "environments", environment, "settings.json"))
	}
	if e != nil {
		return s, environment, e
	}
	var saved struct {
		Version                               int
		Environment, Profile, Region, Account string
	}
	if json.Unmarshal(b, &saved) != nil || saved.Version != 1 || saved.Environment != environment {
		return s, environment, fmt.Errorf("deployment settings scope mismatch")
	}
	s.Profile = saved.Profile
	s.Region = saved.Region
	s.Account = saved.Account
	if b, e = os.ReadFile(filepath.Join(central, "outputs.json")); e == nil {
		var outputs struct{ Bucket, Function string }
		if json.Unmarshal(b, &outputs) != nil {
			return s, environment, fmt.Errorf("invalid deployment outputs")
		}
		s.Bucket = outputs.Bucket
		s.Function = outputs.Function
	} else if !os.IsNotExist(e) {
		return s, environment, e
	}
	var inputs struct{ Front, Back string }
	if b, e = os.ReadFile(filepath.Join(root, ".deploy", "ocr-bench", environment, "inputs.json")); e == nil {
		if json.Unmarshal(b, &inputs) != nil {
			return s, environment, fmt.Errorf("invalid saved image paths")
		}
		s.Front = inputs.Front
		s.Back = inputs.Back
	}
	return s, environment, nil
}
func chooseImage(root, side, saved string) string {
	if saved != "" {
		if _, e := os.Stat(saved); e == nil {
			return saved
		}
	}
	var matches []string
	for _, dir := range []string{root, filepath.Join(root, "photos")} {
		for _, ext := range []string{"jpg", "jpeg", "png"} {
			p := filepath.Join(dir, side+"."+ext)
			if info, e := os.Stat(p); e == nil && !info.IsDir() {
				matches = append(matches, p)
			}
		}
	}
	if len(matches) == 1 {
		return matches[0]
	}
	return ""
}
func askMissing(reader *bufio.Reader, label, value string) (string, error) {
	if value != "" {
		return value, nil
	}
	fmt.Printf("%s: ", label)
	line, e := reader.ReadString('\n')
	if e != nil {
		return "", fmt.Errorf("%s is missing; supply its flag when running noninteractively", label)
	}
	line = strings.Trim(strings.TrimSpace(line), `"`)
	if line == "" {
		return "", fmt.Errorf("%s required", label)
	}
	return line, nil
}
func prepareImage(path, out string) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	cfg, _, e := image.DecodeConfig(f)
	if e != nil {
		return fmt.Errorf("unsupported image %s: use PNG or JPEG", path)
	}
	if cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width)*int64(cfg.Height) > 40000000 {
		return fmt.Errorf("image dimensions exceed benchmark limit")
	}
	if _, e = f.Seek(0, 0); e != nil {
		return e
	}
	im, _, e := image.Decode(f)
	if e != nil {
		return e
	}
	target, e := os.OpenFile(out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	e = jpeg.Encode(target, im, &jpeg.Options{Quality: 90})
	closeErr := target.Close()
	if e != nil {
		return e
	}
	return closeErr
}
