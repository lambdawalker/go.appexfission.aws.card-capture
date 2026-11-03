package runtimepolicy

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

func key(id, sk string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{"id": &types.AttributeValueMemberS{Value: id}, "sk": &types.AttributeValueMemberS{Value: sk}}
}
func (s Store) read(ctx context.Context, id, sk string, v any) error {
	r, e := s.DB.GetItem(ctx, &dynamodb.GetItemInput{TableName: aws.String(s.Table), Key: key(id, sk), ConsistentRead: aws.Bool(true)})
	if e != nil {
		return e
	}
	if len(r.Item) == 0 {
		return ErrNotFound
	}
	return attributevalue.UnmarshalMap(r.Item, v)
}
func (s Store) Get(ctx context.Context, env string) (Config, error) {
	var c Config
	e := s.read(ctx, "ENV#"+env, "CONFIG", &c)
	if e == nil {
		e = c.Validate()
		if c.Environment != env {
			e = ErrInvalid
		}
	}
	return c.Normalized(), e
}
