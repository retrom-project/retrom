package accounts

import (
	"context"
	"errors"
	"testing"
	"time"

	"retrom/internal/capability/security/authn"
)

type recoveryMemory struct {
	target    RecoveryTarget
	plan      RecoveryPlan
	writes    int
	lateError error
}

func (memory *recoveryMemory) ByUsername(context.Context, string) (RecoveryTarget, bool, error) {
	return memory.target, true, nil
}

func (memory *recoveryMemory) Current(context.Context, string) (RecoveryTarget, bool, error) {
	return memory.target, true, nil
}

func (memory *recoveryMemory) WithWrite(_ context.Context, work func(RecoveryScope) error) error {
	if err := work(RecoveryScope{Read: memory, Write: memory}); err != nil {
		return err
	}
	return memory.lateError
}

func (memory *recoveryMemory) Reset(_ context.Context, plan RecoveryPlan) error {
	memory.plan = plan
	memory.writes++
	return nil
}

type recoveryHasher struct{ duringHash func() }

func (recoveryHasher) Verify(context.Context, string, string) (bool, error) { return true, nil }
func (hasher recoveryHasher) Hash(context.Context, string) (string, error) {
	if hasher.duringHash != nil {
		hasher.duringHash()
	}
	return "replacement-hash", nil
}

func TestOfflineRecoveryRechecksAdminVersionAfterHashing(t *testing.T) {
	memory := &recoveryMemory{target: RecoveryTarget{UserID: "user", Username: "owner", DisplayName: "Owner", Role: "ADMIN", Status: "DISABLED", Version: 2}}
	hasher := recoveryHasher{duringHash: func() { memory.target.Version++ }}
	service := NewRecovery(memory, hasher, authn.EmptyBlocklist{}, time.Now)
	err := service.Reset(t.Context(), "owner", "replacement passphrase", "replacement passphrase")
	if !errors.Is(err, ErrOfflineAdmin) || memory.writes != 0 {
		t.Fatalf("reset changed admin: %v", err)
	}
}

func TestOfflineRecoveryPreservesCommitFailureAndTargetsExistingAdmin(t *testing.T) {
	memory := &recoveryMemory{target: RecoveryTarget{UserID: "user", Username: "owner", DisplayName: "Owner", Role: "ADMIN", Status: "DISABLED", Version: 2}, lateError: context.Canceled}
	service := NewRecovery(memory, recoveryHasher{}, authn.EmptyBlocklist{}, func() time.Time { return time.UnixMilli(100) })
	err := service.Reset(t.Context(), "owner", "replacement passphrase", "replacement passphrase")
	if !errors.Is(err, context.Canceled) || memory.writes != 1 || memory.plan.Target.UserID != "user" || memory.plan.Target.Version != 2 || memory.plan.Now != 100 {
		t.Fatalf("recovery commit: %+v / %v", memory.plan, err)
	}
}

func TestOfflineRecoveryRejectsDeletedOrNonAdminTarget(t *testing.T) {
	for _, target := range []RecoveryTarget{{Role: "ADMIN", Status: "DELETED"}, {Role: "USER", Status: "ENABLED"}} {
		memory := &recoveryMemory{target: target}
		err := NewRecovery(memory, nil, authn.EmptyBlocklist{}, time.Now).Reset(t.Context(), "owner", "replacement passphrase", "replacement passphrase")
		if !errors.Is(err, ErrOfflineAdmin) || memory.writes != 0 {
			t.Fatalf("invalid recovery %+v: %v", target, err)
		}
	}
}
