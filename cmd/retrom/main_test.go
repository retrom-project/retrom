package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"retrom/internal/accounts"
	"retrom/internal/authn"
	"retrom/internal/cleanup"
	"retrom/internal/config"
	"retrom/internal/processlock"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/store"
)

func accountCommandFixture(t *testing.T, mode config.Mode) (config.Maintenance, *retromruntime.Credentials) {
	t.Helper()
	root := t.TempDir()
	databasePath := filepath.Join(root, "retrom.db")
	database, err := store.Open(context.Background(), databasePath, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := retromruntime.LoadOrCreateCredentials(root)
	if err != nil {
		t.Fatal(err)
	}
	service, err := accounts.New(
		context.Background(), database.SQL, credentials, mode, authn.EmptyBlocklist{}, time.Now,
	)
	if err != nil {
		t.Fatal(err)
	}
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
	}, credentials
}

func TestReadSetupCodeCommandDoesNotModifyDatabase(t *testing.T) {
	t.Parallel()
	configuration, credentials := accountCommandFixture(t, config.ModeRelease)
	before, err := os.ReadFile(configuration.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	code, err := readSetupCode(context.Background(), configuration)
	if err != nil || code != credentials.SetupCode() {
		t.Fatalf("readSetupCode() = %q, %v", code, err)
	}
	after, err := os.ReadFile(configuration.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("setup-code command modified the database")
	}
}

func TestResetOfflineAdminRequiresLockAndTTYConfirmation(t *testing.T) {
	t.Parallel()
	configuration, _ := accountCommandFixture(t, config.ModeTest)
	lock, err := processlock.Acquire(configuration.DataDir)
	if err != nil {
		t.Fatal(err)
	}
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
	if len(prompts) != 2 || prompts[0] == prompts[1] {
		t.Fatalf("offline reset prompts = %#v", prompts)
	}

	database, err := store.Open(context.Background(), configuration.DBPath, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cleanup.Error("close", database.Close()) }()
	credentials, err := retromruntime.LoadCredentials(configuration.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	service, err := accounts.New(
		context.Background(), database.SQL, credentials, config.ModeRelease, authn.EmptyBlocklist{}, time.Now,
	)
	if err != nil {
		t.Fatal(err)
	}
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
		{"setup-code", "extra"}, {"admin-reset"}, {"admin-reset", "--username", "admin", "extra"},
	} {
		if err := executeWithPasswordReader(arguments, reader); err == nil {
			t.Fatalf("arguments %#v accepted", arguments)
		}
	}
}
