package readiness

import (
	"context"
	"errors"
	"testing"
)

type repositoryStub struct {
	status Status
	err    error
}

func (stub repositoryStub) Check(context.Context) (Status, error) {
	return stub.status, stub.err
}

func TestReasonMapsDependencyStates(t *testing.T) {
	tests := []struct {
		name   string
		status Status
		want   string
	}{
		{name: "ready", want: ""},
		{name: "indexing", status: Status{Missing: 2}, want: "DEPENDENCY_INDEXING"},
		{name: "parse failed", status: Status{Missing: 2, Failed: 1}, want: "DEPENDENCY_DAT_PARSE_FAILED"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := New(repositoryStub{status: test.status}).Reason(context.Background()); got != test.want {
				t.Fatalf("reason = %q, want %q", got, test.want)
			}
		})
	}
}

func TestReasonMapsRepositoryErrorsToDatabaseUnavailable(t *testing.T) {
	if got := New(repositoryStub{err: errors.New("probe failed")}).Reason(context.Background()); got != "DATABASE_UNAVAILABLE" {
		t.Fatalf("reason = %q", got)
	}
	if got := (*Service)(nil).Reason(context.Background()); got != "DATABASE_UNAVAILABLE" {
		t.Fatalf("nil service reason = %q", got)
	}
}
