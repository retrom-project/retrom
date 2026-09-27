package payloadrelease

import "fmt"

func wrapErr(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("payload release persistence: %w", err)
}

func wrapPair[T any](value T, err error) (T, error) {
	return value, wrapErr(err)
}
