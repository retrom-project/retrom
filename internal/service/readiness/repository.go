package readiness

import "context"

type Repository interface {
	Check(context.Context) (Status, error)
}

type Status struct {
	Missing, Failed int64
}
