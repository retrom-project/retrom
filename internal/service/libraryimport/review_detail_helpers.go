package libraryimport

import (
	"path"
	"strings"
)

// ProjectReviewArchiveFormat determines the display archive format for a review
// source file. TyranoScript executables in ZIP format are presented as NWJS.
func ProjectReviewArchiveFormat(contentKind, name string, format *string) *string {
	if contentKind == "TYRANOSCRIPT_PROJECT" &&
		format != nil &&
		*format == "ZIP" &&
		strings.EqualFold(path.Ext(name), ".exe") {
		value := "NWJS_EXECUTABLE"
		return &value
	}
	return format
}

// ReviewApproval determines whether a review item can be approved.
func ReviewApproval(contentKind string, selectedReady, hasCurrentScreenshot bool) bool {
	return selectedReady || contentKind != "SCUMMVM_PROJECT" && hasCurrentScreenshot
}
