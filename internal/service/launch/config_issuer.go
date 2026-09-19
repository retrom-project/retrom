package launch

import (
	"context"
	"crypto/subtle"
	"fmt"
	"math"
	"reflect"

	model "retrom/internal/model/launch"
)

type ConfigIssuer struct {
	repository     model.ConfigRepository
	runtimeBuilder model.ConfigBuilder
	environment    ConfigEnvironment
}

func NewConfigIssuer(
	repository model.ConfigRepository,
	builder model.ConfigBuilder,
	environment ConfigEnvironment,
) *ConfigIssuer {
	return &ConfigIssuer{repository: repository, runtimeBuilder: builder, environment: environment}
}

func (service *ConfigIssuer) Issue(ctx context.Context, ref model.SessionRef, capability string) (Config, error) {
	snapshot, found, err := service.repository.Load(ctx, ref, func(source model.ConfigSource) error {
		if !service.authorized(ref, source, capability, service.environment.Now().UnixMilli()) {
			return model.ErrCredential
		}
		return nil
	})
	if err != nil {
		return Config{}, fmt.Errorf("read config snapshot: %w", err)
	}
	if !found {
		return Config{}, model.ErrCredential
	}
	ticket, err := service.isolationTicket(ref.ID, snapshot.Authority)
	if err != nil {
		return Config{}, err
	}
	configuration, err := service.envelope(ref.ID, snapshot, ticket)
	if err != nil {
		return Config{}, err
	}
	err = service.repository.WithActivation(ctx, func(transaction model.ConfigActivation) error {
		return service.activate(ctx, transaction, ref, capability, snapshot.Authority, ticket)
	})
	if err != nil {
		return Config{}, fmt.Errorf("finalize config: %w", err)
	}
	return configuration, nil
}

func (service *ConfigIssuer) activate(
	ctx context.Context, transaction model.ConfigActivation, ref model.SessionRef, capability string,
	expected model.ConfigAuthority, ticket model.IsolationTicket,
) error {
	current, found, err := transaction.Current(ctx, ref)
	if err != nil {
		return fmt.Errorf("read final config authority: %w", err)
	}
	now := service.environment.Now().UnixMilli()
	if !found || !service.authorized(ref, current.Source, capability, now) ||
		!sameConfigInput(expected, current) || !validConfigRevision(expected.Source, current.Source) {
		return model.ErrCredential
	}
	if ticket.Origin != "" && !validIsolationGrant(current.Isolation, ticket, now) {
		return model.ErrBlocked
	}
	if current.Source.State == "ACTIVE" {
		return nil
	}
	if current.Source.Version == math.MaxInt64 {
		return model.ErrCredential
	}
	if err := transaction.Activate(
		ctx, model.ConfigActivationPlan{Ref: ref, Version: current.Source.Version, NowMS: now},
	); err != nil {
		return fmt.Errorf("activate config: %w", err)
	}
	return nil
}

func (service *ConfigIssuer) authorized(
	ref model.SessionRef,
	source model.ConfigSource,
	capability string,
	now int64,
) bool {
	purpose := "PRODUCT"
	if ref.Preview {
		purpose = "REVIEW_PREVIEW"
	}
	return source.Purpose == purpose && validConfigLifetime(source, now) &&
		service.environment.Matches != nil && service.environment.Matches(capability, source.CredentialHash)
}

func validConfigLifetime(source model.ConfigSource, now int64) bool {
	if source.Version < 1 || source.HardEnd <= now {
		return false
	}
	if source.State == "CREATED" {
		return source.BootstrapEnd > now
	}
	return source.State == "ACTIVE" && (source.IdleEnd == nil || *source.IdleEnd > now)
}

func validConfigRevision(before, after model.ConfigSource) bool {
	if before.State == "ACTIVE" {
		return after.State == "ACTIVE" && after.Version >= before.Version
	}
	return after.State == "CREATED" && after.Version == before.Version ||
		after.State == "ACTIVE" && after.Version > before.Version
}

func sameConfigInput(before, after model.ConfigAuthority) bool {
	left, right := before.Source, after.Source
	left.State, right.State = "", ""
	left.Version, right.Version = 0, 0
	// Heartbeats change idle deadlines but not the immutable configuration.
	left.IdleEnd, right.IdleEnd = nil, nil
	return reflect.DeepEqual(left, right) && before.Restore == after.Restore
}

func (service *ConfigIssuer) isolationTicket(id string, authority model.ConfigAuthority) (
	model.IsolationTicket,
	error,
) {
	if authority.Source.Delivery != "ISOLATED_WEB_PROJECT" {
		return model.IsolationTicket{}, nil
	}
	if service.environment.SignIsolation == nil {
		return model.IsolationTicket{}, model.ErrBlocked
	}
	ticket, err := service.environment.SignIsolation(id)
	if err != nil {
		return model.IsolationTicket{}, fmt.Errorf("sign isolation ticket: %w", err)
	}
	if !validIsolationGrant(authority.Isolation, ticket, service.environment.Now().UnixMilli()) {
		return model.IsolationTicket{}, model.ErrBlocked
	}
	return ticket, nil
}

func validIsolationGrant(grants []model.IsolationGrant, ticket model.IsolationTicket, now int64) bool {
	for _, grant := range grants {
		if grant.Origin == ticket.Origin && grant.ExpiresAtMS > now &&
			(len(grant.TicketHash) == 0 || subtle.ConstantTimeCompare(grant.TicketHash, ticket.Hash[:]) == 1) {
			return true
		}
	}
	return false
}
