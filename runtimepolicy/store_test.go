package runtimepolicy

import (
	"context"
	"errors"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"strings"
	"testing"
)

type readDB struct {
	item  map[string]types.AttributeValue
	input *dynamodb.GetItemInput
	err   error
}

func (d *readDB) GetItem(_ context.Context, in *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	d.input = in
	return &dynamodb.GetItemOutput{Item: d.item}, d.err
}
func TestRuntimeDocumentReadAndLegacyNormalization(t *testing.T) {
	c := Default("dev")
	c.Onboarding = nil
	c.Capture.Messages = Capture{}.Messages
	c.Features.IDCapture = false
	d := &readDB{}
	d.item, _ = attributevalue.MarshalMap(c)
	got, e := (Store{DB: d, Table: "shared"}).Get(context.Background(), "dev")
	if e != nil {
		t.Fatal(e)
	}
	if got.Onboarding == nil || got.Onboarding.IDCapture != Disabled || got.Policy().Enabled || got.Policy().Purposes.EN == "" {
		t.Fatal(got)
	}
	if aws.ToString(d.input.TableName) != "shared" || !aws.ToBool(d.input.ConsistentRead) || d.input.Key["id"].(*types.AttributeValueMemberS).Value != "ENV#dev" || d.input.Key["sk"].(*types.AttributeValueMemberS).Value != "CONFIG" {
		t.Fatal(d.input)
	}
	c.Environment = "prod"
	d.item, _ = attributevalue.MarshalMap(c)
	if _, e := (Store{DB: d}).Get(context.Background(), "dev"); e != ErrInvalid {
		t.Fatal(e)
	}
	d.item = nil
	if _, e := (Store{DB: d}).Get(context.Background(), "dev"); e != ErrNotFound {
		t.Fatal(e)
	}
	d.err = errors.New("read")
	if _, e := (Store{DB: d}).Get(context.Background(), "dev"); e != d.err {
		t.Fatal(e)
	}
}
func TestPublicDocumentValidationAndConsentHash(t *testing.T) {
	base := Default("dev")
	if e := base.Validate(); e != nil {
		t.Fatal(e)
	}
	hash := base.Policy().Version
	base.Revision++
	base.UpdatedAt = "2026-11-03T00:00:00Z"
	if base.Policy().Version != hash {
		t.Fatal("metadata changed consent")
	}
	base.Capture.Messages.ES = "Nuevo propósito"
	if base.Policy().Version == hash {
		t.Fatal("disclosure did not change consent")
	}
	for name, mutate := range map[string]func(*Config){"schema": func(c *Config) { c.SchemaVersion = 2 }, "environment": func(c *Config) { c.Environment = "../dev" }, "revision": func(c *Config) { c.Revision = -1 }, "date": func(c *Config) { c.UpdatedAt = "bad" }, "purpose": func(c *Config) { c.Capture.Purpose = "" }, "control": func(c *Config) { c.Capture.Purpose = "a\nb" }, "size": func(c *Config) { c.Capture.Purpose = strings.Repeat("a", 1025) }, "onboarding": func(c *Config) { c.Features.IDCapture = false }} {
		t.Run(name, func(t *testing.T) {
			c := Default("dev")
			mutate(&c)
			if c.Validate() != ErrInvalid {
				t.Fatal("invalid accepted")
			}
		})
	}
}
