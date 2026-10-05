package idempotency

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type commandMemoryWriter struct {
	saved   []Receipt
	failure error
}

func (writer *commandMemoryWriter) EncodingContext(ctx context.Context) context.Context { return ctx }

func (writer *commandMemoryWriter) SaveCommand(_ context.Context, _ Request, receipt Receipt, _, _ int64) error {
	if writer.failure != nil {
		return writer.failure
	}
	writer.saved = append(writer.saved, receipt)
	return nil
}

func testCommand(t *testing.T) (context.Context, *Command) {
	t.Helper()
	return NewCommand(t.Context(), Request{PrincipalID: "user", OperationID: "create", Key: "key", Digest: strings.Repeat("a", 64)},
		func(context.Context, any) (Receipt, error) {
			return Receipt{HTTPStatus: 201, HeadersJSON: "{}", Body: []byte("{}\n")}, nil
		}, 100)
}

func TestCommandCompletionTracksTransactionOutcome(t *testing.T) {
	ctx, command := testCommand(t)
	if err := Complete(ctx, Result{}); !errors.Is(err, ErrCommandIncomplete) {
		t.Fatalf("completion without scope: %v", err)
	}
	writer := &commandMemoryWriter{}
	command.Begin(writer)
	if err := Complete(ctx, Result{}); err != nil {
		t.Fatal(err)
	}
	if _, found := command.Receipt(); found {
		t.Fatal("receipt visible before commit")
	}
	if err := Complete(ctx, Result{}); !errors.Is(err, ErrCommandIncomplete) {
		t.Fatalf("second completion: %v", err)
	}
	command.End(false)
	if _, found := command.Receipt(); found {
		t.Fatal("rolled back receipt became visible")
	}
	command.Begin(writer)
	if err := Complete(ctx, Result{}); err != nil {
		t.Fatal(err)
	}
	command.End(true)
	receipt, found := command.Receipt()
	if !found || receipt.RequestDigest != strings.Repeat("a", 64) || len(writer.saved) != 2 {
		t.Fatalf("committed receipt: %#v / %v", receipt, found)
	}
}

func TestCommandCompletionPropagatesReceiptFailure(t *testing.T) {
	ctx, command := testCommand(t)
	cause := errors.New("receipt unavailable")
	command.Begin(&commandMemoryWriter{failure: cause})
	if err := Complete(ctx, Result{}); !errors.Is(err, cause) {
		t.Fatalf("receipt failure lost: %v", err)
	}
	command.End(false)
	if _, found := command.Receipt(); found {
		t.Fatal("failed receipt reported committed")
	}
}

func TestCommandCompletionRejectsInvalidResponse(t *testing.T) {
	cases := []Receipt{{HTTPStatus: 500, HeadersJSON: "{}"}, {HTTPStatus: 201, HeadersJSON: "{}", Body: make([]byte, (1<<20)+1)}}
	for _, receipt := range cases {
		ctx, command := NewCommand(t.Context(), Request{}, func(context.Context, any) (Receipt, error) { return receipt, nil }, 100)
		writer := &commandMemoryWriter{}
		command.Begin(writer)
		if err := Complete(ctx, Result{}); !errors.Is(err, ErrInvalidReceipt) {
			t.Fatalf("invalid response accepted: %v", err)
		}
		if len(writer.saved) != 0 {
			t.Fatal("invalid response reached storage")
		}
	}
}

func TestDetachedWorkCannotCompleteParentCommand(t *testing.T) {
	ctx, command := testCommand(t)
	writer := &commandMemoryWriter{}
	command.Begin(writer)
	detached := WithoutCommand(ctx)
	if Active(detached) || RequestFromContext(detached) != nil {
		t.Fatal("detached work retained command")
	}
	if err := Complete(detached, Result{}); err != nil || len(writer.saved) != 0 {
		t.Fatal("detached completion wrote parent receipt")
	}
	if err := Complete(ctx, Result{}); err != nil {
		t.Fatal(err)
	}
}

func TestIdentityCoordinationIsScopedAndCancellationReleasesReferences(t *testing.T) {
	var group gates
	identity := Request{PrincipalID: "a", OperationID: "create", Key: "key", Digest: "one"}
	release, err := group.acquire(t.Context(), identity)
	if err != nil {
		t.Fatal(err)
	}
	different := identity
	different.PrincipalID = "b"
	releaseOther, err := group.acquire(t.Context(), different)
	if err != nil {
		t.Fatal(err)
	}
	releaseOther()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	identity.Digest = "different"
	if _, err := group.acquire(ctx, identity); !errors.Is(err, context.Canceled) {
		t.Fatalf("conflicting input bypassed identity gate: %v", err)
	}
	release()
	if len(group.keys) != 0 {
		t.Fatal("identity coordination leaked references")
	}
}

func TestFrozenCommandPreservesOriginalIdentityAndLifetime(t *testing.T) {
	ctx, command := testCommand(t)
	command.Begin(&commandMemoryWriter{})
	frozen, err := Freeze(ctx, Result{})
	if err != nil || frozen == nil {
		t.Fatalf("freeze: %v", err)
	}
	if frozen.CreatedAtMS != 100 || frozen.ExpiresAtMS != 86_400_100 || !SameRequest(ctx, frozen.Request) {
		t.Fatalf("frozen command: %#v", frozen)
	}
	command.End(true)
	if _, found := command.Receipt(); found {
		t.Fatal("freezing intent completed receipt before publication")
	}
}
