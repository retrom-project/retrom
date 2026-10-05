package dberrors

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// Classify exposes stable diagnostic categories without leaking driver details.
func Classify(err error) string {
	var cause *pgconn.PgError
	if !errors.As(err, &cause) {
		return ""
	}
	switch cause.Code {
	case "40001", "40P01", "55P03", "53300":
		return "DATABASE_BUSY"
	case "23502", "23503", "23505", "23514", "23P01":
		return "DATABASE_CONSTRAINT_FAILED"
	default:
		return ""
	}
}
