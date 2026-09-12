package accounts

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type linkMemory struct {
	record           LinkRecord
	query            LinkQuery
	readErr, lateErr error
	reads, writes    int
	revocation       LinkRevocation
	audits           []AccountAudit
}

func (memory *linkMemory) Current(context.Context, string) (LinkRecord, bool, error) {
	memory.reads++
	return memory.record, true, memory.readErr
}

func (memory *linkMemory) List(_ context.Context, query LinkQuery) ([]LinkRecord, error) {
	memory.query = query
	return []LinkRecord{memory.record}, memory.readErr
}

func (memory *linkMemory) WithWrite(_ context.Context, work func(LinkScope) error) error {
	if err := work(LinkScope{Read: memory, Write: memory}); err != nil {
		return err
	}
	return memory.lateErr
}

func (memory *linkMemory) Replay(context.Context, AccountOperation) (AccountReplay, error) {
	return AccountReplay{}, nil
}

func (memory *linkMemory) Revoke(_ context.Context, plan LinkRevocation) error {
	memory.writes++
	memory.revocation = plan
	return nil
}

func (memory *linkMemory) Audit(_ context.Context, audit AccountAudit) error {
	memory.audits = append(memory.audits, audit)
	return nil
}
func (memory *linkMemory) Remember(context.Context, AccountReceipt) error { return nil }

type linkTokens struct{ valid bool }

func (tokens linkTokens) ParseAccountLinkToken(string, string) (uuid.UUID, bool) {
	return uuid.Nil, tokens.valid
}

func linkFixture() (*LinkService, *linkMemory) {
	memory := &linkMemory{record: LinkRecord{Link: AccountLink{AccountLinkID: "link", Kind: "INVITATION", Version: 1, ExpiresAtMS: 200}}}
	return NewLinks(memory, linkTokens{true}, func() time.Time { return time.UnixMilli(100) }), memory
}

func TestLinkInspectionPreservesStorageFailure(t *testing.T) {
	service, memory := linkFixture()
	memory.readErr = context.Canceled
	_, err := service.Inspect(t.Context(), "INVITATION", "token")
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrAccountLinkUnavailable) {
		t.Fatalf("storage cause: %v", err)
	}
}

func TestLinkInspectionRejectsInvalidTokenBeforeRead(t *testing.T) {
	service, memory := linkFixture()
	service.tokens = linkTokens{}
	_, err := service.Inspect(t.Context(), "INVITATION", "bad")
	if !errors.Is(err, ErrAccountLinkUnavailable) || memory.reads != 0 {
		t.Fatalf("invalid token read database: %v", err)
	}
}

func TestLinkStateUsesOneClockAndTerminalPrecedence(t *testing.T) {
	stamp := int64(50)
	for _, test := range []struct {
		name              string
		consumed, revoked *int64
		expires           int64
		want              string
	}{
		{"consumed", &stamp, &stamp, 90, "CONSUMED"}, {"revoked", nil, &stamp, 90, "REVOKED"}, {"expired", nil, nil, 100, "EXPIRED"}, {"active", nil, nil, 101, "ACTIVE"},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, memory := linkFixture()
			memory.record.Link.ConsumedAtMS = test.consumed
			memory.record.Link.RevokedAtMS = test.revoked
			memory.record.Link.ExpiresAtMS = test.expires
			links, err := service.List(t.Context(), LinkListFilter{Kind: "INVITATION", State: "ALL"})
			if err != nil {
				t.Fatal(err)
			}
			if len(links) != 1 || links[0].State != test.want || memory.query.Now != 100 {
				t.Fatalf("link state: %+v", links)
			}
		})
	}
}

func TestLinkRevocationRejectsExpiredAndStaleVersions(t *testing.T) {
	for _, test := range []struct {
		name             string
		expires, version int64
		want             error
	}{
		{"expired", 100, 1, ErrAccountLinkNotActive}, {"version", 200, 2, ErrUserVersion},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, memory := linkFixture()
			memory.record.Link.ExpiresAtMS = test.expires
			_, err := service.Revoke(t.Context(), "actor", "link", test.version, "key")
			if !errors.Is(err, test.want) || memory.writes != 0 {
				t.Fatalf("revocation guard: %v", err)
			}
		})
	}
}

func TestLinkRevocationLateFailurePreservesCause(t *testing.T) {
	service, memory := linkFixture()
	memory.lateErr = context.Canceled
	replayed, err := service.Revoke(t.Context(), "actor", "link", 1, "key")
	if !errors.Is(err, context.Canceled) || replayed {
		t.Fatalf("late revocation: %v replay=%v", err, replayed)
	}
	if memory.writes != 1 || len(memory.audits) != 1 || memory.audits[0].Action != "INVITATION_REVOKED" {
		t.Fatal("revocation and audit were not in one scope")
	}
}
