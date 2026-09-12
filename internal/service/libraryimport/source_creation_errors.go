package libraryimport

import "errors"

var (
	ErrSourceGrouping           = errors.New("source must form one declared primary group")
	ErrMultiDiscModeUnavailable = errors.New("MULTI_DISC_MODE_UNAVAILABLE")
)
