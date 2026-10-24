//go:build integration

package runs

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"retrom/internal/model"
	"retrom/internal/runtimeclient"
	"retrom/internal/storage"

	"github.com/google/uuid"
)

func TestInstalledBIOSMustSatisfyRequiredMembersBeforeLaunch(t *testing.T) {
	service, store := verifyingService(t)
	requirement := runtimeclient.BiosRequirement{Required: true, Members: []runtimeclient.BiosMember{{
		Name: "firmware.bin", Required: true, SizeBytes: 3, SHA1: "7037807198c22a7d2b0807371d763779a84fdfcf", CRC32: "55bc801d",
	}}}
	for _, test := range []struct {
		name  string
		bytes []byte
		valid bool
	}{
		{"matching renamed member", biosTestArchive(t, map[string][]byte{"renamed.bin": {1, 2, 3}}), true},
		{"incorrect member content", biosTestArchive(t, map[string][]byte{"renamed.bin": {4, 5, 6}}), false},
		{"unreadable archive", []byte("broken"), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertBiosVerification(t, service, store, requirement, test.bytes, test.valid)
		})
	}
}

func TestInstalledRTPMustHoldItsRootDirectoriesBeforeLaunch(t *testing.T) {
	service, store := verifyingService(t)
	requirement := runtimeclient.BiosRequirement{
		Required: true, Delivery: "SEEKABLE_BLOB", ResourceRole: "rtp", ArchiveRoot: []string{"Audio/", "Graphics/"},
	}
	for _, test := range []struct {
		name  string
		bytes []byte
		valid bool
	}{
		{"directories at the root", biosTestArchive(t, map[string][]byte{"Audio/BGM/a.mid": {1}, "Graphics/a.png": {2}}), true},
		{"compressed folder", biosTestArchive(t, map[string][]byte{"Standard/Audio/a.mid": {1}, "Standard/Graphics/a.png": {2}}), false},
		{"unreadable archive", []byte("broken"), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertBiosVerification(t, service, store, requirement, test.bytes, test.valid)
		})
	}
}

func assertBiosVerification(t *testing.T, service *Service, store *storage.Store, requirement runtimeclient.BiosRequirement,
	content []byte, valid bool,
) {
	t.Helper()
	stored, writeErr := store.Write(t.Context(), "bios", uuid.NewString(), bytes.NewReader(content), 4096)
	if writeErr != nil {
		t.Fatal(writeErr)
	}
	err := service.verifyBios(t.Context(), requirement, model.BiosFile{StorageKey: stored.Key})
	if valid && err != nil || !valid && !errors.Is(err, model.ErrBIOSMissing) {
		t.Fatalf("verification error=%v, expected valid=%v", err, valid)
	}
}

func verifyingService(t *testing.T) (*Service, *storage.Store) {
	t.Helper()
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	node := "node"
	if nodeHome := os.Getenv("NODE_HOME"); nodeHome != "" {
		node = filepath.Join(nodeHome, "bin", "node")
	}
	client, err := runtimeclient.Open(t.Context(), os.Getenv("RETROM_RUNTIME_TOOL_INPUT"), node, os.Getenv("RETROM_PROVIDER_INPUT"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return &Service{Storage: store, Runtime: client}, store
}

func biosTestArchive(t *testing.T, members map[string][]byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for name, content := range members {
		member, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = member.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
