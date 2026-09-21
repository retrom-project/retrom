package dberrors

import (
	"errors"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// Classify provides stable diagnostic categories without exposing database driver types.
func Classify(err error) string {
	var databaseError *sqlite.Error
	if !errors.As(err, &databaseError) {
		return ""
	}
	switch databaseError.Code() & 0xff {
	case sqlite3.SQLITE_BUSY, sqlite3.SQLITE_LOCKED:
		return "DATABASE_BUSY"
	case sqlite3.SQLITE_CONSTRAINT:
		return "DATABASE_CONSTRAINT_FAILED"
	default:
		return ""
	}
}
