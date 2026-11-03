// Package resetgate provides only the read-only token fence for capture requests.
package resetgate

import (
	"context"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

type Dynamo interface {
	GetItem(context.Context, *dynamodb.GetItemInput, ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
}
type Store struct {
	DB    Dynamo
	Table string
}

func (s Store) CheckToken(ctx context.Context, iat int64) (bool, error) {
	out, e := s.DB.GetItem(ctx, &dynamodb.GetItemInput{TableName: aws.String(s.Table), Key: map[string]types.AttributeValue{"id": &types.AttributeValueMemberS{Value: "CONTROL"}}, ConsistentRead: aws.Bool(true)})
	if e != nil {
		return false, e
	}
	var state struct {
		Phase      string
		ValidAfter int64
	}
	if e = attributevalue.UnmarshalMap(out.Item, &state); e != nil {
		return false, e
	}
	return (state.Phase == "" || state.Phase == "complete") && (state.ValidAfter == 0 || iat > state.ValidAfter), nil
}
