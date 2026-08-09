package accounts

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"retrom/internal/config"
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
		if !errors.Is(result.err, ErrRateLimited) || RateLimitRetryAfter(result.err) != 900 {
			t.Fatalf("concurrent threshold result = %v retry=%d", result.err, RateLimitRetryAfter(result.err))
		}
	}
	if _, err := fixture.service.LoginRateLimited(
		context.Background(), "test", "test", "192.0.2.10",
	); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("valid credentials during block = %v", err)
	}

	var rows, hashBytes int
	if err := fixture.database.SQL.QueryRow(`
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
	if err := fixture.database.SQL.QueryRow(`
SELECT count(*) FROM auth_rate_limits WHERE scope='LOGIN_ACCOUNT' AND subject_hash=?
`, accountHash[:]).Scan(&accountRows); err != nil || accountRows != 0 {
		t.Fatalf("successful account bucket clear = %d, %v", accountRows, err)
	}
	var ipRows int
	if err := fixture.database.SQL.QueryRow(`
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
		if attempt < 5 && !errors.Is(err, ErrInitializationProof) {
			t.Fatalf("setup failure %d = %v", attempt, err)
		}
		if attempt == 5 && (!errors.Is(err, ErrRateLimited) || RateLimitRetryAfter(err) != 900) {
			t.Fatalf("setup threshold = %v retry=%d", err, RateLimitRetryAfter(err))
		}
	}

	fixture := newAccountFixture(t, config.ModeTest)
	admin := authenticatedTestAdmin(t, fixture)
	invitation, _, err := fixture.service.CreateInvitation(
		context.Background(), admin.Principal, "USER", false, uuid.NewString(),
	)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= 20; attempt++ {
		_, inspectErr := fixture.service.InspectAccountLinkRateLimited(
			context.Background(), "INVITATION", "invalid", "203.0.113.7",
		)
		if attempt < 20 && !errors.Is(inspectErr, ErrAccountLinkUnavailable) {
			t.Fatalf("link failure %d = %v", attempt, inspectErr)
		}
		if attempt == 20 && !errors.Is(inspectErr, ErrRateLimited) {
			t.Fatalf("link threshold = %v", inspectErr)
		}
	}
	if _, err := fixture.service.InspectAccountLinkRateLimited(
		context.Background(), "INVITATION", invitation.CapabilityToken, "203.0.113.7",
	); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("valid link during IP block = %v", err)
	}
}
