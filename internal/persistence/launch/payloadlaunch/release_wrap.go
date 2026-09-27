package payloadlaunch

import "fmt"

func wrapErr(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("payloadlaunch release persistence: %w", err)
}
