package model

import (
	"errors"
	"fmt"
)

var (
	ErrInvalid        = errors.New("invalid input")
	ErrNotFound       = errors.New("not found")
	ErrForbidden      = errors.New("forbidden")
	ErrUnauthorized   = errors.New("authentication required")
	ErrConflict       = errors.New("version conflict")
	ErrUnavailable    = errors.New("service unavailable")
	ErrRateLimited    = errors.New("rate limited")
	ErrBIOSMissing    = errors.New("required BIOS is not installed")
	ErrContextExpired = errors.New("runtime context expired")
)

func Invalid(field string) error { return fmt.Errorf("%s: %w", field, ErrInvalid) }
