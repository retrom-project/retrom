// Package clock provides the system implementation of the technical clock port.
package clock

import (
	"time"

	clockmodel "retrom/internal/model/clock"
)

// System reads the host wall clock in Unix milliseconds.
type System struct{}

var _ clockmodel.Clock = System{}

func (System) NowMS() int64 { return time.Now().UnixMilli() }
