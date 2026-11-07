package ocrbench

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/lambdawalker/go.appexfission.aws.card-capture/capture"
)

type Validation struct {
	Passed     bool     `json:"passed"`
	Mismatches []string `json:"mismatches"`
}

func compareCard(actual, expected capture.CardData) *Validation {
	fields := func(data capture.CardData) map[string]string {
		encoded, _ := json.Marshal(data)
		var result map[string]string
		_ = json.Unmarshal(encoded, &result)
		return result
	}
	want := fields(expected)
	mismatches := []string{}
	for key, value := range fields(actual) {
		// Address line breaks and repeated whitespace are presentation differences.
		if key == "address" {
			value = strings.Join(strings.Fields(value), " ")
			want[key] = strings.Join(strings.Fields(want[key]), " ")
		}
		if value != want[key] {
			mismatches = append(mismatches, key)
		}
	}
	sort.Strings(mismatches)
	return &Validation{Passed: len(mismatches) == 0, Mismatches: mismatches}
}
