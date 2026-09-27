package payloadbios

import "fmt"

func wrapErr(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("payloadbios release persistence: %w", err)
}
