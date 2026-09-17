package emulationstationimport

import (
	"context"

	payload "retrom/internal/model/payloadrelease"
)

type payloadItemMemory struct{ memory *itemWorkMemory }

func payloadItemScope(memory *itemWorkMemory) payload.ReleaseScope {
	return payload.ReleaseScope{Scheduling: payloadItemMemory{memory: memory}}
}

func (records payloadItemMemory) Owner(_ context.Context, ref payload.Scope) (payload.Owner, error) {
	return payload.Owner{
		Scope: ref, State: records.memory.outcome.State, Version: records.memory.before.Item.Version + 1,
		PayloadState: "RETAINED", Retryable: records.memory.outcome.Retryable,
	}, nil
}
func (payloadItemMemory) PendingChildren(context.Context, string) (int64, error) { return 0, nil }
func (payloadItemMemory) Consumption(context.Context, string) (payload.Consumption, error) {
	return payload.Consumption{}, nil
}
func (payloadItemMemory) CreateJob(context.Context, payload.ScheduledJob) error    { return nil }
func (payloadItemMemory) BeginRelease(context.Context, payload.OwnerRelease) error { return nil }
