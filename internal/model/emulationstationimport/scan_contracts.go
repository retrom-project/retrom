package emulationstationimport

import "context"

type ScanMutation struct {
	Before LeaseSnapshot
	NowMS  int64
}

type ScanReader interface {
	Current(context.Context, string) (LeaseSnapshot, bool, error)
}

type ScanWriter interface {
	Clear(context.Context, ScanMutation) error
	Headers(context.Context, ScanMutation, ScanProjection) error
	Items(context.Context, ScanMutation, []ScanItem) error
	Complete(context.Context, ScanMutation, ScanProjection) error
	Reject(context.Context, ScanMutation, ScanProjection) error
}

type ScanScope struct {
	Read  ScanReader
	Write ScanWriter
}

type ScanRepository interface {
	WithScan(context.Context, func(ScanScope) error) error
}
