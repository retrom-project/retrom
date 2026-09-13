package netplay

import "time"

type Clock interface {
	Now() time.Time
}

type clockFunc func() time.Time

func (function clockFunc) Now() time.Time { return function() }
