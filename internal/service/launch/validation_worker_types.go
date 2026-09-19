package launch

import (
	"errors"
	"time"
)

var (
	ErrValidationGameChanged  = errors.New("validation game changed")
	errValidationLeaseExpired = errors.New("validation lease expired")
	ErrValidationInput        = errors.New("validation input invalid")
)

const (
	validationBudget = 30 * time.Minute

	validationUnavailable = "LAUNCH_CORE_VALIDATION_UNAVAILABLE"
)
