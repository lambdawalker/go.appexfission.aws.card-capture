package awscapture

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/lambdawalker/go.appexfission.aws.card-capture/capture"
	"testing"
)

func TestRecordRoundTripPreservesIDAndPendingWork(t *testing.T) {
	in := capture.Record{ID: "abcdef", Owner: "owner", Revision: 3, Work: "cleanup", Due: 123, TTL: 999}
	m, e := recordItem(in)
	if e != nil {
		t.Fatal(e)
	}
	var got capture.Record
	if e = attributevalue.UnmarshalMap(m, &got); e != nil {
		t.Fatal(e)
	}
	if got.ID != in.ID || got.Owner != in.Owner {
		t.Fatal("DynamoDB key overwrote capture ID", got)
	}
	if _, ok := m["ttl"]; ok {
		t.Fatal("pending cleanup must never expire through TTL")
	}
}

func TestRecordRoundTripPreservesExtractionAndReview(t *testing.T) {
	in := capture.Record{ID: "abcdef", Owner: "owner", Revision: 4, Work: "extract", Due: 123,
		Extraction: &capture.Extraction{Status: "complete", Data: capture.CardData{DocumentNumber: "000123"}},
		Reviewed:   &capture.ReviewedCard{Data: capture.CardData{FullName: "Corrected"}, SavedAt: 99}}
	m, e := recordItem(in)
	if e != nil {
		t.Fatal(e)
	}
	var got capture.Record
	if e = attributevalue.UnmarshalMap(m, &got); e != nil {
		t.Fatal(e)
	}
	if got.Extraction.Data.DocumentNumber != "000123" || got.Reviewed.Data.FullName != "Corrected" {
		t.Fatal("lost extracted/reviewed fields")
	}
}

func TestReadyTransactionPersistsDurableAccountCompletion(t *testing.T) {
	before := capture.Record{ID: "capture", Owner: "owner", Revision: 1}
	after := before
	after.Revision = 2
	after.State = "ready"
	after.TTL = 123
	writes, err := swapWrites(before, after, true)
	if err != nil || len(writes) != 3 {
		t.Fatal(writes, err)
	}
	update := writes[2].Update
	if update == nil || aws.ToString(update.UpdateExpression) != "SET IDCaptured = :yes ADD Revision :one" || update.Key["id"].(*types.AttributeValueMemberS).Value != "ACCOUNT#"+ownerKey("owner") {
		t.Fatal("completion must be atomic, monotonic and fence stale account writes", writes)
	}
	if _, ok := update.ExpressionAttributeValues[":ttl"]; ok {
		t.Fatal("onboarding fact must not expire with evidence")
	}
	writes, err = swapWrites(before, after, false)
	if err != nil || len(writes) != 1 {
		t.Fatal("non-ready transition created completion", writes, err)
	}
}

func TestFrontOCRIndexProjectionPreservesValidationLane(t *testing.T) {
	record := capture.Record{Owner: "owner", ID: "sample", State: "finalizing",
		Work: "validate", Due: 200, Generation: 4, FrontOCRDue: 100, FrontOCRGeneration: 2}
	item, err := recordItem(record)
	if err != nil {
		t.Fatal(err)
	}
	if key := item["work"].(*types.AttributeValueMemberS).Value; key != "front-ocr" {
		t.Fatal("GSI must schedule the earlier OCR job", key)
	}
	var decoded capture.Record
	if err := decodeRecord(item, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Work != record.Work || decoded.Due != record.Due {
		t.Fatalf("validation lane changed: %s %d", decoded.Work, decoded.Due)
	}
	job, due := nextJob(decoded)
	if job.Work != "front-ocr" || job.Generation != 2 || due != 100 {
		t.Fatalf("wrong projected job: %+v at %d", job, due)
	}
}
