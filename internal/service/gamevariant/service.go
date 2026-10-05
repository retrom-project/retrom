package gamevariant

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"retrom/internal/service/idempotency"

	"retrom/internal/content/arcade"
	"retrom/internal/format/arcadedat"
)

type EnsureScope struct {
	Read  func(context.Context, string, string) (Snapshot, error)
	Write WriteScope
}
type Repository interface {
	Snapshot(context.Context, string, string) (Snapshot, error)
	WithEnsure(context.Context, func(EnsureScope) error) error
}
type Provider interface {
	BundleSHA256(string, string) (string, bool)
}

type ArcadePreparationReader interface {
	Prepare(context.Context, []arcade.SourceFile, string, string, string) (arcade.Result, error)
}

type Service struct {
	repository Repository
	provider   Provider
	now        func() time.Time
	supervisor *ValidationSupervisor
	arcade     ArcadePreparationReader
}

func New(repository Repository, provider Provider, now func() time.Time, supervisor *ValidationSupervisor,
	arcadePreparation ArcadePreparationReader,
) *Service {
	return &Service{
		repository: repository, provider: provider, now: now, supervisor: supervisor, arcade: arcadePreparation,
	}
}

// Ensure prepares a game/core configuration. Its caller dispatches only after
// its own operation or idempotency receipt has committed.
func (s *Service) Ensure(ctx context.Context, gameID, coreID string) (Result, error) {
	before, err := s.repository.Snapshot(ctx, gameID, coreID)
	if err != nil {
		return Result{}, fmt.Errorf("read variant configuration: %w", err)
	}
	if !before.Found || s.provider == nil {
		return Result{}, ErrBlocked
	}
	bundle, ok := s.provider.BundleSHA256(before.Source.ProviderID, before.Source.TargetID)
	if !ok || bundle != before.Source.BundleSHA256 {
		return Result{}, ErrBlocked
	}
	var prepared *ArcadePreparation
	if before.Source.VariantID == "" && arcadedat.SupportsCore(before.Source.CoreID) {
		prepared, err = s.PrepareArcade(ctx, before)
		if err != nil {
			return Result{}, err
		}
	}
	var result Result
	err = s.repository.WithEnsure(ctx, func(scope EnsureScope) error {
		current, err := scope.Read(ctx, gameID, coreID)
		if err != nil {
			return fmt.Errorf("read final variant inputs: %w", err)
		}
		if !SameValidationInputs(before, current) {
			return ErrBlocked
		}
		now := s.now().UnixMilli()
		if now < 0 {
			return ErrBlocked
		}
		result, err = Schedule(ctx, scope.Write, current, now, newVariantID, prepared, scope.Read)
		if err != nil {
			return err
		}
		return completeMovePreview(ctx, result)
	})
	if err != nil {
		return Result{}, fmt.Errorf("ensure variant: %w", err)
	}
	return result, nil
}

func SameValidationInputs(before, after Snapshot) bool {
	if !before.Found || !after.Found {
		return false
	}
	for _, s := range []*Snapshot{&before, &after} {
		s.Source.VariantID = ""
		s.Source.VariantStatus = ""
		s.Source.DependencySnapshot = ""
		s.Source.CompatibilityCode = ""
		s.Source.DATVersionID = nil
		s.VariantFiles = nil
		s.BIOS = BIOSFacts{}
	}
	return reflect.DeepEqual(before, after)
}
func (s *Service) Dispatch(ctx context.Context, id string) { s.supervisor.Dispatch(ctx, id) }
func (s *Service) Resume(ctx context.Context, id string)   { s.supervisor.Resume(ctx, id) }
func (s *Service) Recover(ctx context.Context) error       { return s.supervisor.Recover(ctx) }
func (s *Service) Close()                                  { s.supervisor.Close() }

func (s *Service) Stop() { s.supervisor.Stop() }
func (s *Service) Wait() { s.supervisor.Wait() }

func completeMovePreview(ctx context.Context, result Result) error {
	command := idempotency.RequestFromContext(ctx)
	if command == nil || command.OperationID != "postAdminGameMovePreview" || result.Ready {
		return nil
	}
	if err := idempotency.Complete(ctx, idempotency.Result{
		Value:    map[string]any{"status": "VALIDATION_PENDING", "jobId": result.JobID, "retryAfterMs": result.RetryAfterMS},
		Accepted: true,
	}); err != nil {
		return fmt.Errorf("complete command receipt: %w", err)
	}
	return nil
}
