package netplay

import "fmt"

func applicationError(operation string, err error) error {
	return fmt.Errorf("netplay/%s: %w", operation, err)
}
