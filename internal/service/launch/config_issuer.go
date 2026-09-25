package launch

import (
	"context"
	"crypto/subtle"
	"fmt"
	"math"
	"reflect"
)

type ConfigIssuer struct {
	repository     ConfigRepository
	runtimeBuilder ConfigBuilder
	environment    ConfigEnvironment
}

func NewConfigIssuer(repository ConfigRepository, builder ConfigBuilder, environment ConfigEnvironment) *ConfigIssuer {
	return &ConfigIssuer{repository: repository, runtimeBuilder: builder, environment: environment}
}

func (service *ConfigIssuer) Issue(ctx context.Context, ref SessionRef, capability string) (Config, error) {
	snapshot, found, err := service.repository.Load(ctx, ref, func(source ConfigSource) error {
		if !service.authorized(ref, source, capability, service.environment.Now().UnixMilli()) {
			return ErrCredential
		}
		return nil
	})
	if err != nil {
		return Config{}, fmt.Errorf("read config snapshot: %w", err)
	}
	if !found {
		return Config{}, ErrCredential
	}
	ticket, err := service.isolationTicket(ref.ID, snapshot.Authority)
	if err != nil {
		return Config{}, err
	}
	configuration, err := service.envelope(ref.ID, snapshot, ticket)
	if err != nil {
		return Config{}, err
	}
	err = service.repository.WithActivation(ctx, func(transaction ConfigActivation) error {
		return service.activate(ctx, transaction, ref, capability, snapshot.Authority, ticket)
	})
	if err != nil {
		return Config{}, fmt.Errorf("finalize config: %w", err)
	}
	return configuration, nil
}

func (service *ConfigIssuer) activate(
	ctx context.Context, transaction ConfigActivation, ref SessionRef, capability string,
	expected ConfigAuthority, ticket IsolationTicket,
) error {
	current, found, err := transaction.Current(ctx, ref)
	if err != nil {
		return fmt.Errorf("read final config authority: %w", err)
	}
	now := service.environment.Now().UnixMilli()
	if !found || !service.authorized(ref, current.Source, capability, now) ||
		!sameConfigInput(expected, current) || !validConfigRevision(expected.Source, current.Source) {
		return ErrCredential
	}
	if ticket.Origin != "" && !validIsolationGrant(current.Isolation, ticket, now) {
		return ErrBlocked
	}
	if current.Source.State == "ACTIVE" {
		return nil
	}
	if current.Source.Version == math.MaxInt64 {
		return ErrCredential
	}
	if err := transaction.Activate(
		ctx,
		ConfigActivationPlan{Ref: ref, Version: current.Source.Version, NowMS: now},
	); err != nil {
		return fmt.Errorf("activate config: %w", err)
	}
	return nil
}

func (service *ConfigIssuer) authorized(ref SessionRef, source ConfigSource, capability string, now int64) bool {
	purpose := "PRODUCT"
	if ref.Preview {
		purpose = "REVIEW_PREVIEW"
	}
	return source.Purpose == purpose && validConfigLifetime(source, now) &&
		service.environment.Matches != nil && service.environment.Matches(capability, source.CredentialHash)
}

func validConfigLifetime(source ConfigSource, now int64) bool {
	if source.Version < 1 || source.HardEnd <= now {
		return false
	}
	if source.State == "CREATED" {
		return source.BootstrapEnd > now
	}
	return source.State == "ACTIVE"
}

func validConfigRevision(before, after ConfigSource) bool {
	if before.State == "ACTIVE" {
		return after.State == "ACTIVE" && after.Version >= before.Version
	}
	return after.State == "CREATED" && after.Version == before.Version ||
		after.State == "ACTIVE" && after.Version > before.Version
}

func sameConfigInput(before, after ConfigAuthority) bool {
	left, right := before.Source, after.Source
	left.State, right.State = "", ""
	left.Version, right.Version = 0, 0
	// Heartbeats change idle deadlines but not the immutable configuration.
	left.IdleEnd, right.IdleEnd = nil, nil
	return reflect.DeepEqual(left, right) && before.Restore == after.Restore
}

func (service *ConfigIssuer) isolationTicket(id string, authority ConfigAuthority) (IsolationTicket, error) {
	if authority.Source.Delivery != "ISOLATED_WEB_PROJECT" {
		return IsolationTicket{}, nil
	}
	if service.environment.SignIsolation == nil {
		return IsolationTicket{}, ErrBlocked
	}
	ticket, err := service.environment.SignIsolation(id)
	if err != nil {
		return IsolationTicket{}, err
	}
	if !validIsolationGrant(authority.Isolation, ticket, service.environment.Now().UnixMilli()) {
		return IsolationTicket{}, ErrBlocked
	}
	return ticket, nil
}

func validIsolationGrant(grants []IsolationGrant, ticket IsolationTicket, now int64) bool {
	for _, grant := range grants {
		if grant.Origin == ticket.Origin && grant.ExpiresAtMS > now &&
			(len(grant.TicketHash) == 0 || subtle.ConstantTimeCompare(grant.TicketHash, ticket.Hash[:]) == 1) {
			return true
		}
	}
	return false
}
