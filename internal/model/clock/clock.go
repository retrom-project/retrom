// Package clock declares the technical time source used at write admission.
package clock

// Clock supplies Unix milliseconds. Repository constructors receive a controlled
// implementation; commands carry sampled time values rather than executable clocks.
type Clock interface {
	NowMS() int64
}
