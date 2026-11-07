package capture

import (
	"maps"
	"slices"
	"strings"
)

// ComparisonReport is a write-once snapshot for administrative evaluation.
type ComparisonReport struct {
	SchemaVersion     int                 `json:"schema_version"`
	CaptureID         string              `json:"capture_id"`
	ImageRevision     int64               `json:"image_revision"`
	OCRStatus         string              `json:"ocr_status"`
	OCR               CardData            `json:"ocr"`
	Submitted         ReviewedCard        `json:"submitted"`
	FieldSimilarity   map[string]*float64 `json:"field_similarity"`
	AverageSimilarity *float64            `json:"average_similarity"`
}

func NewComparisonReport(r Record, review ReviewedCard) ComparisonReport {
	report := ComparisonReport{
		SchemaVersion: 1, CaptureID: r.ID, ImageRevision: r.ImageRevision,
		OCRStatus: r.Extraction.Status, OCR: r.Extraction.Data, Submitted: review,
		FieldSimilarity: make(map[string]*float64),
	}
	extracted, submitted := report.OCR.fields(), review.Data.fields()
	var total float64
	var count int
	for _, name := range slices.Sorted(maps.Keys(extracted)) {
		left, right := normalizeForComparison(extracted[name]), normalizeForComparison(submitted[name])
		if len(left) == 0 && len(right) == 0 {
			report.FieldSimilarity[name] = nil
			continue
		}
		score := fieldSimilarity(left, right)
		report.FieldSimilarity[name] = &score
		total += score
		count++
	}
	if count != 0 {
		average := total / float64(count)
		report.AverageSimilarity = &average
	}
	return report
}

func (d CardData) fields() map[string]string {
	return map[string]string{
		"full_name": d.FullName, "given_names": d.GivenNames, "surnames": d.Surnames,
		"birth_date": d.BirthDate, "document_number": d.DocumentNumber, "issued_identifier": d.PersonalNumber,
		"address": d.Address, "issue_date": d.IssueDate, "expiry_date": d.ExpiryDate, "issuing_country": d.IssuingCountry,
	}
}

func normalizeForComparison(value string) []rune {
	return []rune(strings.ToLower(strings.Join(strings.Fields(value), " ")))
}

func fieldSimilarity(left, right []rune) float64 {
	// Levenshtein distance with one working row.
	row := make([]int, len(right)+1)
	for j := range row {
		row[j] = j
	}
	for i, a := range left {
		diagonal := row[0]
		row[0] = i + 1
		for j, b := range right {
			next := row[j+1]
			cost := 0
			if a != b {
				cost = 1
			}
			row[j+1] = min(next+1, row[j]+1, diagonal+cost)
			diagonal = next
		}
	}
	return 1 - float64(row[len(right)])/float64(max(len(left), len(right)))
}
