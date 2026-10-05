package gamevariant

import (
	"slices"

	"retrom/internal/content/diagnostic"
	"retrom/internal/content/requirements"
	corevalidation "retrom/internal/core/validation"
)

func validationInputRejection(content Snapshot, evidence corevalidation.Snapshot) *diagnostic.Rejection {
	source := content.Source
	for _, file := range content.GameFiles {
		if !slices.Contains([]string{"CONTENT", "DISC", "PROJECT_FILE", "DOS_SOURCE"}, file.Role) {
			continue
		}
		if rejection := source.ContentPolicy.CheckFile(
			source.ContentKind, file.LogicalName, file.SizeBytes,
		); rejection != nil {
			return rejection
		}
	}
	parsed := requirements.Facts{}
	if evidence.ContentFacts != nil {
		parsed = *evidence.ContentFacts
	}
	return source.ContentPolicy.Requirements.Evaluate(parsed, source.ValidationLogicalName)
}
