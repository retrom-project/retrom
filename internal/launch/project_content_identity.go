package launch

import (
	"fmt"

	application "retrom/internal/service/launch"
)

const RuntimeProjectContentPrefix = application.RuntimeProjectContentPrefix

func RuntimeProjectContentRoot(identity string) (string, error) {
	value, err := application.RuntimeProjectContentRoot(identity)
	if err != nil {
		return "", fmt.Errorf("runtime project root: %w", err)
	}
	return value, nil
}
