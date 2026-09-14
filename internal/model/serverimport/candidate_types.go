package serverimport

import "retrom/internal/capability/content/firmware"

type CandidateEvidence struct {
	ID, RequirementID, Association, State string
	Facts                                 firmware.FileFacts
	Static                                *firmware.StaticEvaluation
	DAT                                   *firmware.DATEvaluation
	Details                               map[string]any
}
