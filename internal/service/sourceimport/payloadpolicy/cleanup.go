package payloadpolicy

import "retrom/internal/cleanup"

func Cleanup() cleanup.Plan {
	return cleanup.Plan{
		Groups:               []string{"SOURCE_FILES", "SOURCE_ASSETS"},
		AdvanceVersion:       true,
		AllowAdvancedVersion: true,
	}
}
