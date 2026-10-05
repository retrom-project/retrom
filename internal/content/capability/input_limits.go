package contentcapability

import "retrom/internal/content/diagnostic"

func (policy Policy) CheckFile(contentKind, logicalName string, size int64) *diagnostic.Rejection {
	maximum := policy.MaxFileBytes(contentKind)
	if maximum == 0 || size <= maximum {
		return nil
	}
	return &diagnostic.Rejection{
		Code: "CONTENT_FILE_BYTES_EXCEEDED", RelativePath: logicalName,
		Limit: &diagnostic.Limit{Metric: "FILE_BYTES", Actual: size, Maximum: maximum},
	}
}
