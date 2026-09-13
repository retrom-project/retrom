package libraryimport

import "errors"

var (
	ErrMultiDiscPlaylistMissing = errors.New("MULTI_DISC_PLAYLIST_MISSING")
	ErrSourceGrouping           = errors.New("source must form one declared primary group")
	ErrMultiDiscModeUnavailable = errors.New("MULTI_DISC_MODE_UNAVAILABLE")
)
