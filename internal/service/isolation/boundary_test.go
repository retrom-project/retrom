package isolation

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"
)

type isolationMemory struct {
	bootstrap                       Bootstrap
	capability                      Capability
	reads, writes, consumed, issued int
}

func (memory *isolationMemory) Bootstrap(context.Context, TicketQuery) (Bootstrap, error) {
	memory.reads++
	return memory.bootstrap, nil
}

func (memory *isolationMemory) Capability(context.Context, CredentialQuery) (Capability, error) {
	memory.reads++
	return memory.capability, nil
}
func (memory *isolationMemory) Revoke(context.Context, Access, int64) error { return nil }

func (memory *isolationMemory) ConsumeAndIssue(
	_ context.Context, cmd ConsumeAndIssueCommand,
) (ConsumeAndIssueResult, error) {
	memory.writes++
	if memory.bootstrap.Consumed || memory.bootstrap.ExpiresAtMS <= cmd.NowMS ||
		!ActiveSession(memory.bootstrap.Session, cmd.NowMS) {
		return ConsumeAndIssueResult{}, ErrCredential
	}
	memory.consumed++
	memory.issued++
	access := Access{
		LaunchID: cmd.LaunchID, Origin: cmd.Origin,
		Profile:       memory.bootstrap.Session.Profile,
		ContentFormat: memory.bootstrap.Session.ContentFormat,
		Preview:       memory.bootstrap.Session.Preview,
		Expires:       memory.bootstrap.Session.HardExpiresAtMS,
	}
	return ConsumeAndIssueResult{Access: access}, nil
}

func TestInvalidCredentialsDoNotReachRepository(t *testing.T) {
	t.Parallel()
	memory := &isolationMemory{}
	service := New(memory, "https://{launchId}.runtime.test", time.Now)
	for _, token := range []string{"", "invalid", base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 31))} {
		if _, _, err := service.ConsumeTicket(t.Context(), "launch", "origin", token); !errors.Is(err, ErrCredential) {
			t.Fatal(err)
		}
		if _, err := service.Authenticate(t.Context(), "launch", "origin", token); !errors.Is(err, ErrCredential) {
			t.Fatal(err)
		}
	}
	if memory.reads != 0 || memory.writes != 0 {
		t.Fatalf("reads=%d writes=%d", memory.reads, memory.writes)
	}
}

func TestBootstrapEligibilityPrecedesConsumption(t *testing.T) {
	t.Parallel()
	active := RuntimeSession{State: "ACTIVE", HardExpiresAtMS: 200, ContentFormat: "RPG_MAKER_PROJECT"}
	cases := []Bootstrap{
		{Session: active, Consumed: true, ExpiresAtMS: 150},
		{Session: active, ExpiresAtMS: 100},
		{Session: RuntimeSession{State: "ACTIVE", HardExpiresAtMS: 100, ContentFormat: "RPG_MAKER_PROJECT"}, ExpiresAtMS: 150},
		{Session: RuntimeSession{State: "REVOKED", HardExpiresAtMS: 200, ContentFormat: "RPG_MAKER_PROJECT"}, ExpiresAtMS: 150},
		{Session: RuntimeSession{State: "ACTIVE", HardExpiresAtMS: 200, ContentFormat: "OTHER"}, ExpiresAtMS: 150},
	}
	for _, bootstrap := range cases {
		memory := &isolationMemory{bootstrap: bootstrap}
		service := New(memory, "https://{launchId}.runtime.test", func() time.Time { return time.UnixMilli(100) })
		if _, err := service.InspectBootstrap(t.Context(), "launch", "origin"); !errors.Is(err, ErrCredential) {
			t.Fatalf("inspect=%v bootstrap=%+v", err, bootstrap)
		}
		token := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
		if _, _, err := service.ConsumeTicket(t.Context(), "launch", "origin", token); !errors.Is(err, ErrCredential) {
			t.Fatalf("consume=%v bootstrap=%+v", err, bootstrap)
		}
		if memory.consumed != 0 || memory.issued != 0 {
			t.Fatalf("consumed=%d issued=%d", memory.consumed, memory.issued)
		}
	}
}

func TestCapabilityChecksRevocationAndBothExpiries(t *testing.T) {
	t.Parallel()
	for _, capability := range []Capability{
		{Revoked: true, ExpiresAtMS: 200, Session: RuntimeSession{State: "ACTIVE", HardExpiresAtMS: 200, ContentFormat: "TYRANOSCRIPT_PROJECT"}},
		{ExpiresAtMS: 100, Session: RuntimeSession{State: "ACTIVE", HardExpiresAtMS: 200, ContentFormat: "TYRANOSCRIPT_PROJECT"}},
		{ExpiresAtMS: 200, Session: RuntimeSession{State: "ACTIVE", HardExpiresAtMS: 100, ContentFormat: "TYRANOSCRIPT_PROJECT"}},
	} {
		memory := &isolationMemory{capability: capability}
		service := New(memory, "https://{launchId}.runtime.test", func() time.Time { return time.UnixMilli(100) })
		token := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
		if _, err := service.Authenticate(t.Context(), "launch", "origin", token); !errors.Is(err, ErrCredential) {
			t.Fatalf("error=%v capability=%+v", err, capability)
		}
	}
}
