package releaseschedule

import (
	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/payloadrelease"
)

func OwnerUpdate(before application.Owner, jobID string) recordstore.Update {
	return recordstore.Update{
		Set:    "payload_state='RELEASING',payload_release_job_id=?,version=version+1",
		Values: []any{jobID},
		Scope: recordstore.Scope{
			Where: "id=? AND version=? AND payload_state='RETAINED' AND COALESCE(payload_release_job_id,'')=?",
			Args:  []any{before.Scope.ID, before.Version, before.ReleaseJobID},
		},
	}
}
