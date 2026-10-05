// Package diagnostic defines content rejection facts shared across preparation
// and import projections. It performs no I/O and carries no runtime ownership.
package diagnostic

type Limit struct {
	Metric  string `json:"metric"`
	Actual  int64  `json:"actual"`
	Maximum int64  `json:"maximum"`
}

type Rejection struct {
	Code         string `json:"code"`
	RelativePath string `json:"relativePath"`
	Limit        *Limit `json:"limit"`
}
