package uploads

import "errors"

var (
	errFinalizeIO  = errors.New("UPLOAD_FINALIZE_IO")
	errPartMissing = errors.New("UPLOAD_PART_MISSING")
	errPartCorrupt = errors.New("UPLOAD_PART_CORRUPT")
)

type byteRange struct{ start, end, total int64 }
