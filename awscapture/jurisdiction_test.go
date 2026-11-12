package awscapture

import (
	"encoding/json"
	"github.com/lambdawalker/go.appexfission.aws.card-capture/capture"
	"strings"
	"testing"
)

func TestDocumentJurisdictionRoundTrip(t *testing.T) {
	input := `{"full_name":"Test Person","document_type":"voter_id","document_name":"Credencial para votar","issuing_country":"Mexico","issuing_region":"Chihuahua","issuing_city":"Ciudad Juárez","issuing_authority":"INE"}`
	data, err := decodeCardJSON(input)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	var got, want map[string]string
	json.Unmarshal(encoded, &got)
	json.Unmarshal([]byte(input), &want)
	for key, value := range want {
		if got[key] != value {
			t.Errorf("%s not preserved", key)
		}
	}
	report := capture.NewComparisonReport(capture.Record{Extraction: &capture.Extraction{Status: "complete", Data: data}}, capture.ReviewedCard{Data: data})
	for key := range want {
		score := report.FieldSimilarity[key]
		if score == nil || *score != 1 {
			t.Errorf("%s missing from comparison report", key)
		}
	}
	var restored capture.CardData
	if err := json.Unmarshal(encoded, &restored); err != nil || restored != data {
		t.Fatal("round trip failed", err)
	}
}

func TestNewDocumentFieldsBounded(t *testing.T) {
	for _, key := range []string{"document_type", "document_name", "issuing_region", "issuing_city", "issuing_authority"} {
		for _, value := range []string{strings.Repeat("a", 513), "bad\rvalue", "bad\x00value"} {
			encoded, _ := json.Marshal(map[string]string{key: value})
			var data capture.CardData
			if err := json.Unmarshal(encoded, &data); err != nil {
				t.Fatal(err)
			}
			if data.Valid() {
				t.Errorf("%s accepted invalid value", key)
			}
		}
	}
}
