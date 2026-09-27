package releaseeffects

import (
	"context"
	"fmt"

	application "retrom/internal/service/payloadrelease"
)

func (records records) Payload(ctx context.Context, scope application.Scope) (application.EffectPayload, error) {
	if records.domain.Payload == nil {
		return application.EffectPayload{}, application.ErrScopeInvalid
	}
	value, err := records.domain.Payload(ctx, scope)
	if err != nil {
		return value, fmt.Errorf("read domain payload: %w", err)
	}
	return value, nil
}

func (records records) Links(ctx context.Context, scope application.Scope) ([]application.Scope, error) {
	if records.domain.Links == nil {
		return nil, application.ErrScopeInvalid
	}
	value, err := records.domain.Links(ctx, scope)
	if err != nil {
		return nil, fmt.Errorf("read domain children: %w", err)
	}
	return value, nil
}

func (records records) Remaining(ctx context.Context, scope application.Scope) (int64, error) {
	if records.domain.Remaining == nil {
		return 0, application.ErrScopeInvalid
	}
	value, err := records.domain.Remaining(ctx, scope)
	if err != nil {
		return 0, fmt.Errorf("read remaining domain payload: %w", err)
	}
	return value, nil
}

func (records records) Mutations(ctx context.Context, scope application.Scope) (int64, error) {
	return Mutations(ctx, records.executor, scope)
}
