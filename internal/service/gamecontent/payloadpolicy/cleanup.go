package payloadpolicy

import "retrom/internal/cleanup"

func Cleanup() cleanup.Plan {
	return cleanup.Plan{
		Groups:           []string{"GAME_RUNTIME", "GAME_EVIDENCE", "GAME_FILES"},
		ConsumeUploads:   true,
		WaitForMutations: true,
		AdvanceVersion:   true,
	}
}
