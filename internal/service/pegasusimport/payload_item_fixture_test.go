package pegasusimport

import (
	"context"

	payloadreleasemodel "retrom/internal/model/payloadrelease"
)

type payloadItemMemory struct{ memory *itemWorkFake }

func payloadItemScope(memory *itemWorkFake) payloadreleasemodel.ReleaseScope {
	return payloadreleasemodel.ReleaseScope{Scheduling: payloadItemMemory{memory: memory}}
}

func (records payloadItemMemory) Owner(_ context.Context, ref payloadreleasemodel.Scope) (payloadreleasemodel.Owner, error) {
	return payloadreleasemodel.Owner{
		Scope: ref, State: records.memory.outcome.State, Version: records.memory.item.Version + 1,
		PayloadState: "RETAINED", Retryable: records.memory.outcome.Retryable,
	}, nil
}
func (payloadItemMemory) PendingChildren(context.Context, string) (int64, error) { return 0, nil }
func (payloadItemMemory) Consumption(context.Context, string) (payloadreleasemodel.Consumption, error) {
	return payloadreleasemodel.Consumption{}, nil
}

func (payloadItemMemory) CreateJob(context.Context, payloadreleasemodel.ScheduledJob) error {
	return nil
}

func (payloadItemMemory) BeginRelease(context.Context, payloadreleasemodel.OwnerRelease) error {
	return nil
}
