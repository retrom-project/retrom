package pegasusimport

import (
	"context"
	payload "retrom/internal/service/payloadrelease"
)

type payloadItemMemory struct{ memory *itemWorkFake }

func payloadItemScope(memory *itemWorkFake) payload.ReleaseScope {
	return payload.ReleaseScope{Scheduling: payloadItemMemory{memory: memory}}
}
func (records payloadItemMemory) Owner(_ context.Context, ref payload.Scope) (payload.Owner, error) {
	return payload.Owner{Scope: ref, State: records.memory.outcome.State, Version: records.memory.item.Version + 1,
		PayloadState: "RETAINED", Retryable: records.memory.outcome.Retryable}, nil
}
func (payloadItemMemory) PendingChildren(context.Context, string) (int64, error) { return 0, nil }
func (payloadItemMemory) Consumption(context.Context, string) (payload.Consumption, error) {
	return payload.Consumption{}, nil
}
func (payloadItemMemory) CreateJob(context.Context, payload.ScheduledJob) error    { return nil }
func (payloadItemMemory) BeginRelease(context.Context, payload.OwnerRelease) error { return nil }
