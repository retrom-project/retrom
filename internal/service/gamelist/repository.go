package gamelist

import "context"

// Repository provides game projections without exposing the storage engine to
// the application layer.
type Repository interface {
	Detail(context.Context, string, string) (Detail, error)
	List(context.Context, ListRequest) (ListResult, error)
}
