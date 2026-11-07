package ocrbench

import (
	"testing"

	"github.com/lambdawalker/go.appexfission.aws.card-capture/capture"
)

func TestCompareCard(t *testing.T) {
	want := capture.CardData{DocumentNumber: "001", Address: "42 Maple Street Apt 3B"}
	got := capture.CardData{DocumentNumber: "001", Address: "42 Maple Street\nApt 3B"}
	if result := compareCard(got, want); !result.Passed {
		t.Fatal(result)
	}
	got.DocumentNumber = "1"
	got.PersonalNumber = "unexpected"
	result := compareCard(got, want)
	if result.Passed || len(result.Mismatches) != 2 || result.Mismatches[0] != "document_number" || result.Mismatches[1] != "issued_identifier" {
		t.Fatal(result)
	}
}
