package main

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"retrom/internal/composition"

	"retrom/internal/authn"
	"retrom/internal/cleanup"
	"retrom/internal/config"
	"retrom/internal/processlock"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/store"
	"retrom/internal/testassert"
)

func accountCommandFixture(t *testing.T, mode config.Mode) config.Maintenance {
	t.Helper()
	root := t.TempDir()
	databasePath := filepath.Join(root, "retrom.db")
	database, err := store.Open(context.Background(), databasePath, time.Now)
	testassert.False(t, err != nil, err)
	credentials, err := retromruntime.LoadOrCreateCredentials(root)
	testassert.False(t, err != nil, err)
	service, err := composition.NewAccounts(
		context.Background(), database.SQL, credentials, mode, authn.EmptyBlocklist{}, time.Now,
	)
	testassert.False(t, err != nil, err)
	if err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	return config.Maintenance{
		DataDir: root, DBPath: databasePath, DependencyRoot: filepath.Join(repositoryRoot, "data"),
	}
}

func TestResetOfflineAdminRequiresLockAndTTYConfirmation(t *testing.T) {
	t.Parallel()
	configuration := accountCommandFixture(t, config.ModeTest)
	lock, err := processlock.Acquire(configuration.DataDir)
	testassert.False(t, err != nil, err)
	readCount := 0
	if err := resetOfflineAdmin(
		context.Background(), configuration, "test", func(string) (string, error) {
			readCount++
			return "should not be read", nil
		},
	); !errors.Is(err, processlock.ErrAlreadyRunning) || readCount != 0 {
		t.Fatalf("online reset = %v reads=%d", err, readCount)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}

	passwords := []string{"an offline command password", "an offline command password"}
	prompts := make([]string, 0, 2)
	if err := resetOfflineAdmin(
		context.Background(), configuration, "test", func(prompt string) (string, error) {
			prompts = append(prompts, prompt)
			result := passwords[0]
			passwords = passwords[1:]
			return result, nil
		},
	); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return len(prompts) != 2 }, func() bool { return prompts[0] == prompts[1] }), "offline reset prompts = %#v", prompts)

	database, err := store.Open(context.Background(), configuration.DBPath, time.Now)
	testassert.False(t, err != nil, err)
	defer func() { cleanup.Error("close", database.Close()) }()
	credentials, err := retromruntime.LoadCredentials(configuration.DataDir)
	testassert.False(t, err != nil, err)
	service, err := composition.NewAccounts(
		context.Background(), database.SQL, credentials, config.ModeRelease, authn.EmptyBlocklist{}, time.Now,
	)
	testassert.False(t, err != nil, err)
	if err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Login(context.Background(), "test", "an offline command password"); err != nil {
		t.Fatalf("offline command credential = %v", err)
	}
}

func TestOfflineCommandArgumentsAreClosed(t *testing.T) {
	t.Parallel()
	reader := func(string) (string, error) { return "unused", nil }
	for _, arguments := range [][]string{
		{"admin-reset"}, {"admin-reset", "--username", "admin", "extra"},
	} {
		if err := executeWithPasswordReader(arguments, reader); err == nil {
			t.Fatalf("arguments %#v accepted", arguments)
		}
	}
}

func TestRemovedSetupCodeCommandIsRejected(t *testing.T) {
	t.Parallel()
	if err := execute([]string{"setup-code"}); !errors.Is(err, errCommand) {
		t.Fatalf("removed setup-code command = %v", err)
	}
}
