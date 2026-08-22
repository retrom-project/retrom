package accounts

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"retrom/internal/config"
	"retrom/internal/testassert"
)

func TestLoginRateLimitIsAtomicHashedAndExpiresWithInjectedClock(t *testing.T) {
	t.Parallel()
	fixture := newAccountFixture(t, config.ModeTest)
	if err := fixture.service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt < 5; attempt++ {
		if _, err := fixture.service.LoginRateLimited(
			context.Background(), "test", "wrong password", "192.0.2.10",
		); !errors.Is(err, ErrAuthentication) {
			t.Fatalf("login failure %d = %v", attempt, err)
		}
	}

	type outcome struct{ err error }
	outcomes := make(chan outcome, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := fixture.service.LoginRateLimited(
				context.Background(), "test", "wrong password", "192.0.2.10",
			)
			outcomes <- outcome{err: err}
		}()
	}
	wait.Wait()
	close(outcomes)
	for result := range outcomes {
		testassert.Falsef(t, testassert.Any(func() bool { return !errors.Is(result.err, ErrRateLimited) }, func() bool { return RateLimitRetryAfter(result.err) != 900 }), "concurrent threshold result = %v retry=%d", result.err, RateLimitRetryAfter(result.err))
	}
	if _, err := fixture.service.LoginRateLimited(
		context.Background(), "test", "test", "192.0.2.10",
	); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("valid credentials during block = %v", err)
	}

	var rows, hashBytes int
	if err := fixture.database.SQL.QueryRowContext(context.Background(), `
SELECT count(*),min(length(subject_hash)) FROM auth_rate_limits
`).Scan(&rows, &hashBytes); err != nil || rows != 2 || hashBytes != 32 {
		t.Fatalf("rate-limit storage = rows=%d hashBytes=%d error=%v", rows, hashBytes, err)
	}
	*fixture.now = fixture.now.Add(15 * time.Minute)
	if _, err := fixture.service.LoginRateLimited(
		context.Background(), "test", "test", "192.0.2.10",
	); err != nil {
		t.Fatalf("login after block expiry = %v", err)
	}
	accountHash := fixture.credentials.RateLimitSubject("LOGIN_ACCOUNT", "test")
	var accountRows int
	if err := fixture.database.SQL.QueryRowContext(context.Background(), `
SELECT count(*) FROM auth_rate_limits WHERE scope='LOGIN_ACCOUNT' AND subject_hash=?
`, accountHash[:]).Scan(&accountRows); err != nil || accountRows != 0 {
		t.Fatalf("successful account bucket clear = %d, %v", accountRows, err)
	}
	var ipRows int
	if err := fixture.database.SQL.QueryRowContext(context.Background(), `
SELECT count(*) FROM auth_rate_limits WHERE scope='LOGIN_IP'
`).Scan(&ipRows); err != nil || ipRows != 1 {
		t.Fatalf("successful login cleared IP bucket = %d, %v", ipRows, err)
	}
}

func TestSetupAndLinkRateLimitsUseIndependentIPBuckets(t *testing.T) {
	t.Parallel()
	release := newAccountFixture(t, config.ModeRelease)
	if err := release.service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	invalidSetup := InitializeRequest{
		SetupCode: "invalid", Username: "admin", DisplayName: "Administrator",
		Password: "a sufficiently long phrase", PasswordConfirmation: "a sufficiently long phrase",
	}
	for attempt := 1; attempt <= 5; attempt++ {
		_, err := release.service.InitializeRateLimited(context.Background(), invalidSetup, "198.51.100.5")
		testassert.Falsef(t, testassert.All(func() bool { return attempt < 5 }, func() bool { return !errors.Is(err, ErrInitializationProof) }), "setup failure %d = %v", attempt, err)
		testassert.Falsef(t, testassert.All(func() bool { return attempt == 5 }, func() bool { return (!errors.Is(err, ErrRateLimited) || RateLimitRetryAfter(err) != 900) }), "setup threshold = %v retry=%d", err, RateLimitRetryAfter(err))
	}

	fixture := newAccountFixture(t, config.ModeTest)
	admin := authenticatedTestAdmin(t, fixture)
	invitation, _, err := fixture.service.CreateInvitation(
		context.Background(), admin.Principal, "USER", false, uuid.NewString(),
	)
	testassert.False(t, err != nil, err)
	for attempt := 1; attempt <= 20; attempt++ {
		_, inspectErr := fixture.service.InspectAccountLinkRateLimited(
			context.Background(), "INVITATION", "invalid", "203.0.113.7",
		)
		testassert.Falsef(t, testassert.All(func() bool { return attempt < 20 }, func() bool { return !errors.Is(inspectErr, ErrAccountLinkUnavailable) }), "link failure %d = %v", attempt, inspectErr)
		testassert.Falsef(t, testassert.All(func() bool { return attempt == 20 }, func() bool { return !errors.Is(inspectErr, ErrRateLimited) }), "link threshold = %v", inspectErr)
	}
	if _, err := fixture.service.InspectAccountLinkRateLimited(
		context.Background(), "INVITATION", invitation.CapabilityToken, "203.0.113.7",
	); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("valid link during IP block = %v", err)
	}
}
