package payloadpolicy

import "retrom/internal/cleanup"

func ItemCleanup() cleanup.Plan {
	return cleanup.Plan{Groups: []string{"IMPORT_REVIEW", "IMPORT_FILES", "IMPORT_EVIDENCE"}, ConsumeUploads: true}
}

func JobCleanup() cleanup.Plan {
	return cleanup.Plan{ConsumeUploads: true, RequireChildrenReleased: true}
}
