package pegasusimport

import (
	"errors"
	"os"
	"strings"
)

func (service *Sources) Sanitize(err error) string {
	if err == nil {
		return ""
	}
	detail := err.Error()
	var pathError *os.PathError
	if errors.As(err, &pathError) && pathError.Path != "" {
		detail = strings.ReplaceAll(detail, pathError.Path, "[path]")
	}
	var linkError *os.LinkError
	if errors.As(err, &linkError) {
		if linkError.Old != "" {
			detail = strings.ReplaceAll(detail, linkError.Old, "[path]")
		}
		if linkError.New != "" {
			detail = strings.ReplaceAll(detail, linkError.New, "[path]")
		}
	}
	for _, root := range service.roots {
		if root.path != "" {
			detail = strings.ReplaceAll(detail, root.path, "[server-root]")
		}
	}
	detail = strings.Join(strings.Fields(detail), " ")
	if len(detail) > 2048 {
		detail = detail[:2048]
	}
	return detail
}
