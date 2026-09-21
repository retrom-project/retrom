package bios

import "context"

// Repository exposes the BIOS catalog projection without coupling the
// application layer to a particular database implementation.
type Repository interface {
	List(context.Context, ListRequest) (ListResult, error)
}
