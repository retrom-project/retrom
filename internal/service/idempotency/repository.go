package idempotency

import "context"

// Receipt is the durable response associated with one idempotency key.
type Receipt struct {
	RequestDigest string
	HTTPStatus    int
	HeadersJSON   string
	Body          []byte
}

// Repository owns the durable idempotency record boundary. Implementations
// may use any database, while the service keeps the HTTP middleware free of
// storage details.
type Repository interface {
	DeleteExpired(context.Context, string, string, string, int64) error
	Find(context.Context, string, string, string) (Receipt, bool, error)
	Save(context.Context, string, string, string, Receipt, int64, int64) error
}
