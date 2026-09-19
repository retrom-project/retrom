package refactor

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	launchmodel "retrom/internal/model/launch"
	metadatamodel "retrom/internal/model/metadata"
	scrapemodel "retrom/internal/model/metadatascrape"
	savesmodel "retrom/internal/model/saves"
	launchservice "retrom/internal/service/launch"
	metadataservice "retrom/internal/service/metadatascrape"
	savesservice "retrom/internal/service/saves"
)

const (
	rf03Capability      = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8"
	rf03CapabilityHash  = "630dcd2966c4336691125448bbb25b4ff412a49c732db2c8abc1b8581bd710dd"
	rf03MetadataDigest  = "fb3eb2ea1255b92c14a3e5555264e310d65e1ef2f8c853f3c629006eac13b514"
	rf03ProjectIdentity = "385a629fabe80fab0148faa53cc889ad25167e76e8f557168241ff6817c86351"
)

func TestRefactorRF03_minimal_fake(t *testing.T) {
	t.Parallel()
	t.Run("Saves", rf03SavesMinimalFake)
	t.Run("Metadata", rf03MetadataMinimalFake)
	t.Run("Launch", rf03LaunchMinimalFake)
}

func rf03FixedNow() time.Time { return time.UnixMilli(1000) }

func rf03CredentialHash(t *testing.T) []byte {
	t.Helper()
	hash, err := hex.DecodeString(rf03CapabilityHash)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

// CheckpointStatus exercises the real save availability and capability guards.
// The blob dependency is nil because this use case never reads or writes CAS;
// this test makes no claim about the separate streamed checkpoint write path.
type rf03SavesCase struct {
	name                                string
	badCapability, expired, unavailable bool
	cause, wantError                    error
}

func rf03SavesMinimalFake(t *testing.T) {
	for _, test := range []rf03SavesCase{
		{name: "normal"},
		{name: "credential_rejected", badCapability: true, wantError: savesmodel.ErrCredential},
		{name: "expiry_rejected", expired: true, wantError: savesmodel.ErrCredential},
		{name: "checkpoint_unavailable", unavailable: true, wantError: savesmodel.ErrCheckpointUnavailable},
		{name: "reader_failure", cause: context.DeadlineExceeded, wantError: context.DeadlineExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			rf03SavesScenario(t, test)
		})
	}
}

func rf03SavesScenario(t *testing.T, test rf03SavesCase) {
	t.Helper()
	launch := savesmodel.Launch{
		State: "ACTIVE", HardExpiresAtMS: 2000, CredentialHash: rf03CredentialHash(t),
		ContentFormat: "RPG_MAKER_PROJECT",
	}
	launch.Checkpoint.WriteFormat, launch.Checkpoint.MaxBytes = "RETROM_FIXTURE_CHECKPOINT_V1", 1024
	if test.expired {
		launch.HardExpiresAtMS = 1000
	}
	if test.unavailable {
		launch.Checkpoint.WriteFormat = ""
	}
	repository := &rf03SaveRepository{launch: launch, cause: test.cause}
	service := savesservice.New(repository, nil, rf03FixedNow)
	capability := rf03Capability
	if test.badCapability {
		capability = "invalid"
	}
	status, err := service.CheckpointStatus(t.Context(), "save-launch", capability)
	if repository.calls != 1 || repository.launchID != "save-launch" {
		t.Fatalf("save reader calls=%d id=%q", repository.calls, repository.launchID)
	}
	if test.wantError != nil {
		if !errors.Is(err, test.wantError) || status.CheckpointFormat != "" || status.Availability.Available {
			t.Fatalf("save rejection/failure leaked success: %+v / %v", status, err)
		}
		return
	}
	if err != nil || status.CheckpointFormat != "RETROM_FIXTURE_CHECKPOINT_V1" ||
		!status.Availability.Available || status.Availability.Reason != nil {
		t.Fatalf("save checkpoint status = %+v / %v", status, err)
	}
}

type rf03SaveRepository struct {
	launch   savesmodel.Launch
	cause    error
	calls    int
	launchID string
}

var _ savesmodel.Repository = (*rf03SaveRepository)(nil)

func (repository *rf03SaveRepository) LoadLaunch(_ context.Context, id string) (savesmodel.Launch, error) {
	repository.calls++
	repository.launchID = id
	return repository.launch, repository.cause
}

func (*rf03SaveRepository) Restore(context.Context, string) (savesmodel.Restore, error) {
	panic("checkpoint status must not load a restore payload")
}

func (*rf03SaveRepository) WithWrite(context.Context, func(savesmodel.WriteScope) error) error {
	panic("checkpoint status must not acquire a writer")
}

type rf03MetadataCase struct {
	name                      string
	invalidHash               bool
	cacheError, providerError error
}

func rf03MetadataMinimalFake(t *testing.T) {
	for _, test := range []rf03MetadataCase{
		{name: "normal"},
		{name: "invalid_hash_rejected", invalidHash: true},
		{name: "cache_failure", cacheError: context.Canceled},
		{name: "provider_failure", providerError: context.DeadlineExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			rf03MetadataScenario(t, test)
		})
	}
}

func rf03MetadataScenario(t *testing.T, test rf03MetadataCase) {
	t.Helper()
	cache := &rf03MetadataCache{cause: test.cacheError}
	provider := &rf03MetadataProvider{
		cause: test.providerError,
		result: metadatamodel.LookupResult{
			Outcome: metadatamodel.OutcomeHit, RequestDigest: rf03MetadataDigest,
			Candidate: &metadatamodel.Candidate{
				ProviderGameID: "fixture-game", Metadata: json.RawMessage(`{"schemaVersion":1,"title":"Fixture"}`),
			},
		},
	}
	service := metadataservice.NewLookup(cache, nil, provider, rf03FixedNow)
	hashes := metadatamodel.ContentHashes{CRC32: "1234abcd"}
	if test.invalidHash {
		hashes.CRC32 = "1234ABCD"
	}
	result, err := service.Lookup(t.Context(), hashes, false)
	if test.invalidHash {
		rf03AssertMetadataHashRejection(t, result, err, cache, provider)
		return
	}
	rf03AssertMetadataCacheRead(t, cache)
	if test.cacheError != nil {
		rf03AssertMetadataFailure(t, result, err, test.cacheError)
		if provider.calls != 0 {
			t.Fatalf("cache failure reached provider: calls=%d", provider.calls)
		}
		return
	}
	if provider.calls != 1 || provider.hashes != hashes {
		t.Fatalf("metadata provider input = %+v", provider)
	}
	if test.providerError != nil {
		rf03AssertMetadataFailure(t, result, err, test.providerError)
		return
	}
	rf03AssertMetadataCandidate(t, result, provider.result, err)
}

func rf03AssertMetadataHashRejection(t *testing.T, result scrapemodel.ResolvedLookup, err error,
	cache *rf03MetadataCache, provider *rf03MetadataProvider,
) {
	t.Helper()
	if err == nil || errors.Unwrap(err) == nil || cache.calls != 0 || provider.calls != 0 || result.Result.Candidate != nil {
		t.Fatalf("invalid hashes reached a port: result=%+v cache=%d provider=%d error=%v", result, cache.calls, provider.calls, err)
	}
}

func rf03AssertMetadataCacheRead(t *testing.T, cache *rf03MetadataCache) {
	t.Helper()
	if cache.calls != 1 || cache.digest != rf03MetadataDigest || cache.now != 1000 {
		t.Fatalf("metadata cache input = %+v", cache)
	}
}

func rf03AssertMetadataFailure(t *testing.T, result scrapemodel.ResolvedLookup, err, cause error) {
	t.Helper()
	if !errors.Is(err, cause) || result.Result.Candidate != nil || result.Result.Outcome != "" || result.CachedResponseID != "" {
		t.Fatalf("metadata failure leaked success: %+v / %v", result, err)
	}
}

func rf03AssertMetadataCandidate(t *testing.T, result scrapemodel.ResolvedLookup, expected metadatamodel.LookupResult, err error) {
	t.Helper()
	if err != nil || result.CachedResponseID != "" || result.Result.Outcome != metadatamodel.OutcomeHit ||
		result.Result.RequestDigest != rf03MetadataDigest || result.Result.Candidate == nil {
		t.Fatalf("metadata lookup = %+v / %v", result, err)
	}
	if result.Result.Candidate.ProviderGameID != "fixture-game" || !bytes.Equal(result.Result.Candidate.Metadata, expected.Candidate.Metadata) {
		t.Fatalf("provider candidate changed: %+v", result.Result.Candidate)
	}
}

type rf03MetadataCache struct {
	calls  int
	digest string
	now    int64
	cause  error
}

var _ scrapemodel.CacheReader = (*rf03MetadataCache)(nil)

func (cache *rf03MetadataCache) Cached(_ context.Context, digest string, now int64) (scrapemodel.CachedResponse, bool, error) {
	cache.calls++
	cache.digest, cache.now = digest, now
	return scrapemodel.CachedResponse{}, false, cache.cause
}

type rf03MetadataProvider struct {
	result metadatamodel.LookupResult
	cause  error
	calls  int
	hashes metadatamodel.ContentHashes
}

var _ scrapemodel.LookupProvider = (*rf03MetadataProvider)(nil)

func (provider *rf03MetadataProvider) LookupByHash(_ context.Context, hashes metadatamodel.ContentHashes) (metadatamodel.LookupResult, error) {
	provider.calls++
	provider.hashes = hashes
	return provider.result, provider.cause
}

func (*rf03MetadataProvider) RestoreCached(metadatamodel.ContentHashes, metadatamodel.ProviderOutcome, metadatamodel.ProtocolAudit, []byte) (metadatamodel.LookupResult, error) {
	panic("a cache miss must not restore provider evidence")
}

type rf03LaunchCase struct {
	name                                string
	badCapability, revoked, invalidPath bool
	cause, wantError                    error
}

func rf03LaunchMinimalFake(t *testing.T) {
	for _, test := range []rf03LaunchCase{
		{name: "normal"},
		{name: "credential_rejected", badCapability: true, wantError: launchmodel.ErrCredential},
		{name: "revoked_rejected", revoked: true, wantError: launchmodel.ErrCredential},
		{name: "project_path_rejected", invalidPath: true, wantError: launchmodel.ErrBlocked},
		{name: "reader_failure", cause: context.Canceled, wantError: context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			rf03LaunchScenario(t, test)
		})
	}
}

func rf03LaunchScenario(t *testing.T, test rf03LaunchCase) {
	t.Helper()
	credentialHash := rf03CredentialHash(t)
	snapshot := launchmodel.ConfigSnapshot{
		Authority: launchmodel.ConfigAuthority{Source: launchmodel.ConfigSource{
			State: "ACTIVE", Purpose: "PRODUCT", Delivery: "FILE_TREE_PROJECT",
			Version: 1, HardEnd: 2000, CredentialHash: credentialHash,
		}},
		Files: []launchmodel.ConfigFile{
			{LogicalName: "Data/System.json", Format: "RPG_MAKER_PROJECT", Digest: strings.Repeat("a", 64)},
			{LogicalName: "Data/Actors.json", Format: "RPG_MAKER_PROJECT", Digest: strings.Repeat("b", 64)},
		},
	}
	if test.revoked {
		snapshot.Authority.Source.State = "REVOKED"
	}
	if test.invalidPath {
		snapshot.Files[0].LogicalName = "../escape.json"
	}
	reader := &rf03ProjectReader{snapshot: snapshot, cause: test.cause}
	service := launchservice.NewProjectQueries(reader, rf03FixedNow, func(capability string, hash []byte) bool {
		return capability == rf03Capability && bytes.Equal(hash, credentialHash)
	})
	capability := rf03Capability
	if test.badCapability {
		capability = "invalid"
	}
	identity, err := service.Identity(t.Context(), "project-launch", capability)
	if reader.calls != 1 || reader.id != "project-launch" {
		t.Fatalf("launch reader = %+v", reader)
	}
	authorizations := 1
	if test.cause != nil {
		authorizations = 0
	}
	if reader.authorizations != authorizations {
		t.Fatalf("Service authorization callback calls = %d", reader.authorizations)
	}
	if test.wantError != nil {
		if !errors.Is(err, test.wantError) || identity != "" {
			t.Fatalf("launch rejection/failure leaked identity: %s / %v", identity, err)
		}
		return
	}
	if err != nil || identity != rf03ProjectIdentity {
		t.Fatalf("project identity = %s / %v", identity, err)
	}
}

type rf03ProjectReader struct {
	snapshot              launchmodel.ConfigSnapshot
	cause                 error
	calls, authorizations int
	id                    string
}

var _ launchmodel.ProjectIdentityReader = (*rf03ProjectReader)(nil)

func (reader *rf03ProjectReader) Project(_ context.Context, id string, authorize launchmodel.ConfigAuthorization) (launchmodel.ConfigSnapshot, bool, error) {
	reader.calls++
	reader.id = id
	if reader.cause != nil {
		return launchmodel.ConfigSnapshot{}, false, reader.cause
	}
	reader.authorizations++
	if err := authorize(reader.snapshot.Authority.Source); err != nil {
		return launchmodel.ConfigSnapshot{}, false, err
	}
	return reader.snapshot, true, nil
}

func TestRefactorRF03_value_compat(t *testing.T) {
	t.Parallel()
	input, golden := rf03ValueCompatibilityFixture(t)
	t.Run("HTTP_candidate_raw_and_digest", func(t *testing.T) { rf03MetadataValueCompatibility(t, input, golden.Metadata) })
	t.Run("jobs", func(t *testing.T) { rf03JobValueCompatibility(t, input, golden) })
	t.Run("saves", func(t *testing.T) { rf03SaveValueCompatibility(t, input, golden) })
	t.Run("blob_facts", func(t *testing.T) { rf03BlobValueCompatibility(t, input.BlobBody, golden.BlobFactsJSON) })
	t.Run("netplay_wire_values", rf03NetplayValueCompatibility)
}
