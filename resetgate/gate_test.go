package resetgate

import (
	"context"
	"errors"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"testing"
)

type gateDB struct {
	phase string
	after int64
	err   error
	empty bool
}

func (d gateDB) GetItem(_ context.Context, in *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	if aws.ToString(in.TableName) != "reset" || !aws.ToBool(in.ConsistentRead) || in.Key["id"].(*types.AttributeValueMemberS).Value != "CONTROL" {
		panic("incorrect gate read")
	}
	var item map[string]types.AttributeValue
	if !d.empty {
		item, _ = attributevalue.MarshalMap(struct {
			Phase      string
			ValidAfter int64
		}{d.phase, d.after})
	}
	return &dynamodb.GetItemOutput{Item: item}, d.err
}
func TestTokenFenceParity(t *testing.T) {
	for _, tc := range []struct {
		phase      string
		after, iat int64
		want       bool
	}{{"", 0, 0, true}, {"complete", 10, 11, true}, {"complete", 10, 10, false}, {"complete", 10, 9, false}, {"resetting", 0, 100, false}, {"failed", 0, 100, false}} {
		got, e := (Store{DB: gateDB{phase: tc.phase, after: tc.after}, Table: "reset"}).CheckToken(context.Background(), tc.iat)
		if e != nil || got != tc.want {
			t.Fatalf("%+v got %v,%v", tc, got, e)
		}
	}
	sentinel := errors.New("read")
	if ok, e := (Store{DB: gateDB{err: sentinel}, Table: "reset"}).CheckToken(context.Background(), 100); ok || e != sentinel {
		t.Fatal(ok, e)
	}
}
