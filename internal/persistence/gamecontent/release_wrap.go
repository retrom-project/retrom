package gamecontent

import "fmt"

func wrapErr(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("gamecontent release persistence: %w", err)
}
