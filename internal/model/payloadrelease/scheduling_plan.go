package payloadrelease

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// BuildScheduledJob constructs the durable job values from explicit
// identities and request facts. Identity generation and persistence remain
// outside the model; both application and transaction-side coordinators use
// this single deterministic construction rule.
func BuildScheduledJob(request ScheduleRequest, jobID, executionID string) (ScheduledJob, error) {
	if !ValidScheduleScope(request.Scope.Type) || request.Scope.ID == "" || request.ScopeVersion < 1 ||
		!ValidReason(request.Reason) || request.NowMS < 0 {
		return ScheduledJob{}, ErrScopeInvalid
	}
	if jobID == "" || executionID == "" {
		return ScheduledJob{}, ErrScheduleIDInvalid
	}
	input := Input{
		SchemaVersion: 1, Kind: "PAYLOAD_RELEASE", Scope: request.Scope, ExecutionID: executionID,
		Inputs: ScopeInputs{ScopeVersion: request.ScopeVersion, Reason: request.Reason},
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return ScheduledJob{}, fmt.Errorf("encode release input: %w", err)
	}
	inputDigest := sha256.Sum256(encoded)
	dedupe := sha256.Sum256([]byte("retrom-job-dedupe-v1\x00PAYLOAD_RELEASE\x00" +
		string(request.Scope.Type) + "\x00" + request.Scope.ID))
	return ScheduledJob{
		ID: jobID, Scope: request.Scope, NowMS: request.NowMS,
		DedupeKey: hex.EncodeToString(dedupe[:]), InputJSON: string(encoded), InputDigest: hex.EncodeToString(inputDigest[:]),
	}, nil
}
