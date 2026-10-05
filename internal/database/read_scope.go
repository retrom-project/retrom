package database

import "context"

type readScopeKey struct{}

// WithReadScope permits a composed response projection to read the exact
// transaction that owns the command result instead of a different pool snapshot.
func WithReadScope(ctx context.Context, reader Queryer) context.Context {
	return context.WithValue(ctx, readScopeKey{}, reader)
}

func ReadScope(ctx context.Context) (Queryer, bool) {
	reader, found := ctx.Value(readScopeKey{}).(Queryer)
	return reader, found
}
