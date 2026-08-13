package netplay

import "time"

type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

type clockFunc func() time.Time

func (function clockFunc) Now() time.Time { return function() }
