package scans

import (
	"context"
	"errors"

	"retrom/internal/model"
)

// The host identifies a failed stage, not an engine's format or decoder reason.
// The original error remains in diagnostics; HTTP/progress get a safe stable code.
func normalizationFailure(err error) error {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded, model.ErrUnavailable, model.ErrStorageFull} {
		if errors.Is(err, cause) {
			return wrap(err)
		}
	}
	return errors.Join(&model.RuntimeContentError{
		Code:    "CONTENT_NORMALIZATION_FAILED",
		Message: "内容转换未能完成，原内容已保留；请检查来源格式、配套文件和完整性后重试。",
	}, err)
}

func importFailureCode(err error) string {
	var contentError *model.RuntimeContentError
	if errors.As(err, &contentError) && contentError.Code == "CONTENT_NORMALIZATION_FAILED" {
		return contentError.Code
	}
	return "IMPORT_FAILED"
}
