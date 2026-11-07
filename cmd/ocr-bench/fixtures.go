package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/lambdawalker/go.appexfission.aws.card-capture/capture"
)

type fixture struct {
	name, front, back string
	expected          *capture.CardData
}

func loadFixtures(root string) ([]fixture, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read validation directory %q: %w", root, err)
	}
	numbered := regexp.MustCompile(`^[0-9]+$`)
	var fixtures []fixture
	for _, entry := range entries {
		if !entry.IsDir() || !numbered.MatchString(entry.Name()) {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		item := fixture{name: entry.Name(), front: filepath.Join(dir, "layout-1.jpg"), back: filepath.Join(dir, "layout-2.jpg")}
		for _, path := range []string{item.front, item.back} {
			info, err := os.Stat(path)
			if err != nil || !info.Mode().IsRegular() {
				return nil, fmt.Errorf("fixture %s requires %s", item.name, filepath.Base(path))
			}
		}
		data, err := os.ReadFile(filepath.Join(dir, "expected-card-capture.json"))
		if err != nil {
			return nil, fmt.Errorf("fixture %s: %w", item.name, err)
		}
		var fields map[string]*string
		canonical, _ := json.Marshal(capture.CardData{})
		var required map[string]string
		_ = json.Unmarshal(canonical, &required)
		if json.Unmarshal(data, &fields) != nil || len(fields) != len(required) {
			return nil, fmt.Errorf("fixture %s: expected-card-capture.json must contain every OCR field as a string", item.name)
		}
		for field := range required {
			if fields[field] == nil {
				return nil, fmt.Errorf("fixture %s: expected-card-capture.json requires string field %s", item.name, field)
			}
		}
		item.expected = new(capture.CardData)
		if err := json.Unmarshal(data, item.expected); err != nil || !item.expected.Valid() {
			return nil, fmt.Errorf("fixture %s: invalid expected card data", item.name)
		}
		fixtures = append(fixtures, item)
	}
	if len(fixtures) == 0 {
		return nil, fmt.Errorf("no numbered fixture directories in %q", root)
	}
	return fixtures, nil
}
