package ocrbench

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/lambdawalker/go.appexfission.aws.card-capture/capture"
)

type Validation struct {
	Similarity    float64  `json:"similarity_pct"`
	MinSimilarity float64  `json:"min_similarity_pct"`
	Passed        bool     `json:"passed"`
	Mismatches    []string `json:"mismatches"`
}

func compareCard(actual, expected capture.CardData, threshold float64) *Validation {
	fields := func(data capture.CardData) map[string]string {
		encoded, _ := json.Marshal(data)
		var result map[string]string
		_ = json.Unmarshal(encoded, &result)
		return result
	}
	want := fields(expected)
	mismatches := []string{}
	var total float64
	var scored int
	for key, value := range fields(actual) {
		// Address line breaks and repeated whitespace are presentation differences.
		if key == "address" {
			value = strings.Join(strings.Fields(value), " ")
			want[key] = strings.Join(strings.Fields(want[key]), " ")
		}
		expectedValue := want[key]
		if value == "" && expectedValue == "" {
			continue
		}
		scored++
		total += fieldSimilarity(value, expectedValue)
		if value != expectedValue {
			mismatches = append(mismatches, key)
		}
	}
	sort.Strings(mismatches)
	similarity := 100.0
	if scored != 0 {
		similarity = total * 100 / float64(scored)
	}
	return &Validation{Similarity: similarity, MinSimilarity: threshold, Passed: similarity >= threshold, Mismatches: mismatches}
}

// fieldSimilarity is one minus the fraction of characters that must change.
func fieldSimilarity(actual, expected string) float64 {
	if actual == expected {
		return 1
	}
	a, b := []rune(actual), []rune(expected)
	row := make([]int, len(b)+1)
	for j := range row {
		row[j] = j
	}
	for i, char := range a {
		diagonal := row[0]
		row[0] = i + 1
		for j, other := range b {
			cost := 0
			if char != other {
				cost = 1
			}
			next := min(row[j+1]+1, row[j]+1, diagonal+cost)
			diagonal, row[j+1] = row[j+1], next
		}
	}
	return 1 - float64(row[len(b)])/float64(max(len(a), len(b)))
}
