#!/usr/bin/env python3
"""Deterministic Retrom acceptance evidence runner.

The runner deliberately refuses to call an unregistered Case a success.  A Case
only becomes registered when the repository contains a focused executable check
for its complete contract.
"""

from __future__ import annotations

import hashlib
import json
import os
import re
import secrets
import shutil
import signal
import subprocess
import sys
import time
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
ARTIFACTS = ROOT / ".artifacts" / "acceptance"
CURRENT = ARTIFACTS / "current-run"
CASE_PATTERN = re.compile(r"^### (ACC-[A-Z]+-\d{3})[：:]", re.MULTILINE)
CORE_CASE_ROW_PATTERN = re.compile(r"^\| `(ACC-CORE-\d{3})` \|", re.MULTILINE)
CONDITIONAL_CASES = {"ACC-NET-002", "ACC-DAT-006"}


# These commands are intentionally focused. Cases omitted here are emitted as
# FAIL instead of being hidden behind a broad package-level green test.
CASE_COMMANDS: dict[str, tuple[int, str]] = {
    "ACC-QA-001": (900, "make ci"),
    "ACC-QA-002": (300, "scripts/acceptance/quality-sentinels.sh"),
    "ACC-PKG-001": (
        900,
        """set -euo pipefail
make build-backend-image
docker image inspect retrom:latest
docker image inspect retrom:latest | python3 -c 'import json, sys; assert not json.load(sys.stdin)[0]["Config"].get("User")'
docker run --rm --network none --read-only --user 1000:1000 --entrypoint /bin/sh retrom:latest -ec '
unreadable="$(find /opt/retrom/dependencies \\( -type d ! -perm -005 -o -type f ! -perm -004 \\) -print)"
test -z "$unreadable"
test -z "$(find /opt/retrom/dependencies \\( -type d -perm -222 -o -type f -perm -222 \\) -print)"
! grep -q "^retrom:" /etc/passwd
find /opt/retrom/dependencies -type f -exec sha256sum {} + >/dev/null
'
""",
    ),
    "ACC-PKG-002": (
        900,
        """set -euo pipefail
make build-web-image
docker image inspect retrom-web:latest
docker image inspect retrom-web:latest | python3 -c 'import json, sys; assert not json.load(sys.stdin)[0]["Config"].get("User")'
docker run --rm --network none --read-only --user 1000:1000 --entrypoint /bin/sh retrom-web:latest -ec '! grep -q "^retrom:" /etc/passwd && test -r /app/server.js'
""",
    ),
    "ACC-PKG-003": (
        900,
        """set -euo pipefail
dry_run="$(make -n build-images)"
if grep -Eiq '(^|[[:space:]])(docker[[:space:]]+)?(run|compose|login|push)([[:space:]]|$)' <<<"$dry_run"; then
  echo 'build-images dry-run contains a forbidden runtime, compose, login, or push command' >&2
  exit 1
fi
expected="$(make --no-print-directory release-input-digest)"
containers_before="$(docker ps -aq --no-trunc | sort)"
networks_before="$(docker network ls -q --no-trunc | sort)"
volumes_before="$(docker volume ls -q | sort)"
make build-images
containers_after="$(docker ps -aq --no-trunc | sort)"
networks_after="$(docker network ls -q --no-trunc | sort)"
volumes_after="$(docker volume ls -q | sort)"
backend_label="$(docker image inspect --format '{{ index .Config.Labels \"io.retrom.release-input-sha256\" }}' retrom:latest)"
web_label="$(docker image inspect --format '{{ index .Config.Labels \"io.retrom.release-input-sha256\" }}' retrom-web:latest)"
test "$containers_before" = "$containers_after"
test "$networks_before" = "$networks_after"
test "$volumes_before" = "$volumes_after"
test "$backend_label" = "$expected"
test "$web_label" = "$expected"
printf 'release_input=%s\\ncontainers_before=%s\\ncontainers_after=%s\\nnetworks_before=%s\\nnetworks_after=%s\\nvolumes_before=%s\\nvolumes_after=%s\\nbackend_label=%s\\nweb_label=%s\\n' \
  "$expected" "$containers_before" "$containers_after" "$networks_before" "$networks_after" \
  "$volumes_before" "$volumes_after" "$backend_label" "$web_label"
""",
    ),
    "ACC-DEV-001": (180, "scripts/acceptance/local-development.sh"),
    "ACC-NET-001": (180, "scripts/acceptance/network-boundary.sh"),
    "ACC-DB-001": (120, "go test -tags=integration ./internal/store -run '^TestMigrationsCreateIntegerBusinessTimesAndSeedCatalog$' -count=1"),
    "ACC-DB-002": (120, "go test -tags=integration ./internal/store -run '^TestSupportedMigrationVersionsIdempotencyAndFutureProtection$' -count=1"),
    "ACC-CAS-001": (120, "go test ./internal/blobstore -run '^TestPutDeduplicatesConcurrentContent$' -count=1"),
    "ACC-CAS-002": (120, "go test ./internal/blobgc -run '^TestRunOnceHonorsGraceAndConcurrentReference$' -count=1"),
    "ACC-BKP-001": (300, "go test -tags=integration ./internal/maintenance -run '^TestBackupRestoreRoundTripAndOnlineRefusal$' -count=1"),
    "ACC-SEC-001": (120, "go test -tags=integration ./internal/arcadedat ./internal/importing -run 'TestParserAllowsSafeDoctypeWithoutResolvingIt|TestParserRejectsEntityDirective|TestValidateLogicalPath' -count=1"),
    "ACC-SEC-002": (
        120,
        "go test ./internal/runtime ./internal/httpapi -run 'TestCredentialsConcurrentCreationConverges|TestCredentialsRejectSymlink|TestRestrictedBinaryEndpointsRejectMultipleRanges' -count=1 && go test -tags=integration ./internal/launch -run '^TestPublishedGameLaunchLocksContentAndCredential$' -count=1",
    ),
    "ACC-SEC-003": (
        120,
        "go test ./internal/httpapi -run 'TestHealthIsPublicAndProtectedWritesRequireAuthentication|TestProtectedWritesRejectInvalidOriginWithoutEnablingCORS|TestAuthHTTPTestLoginCookieCSRFAndLogout|TestAuthHTTPReleasePendingRequiresSetupAndExactOrigin' -count=1",
    ),
    "ACC-SEC-004": (120, "go test ./internal/hasheous -run 'TestLookupNormalizesBoundedResponse|TestLookupClassifiesMissAndOversize|TestFetchAssetValidatesImageAndEveryRedirect' -count=1"),
    "ACC-API-001": (120, "go test ./internal/httpapi ./internal/cursor -count=1"),
    "ACC-FAV-001": (
        120,
        "go test ./internal/store -run 'TestFavoritesMigrationConstraintsAndIndexes|TestFavoritesMigrationUpgradesVersion24AndPreservesFixture|TestMultiDiscMigrationUpgradesVersion23WithoutOwnershipDrift' -count=1 && go test -tags=integration ./internal/maintenance -run '^TestBackupRestoreRoundTripAndOnlineRefusal$' -count=1",
    ),
    "ACC-FAV-002": (
        120,
        "go test ./internal/favorites ./internal/httpapi -run 'TestNormalizeFolderName|TestServiceFolderLifecycleUndoAndOwnerIsolation|TestServiceListPaginationScopesAndVisibility|TestFavoriteHTTPContractLifecycleReplayIsolationAndProjection|TestFavoriteHTTPRejectsAnonymousUnsafeAndNonStrictRequests' -count=1",
    ),
    "ACC-FAV-003": (180, "scripts/acceptance/ui-case.sh ACC-FAV-003"),
    "ACC-FAV-004": (180, "scripts/acceptance/ui-case.sh ACC-FAV-004"),
    "ACC-OPS-001": (
        120,
        "go test ./internal/config ./internal/httpapi -run 'TestRejectUnknownVariablesAllowsToolPrefixesOnly|TestDiagnosticsUsesClosedSnapshotSchemaAndRequiredHeaders' -count=1 && go test -tags=integration ./internal/httpapi -run '^TestReadinessGatesBusinessRoutesDuringDATIndexing$' -count=1",
    ),
    "ACC-AUTH-001": (
        120,
        "go test ./internal/accounts ./internal/httpapi -run 'TestReadSetupCodeIsReadOnlyAndPendingOnly|TestAuthHTTPReleasePendingRequiresSetupAndExactOrigin|TestReleaseInitializationLoginExpiryAndPasswordRotation' -count=1",
    ),
    "ACC-AUTH-002": (
        120,
        "go test ./internal/accounts ./internal/store -run 'TestTestModeBootstrapsExactlyOnceAndReleaseRejectsDefaultCredential|TestSupportedMigrationVersionsIdempotencyAndFutureProtection' -count=1",
    ),
    "ACC-AUTH-003": (
        120,
        "make data-check && make deps-check && go test ./internal/accounts ./internal/httpapi -run 'TestReleaseInitializationLoginExpiryAndPasswordRotation|TestLoginRateLimitIsAtomicHashedAndExpiresWithInjectedClock|TestSetupAndLinkRateLimitsUseIndependentIPBuckets|TestAuthHTTPTestLoginCookieCSRFAndLogout|TestAuthHTTPLoginRateLimitReturnsRetryAfter|TestCanonicalClientIPTrustsOnlyConfiguredProxyChain' -count=1",
    ),
    "ACC-AUTH-004": (
        120,
        "go test ./internal/accounts ./internal/httpapi -run 'TestInvitationAndPasswordResetCapabilitiesAreSingleUseAndSecretless|TestInvitationConcurrentConsumptionAndUserLifecycleRevocations|TestAccountAdministrationHTTPInvitationAndAuthorization' -count=1",
    ),
    "ACC-AUTH-005": (
        120,
        "go test ./internal/accounts -run 'TestInvitationConcurrentConsumptionAndUserLifecycleRevocations|TestOfflineAdminResetRotatesCredentialAndSecurityState|TestAccountSecurityAuditUsesClosedActions' -count=1",
    ),
    "ACC-AUTH-006": (
        120,
        "go test ./internal/httpapi -run '^TestAccountAdministrationHTTPInvitationAndAuthorization$' -count=1",
    ),
    "ACC-ISO-001": (
        120,
        "go test ./internal/httpapi -run '^TestGameDetailReturnsCoreValidationChoicesAndDOSPrograms$' -count=1",
    ),
    "ACC-ISO-002": (
        120,
        "go test ./internal/httpapi -run 'TestGameDetailReturnsCoreValidationChoicesAndDOSPrograms|TestIdempotencyRecordsAreScopedToAuthenticatedUser' -count=1",
    ),
    "ACC-ISO-003": (
        180,
        "go test ./internal/accounts -run '^TestInvitationConcurrentConsumptionAndUserLifecycleRevocations$' -count=1 && make web-test",
    ),
    "ACC-PLAT-001": (
        120,
        "go test ./internal/httpapi -run '^TestPlatformLifecycleUsesImpactDigestVersioningAndAudit$' -count=1",
    ),
    "ACC-PLAT-002": (
        120,
        "go test -tags=integration ./internal/httpapi -run '^TestPlatformInstanceOwnershipAndNonEmptyLifecycleBoundaries$' -count=1",
    ),
    "ACC-PLAT-003": (
        180,
        "go test -tags=integration ./internal/httpapi -run '^TestDefaultCoreImpactPaginationRejectsDriftAndPreservesSaveLaunch$' -count=1 -timeout=30s",
    ),
    "ACC-PLAT-004": (
        180,
        "go test -tags=integration ./internal/httpapi -run '^TestGameMovePreviewQueuesTargetCoreValidationAndPreservesHistory$' -count=1 -timeout=30s",
    ),
    "ACC-PLAT-005": (
        120,
        "go test -tags=integration ./internal/httpapi -run 'TestPlatformLifecycleUsesImpactDigestVersioningAndAudit|TestPlatformInstanceOwnershipAndNonEmptyLifecycleBoundaries' -count=1",
    ),
    "ACC-GAME-002": (180, "go test -tags=integration ./internal/gamecontent -run '^TestReplacementPublishesAtomicallyAndFailureKeepsCurrent$' -count=1"),
    "ACC-GAME-001": (
        180,
        "go test -tags=integration ./internal/httpapi ./internal/metadatascrape -run 'TestGameMetadataRevisionProjectionAndOptimisticEdit|TestImportPersistsHasheousEvidenceCandidateAndAsset' -count=1 -timeout=30s",
    ),
    "ACC-GAME-003": (
        180,
        "go test -tags=integration ./internal/httpapi -run '^TestGameSoftDeleteIsIdempotentRevokesLaunchAndPreservesReferences$' -count=1 -timeout=30s",
    ),
    "ACC-IMP-001": (180, "go test -tags=integration ./internal/uploads ./internal/libraryimport -run 'TestUploadPartAndFinalization|TestCreateRejectsUnsafeAndDuplicatePaths|TestUploadImportReviewPublishPipeline' -count=1"),
    "ACC-IMP-002": (
        180,
        "go test -tags=integration ./internal/libraryimport -run 'TestImportGroupsSingleArchiveMemberAndReportsEveryFile|TestDOSDirectoryGroupingProducesDeterministicBundleAndSafePrograms' -count=1",
    ),
    "ACC-IMP-003": (
        180,
        "go test -tags=integration ./internal/metadatascrape ./internal/libraryimport -run 'TestImportPersistsHasheousEvidenceCandidateAndAsset|TestArcadeHasheousEvidenceUsesMatchedDATEntriesOnly|TestImportGroupsSingleArchiveMemberAndReportsEveryFile|TestSevenZipImportMaterializesSingleROMAndPreservesEvidence|TestArcadeGroupingBuildsCoreScopedParentAndBIOSClosure' -count=1",
    ),
    "ACC-IMP-004": (
        180,
        "go test -tags=integration ./internal/libraryimport -run '^TestUploadImportReviewPublishPipeline$' -count=1",
    ),
    "ACC-IMP-005": (180, "go test -tags=integration ./internal/hasheous ./internal/metadatascrape -count=1"),
    "ACC-IMP-006": (
        180,
        "go test -tags=integration ./internal/metadatascrape -run '^TestArcadeHasheousEvidenceUsesMatchedDATEntriesOnly$' -count=1",
    ),
    "ACC-IMP-007": (
        180,
        "go test -tags=integration ./internal/libraryimport ./internal/metadatascrape -run 'TestUploadImportReviewPublishPipeline|TestDuplicateContentIsSkippedDuringIdentificationAndConfirmedDuringReview|TestImportPersistsHasheousEvidenceCandidateAndAsset' -count=1",
    ),
    "ACC-IMP-008": (
        180,
        "go test ./internal/jobs -run '^TestCancelAndRetryEnforceVersionedState$' -count=1 && go test ./internal/importing -run 'TestSevenZip' -count=1 && go test -tags=integration ./internal/libraryimport -run '^TestImportGroupsSingleArchiveMemberAndReportsEveryFile$' -count=1",
    ),
    "ACC-DAT-001": (300, "go test -tags=integration ./internal/arcadedat ./internal/dependencies -run 'TestRealDATStatisticsMatchManifest|TestBootstrapCatalogsMaterializesPinnedDATsIdempotently' -count=1"),
    "ACC-DAT-002": (
        180,
        "go test -tags=integration ./internal/libraryimport -run '^TestArcadeGroupingBuildsCoreScopedParentAndBIOSClosure$' -count=1",
    ),
    "ACC-DAT-003": (180, "go test -tags=integration ./internal/arcadecatalog -run '^TestUserDATRequiresParseDiffAndExplicitActivation$' -count=1"),
    "ACC-DAT-004": (
        180,
        "go test -tags=integration ./internal/arcadecatalog -run '^TestUserDATRequiresParseDiffAndExplicitActivation$' -count=1",
    ),
    "ACC-DAT-005": (120, "go test ./internal/arcadedat -run 'TestParserAllowsSafeDoctypeWithoutResolvingIt|TestParserRejectsEntityDirective' -count=1"),
    "ACC-DAT-006": (300, "scripts/acceptance/dependency-upgrade.sh"),
    "ACC-BIOS-001": (120, "go test -tags=integration ./internal/firmware -run '^TestStaticBIOSHashMismatchIsInstalledAsWarning$' -count=1"),
    "ACC-BIOS-002": (
        180,
        "go test -tags=integration ./internal/dependencies ./internal/launch ./internal/libraryimport ./internal/firmware -run 'TestBIOSActivationOptionsRejectConflictingSeed|TestPublishedGameLaunchLocksContentAndCredential|TestMelonDSExternalBIOSIsLockedPerLaunch|TestArcadeGroupingBuildsCoreScopedParentAndBIOSClosure|TestStaticBIOSHashMismatchIsInstalledAsWarning' -count=1",
    ),
    "ACC-BIOS-003": (
        120,
        "go test ./internal/config ./internal/serverimport ./internal/httpapi -run 'TestParseServerImportRootsStrictSchemaAndOverlap|TestRelativePathAndNoFollowDirectoryBoundary|TestServerImportHTTPRootBoundaryAuthorizationAndIdempotency' -count=1",
    ),
    "ACC-BIOS-004": (
        180,
        "go test ./internal/firmware ./internal/serverimport -run 'TestStaticRankingNeverLetsSizeBeatExactHash|TestDATRankingPrefersLaunchableArchive|TestServerBIOSImportDiscoversAndInstallsExactStaticCandidate' -count=1",
    ),
    "ACC-BIOS-005": (
        180,
        "go test -race ./internal/firmware ./internal/serverimport -run 'TestStaticRankingNeverLetsSizeBeatExactHash|TestDATRankingPrefersLaunchableArchive|TestServerBIOSImportDiscoversAndInstallsExactStaticCandidate' -count=1",
    ),
    "ACC-BIOS-006": (
        300,
        "go test ./internal/serverimport -run 'TestServerBIOSImportDiscoversAndInstallsExactStaticCandidate|TestRelativePathAndNoFollowDirectoryBoundary' -count=1 && go test -tags=integration ./internal/maintenance -run '^TestBackupRestoreRoundTripAndOnlineRefusal$' -count=1 && scripts/acceptance/ui-case.sh ACC-BIOS-006",
    ),
    "ACC-BIOS-007": (
        240,
        "go test ./internal/httpapi -run '^TestBIOSFullCatalogCursorTraverses286Items$' -count=1 && scripts/acceptance/ui-case.sh ACC-BIOS-007",
    ),
    "ACC-PEG-001": (
        180,
        "go test ./internal/pegasusmeta ./internal/pegasusimport -run 'TestParse|TestScan' -count=1",
    ),
    "ACC-PEG-002": (
        180,
        "go test ./internal/serversource ./internal/httpapi -run 'TestDeclaredPathNormalization|TestWalkAndOpenStayWithinNoFollowDescriptors|TestPegasusImportHTTPScanMappingAndSourceDrift' -count=1",
    ),
    "ACC-PEG-003": (
        300,
        "go test ./internal/store -run '^TestPegasusMigrationUpgradesVersion27AndPreservesImageAssets$' -count=1 && go test ./internal/pegasusimport ./internal/libraryimport -run 'TestScanMapImportPublishesGameAndMedia|TestSelectServerImportItemUsesTheDeclaredPrimarySource|TestMultiDiscDirectoryCreatesOrderedItemsAndPublishesCanonicalContent|TestArcadeGroupingBuildsCoreScopedParentAndBIOSClosure' -count=1",
    ),
    "ACC-PEG-004": (
        300,
        "go test ./internal/pegasusimport ./internal/serversource -run 'TestRecoverWorkClosesExhaustedLeaseAsFailed|TestWalkAndOpenStayWithinNoFollowDescriptors' -count=1 && go test -tags=integration ./internal/maintenance -run '^TestBackupRestoreRoundTripAndOnlineRefusal$' -count=1 && go test ./internal/blobgc -run '^TestRunOnceHonorsGraceAndConcurrentReference$' -count=1",
    ),
    "ACC-PEG-005": (240, "scripts/acceptance/ui-case.sh ACC-PEG-005"),
    "ACC-MEDIA-001": (
        240,
        "go test ./internal/mediaasset ./internal/httpapi -run 'TestInspect|TestGameDetailReturnsCoreValidationChoicesAndDOSPrograms' -count=1 && scripts/acceptance/ui-case.sh ACC-MEDIA-001",
    ),
    "ACC-RUN-001": (180, "go test -tags=integration ./internal/launch -run '^TestPublishedGameLaunchLocksContentAndCredential$' -count=1"),
    "ACC-RUN-002": (180, "scripts/acceptance/ui-case.sh ACC-RUN-002 && node data/example/smoke-test.mjs mame2003"),
    "ACC-RUN-003": (180, "scripts/acceptance/ui-case.sh ACC-RUN-003"),
    "ACC-RUN-004": (180, "scripts/acceptance/ui-case.sh ACC-RUN-004"),
    "ACC-RUN-005": (
        180,
        "go test -tags=integration ./internal/launch ./internal/libraryimport -run 'TestDOSDirectBundleIsDeterministicAndInjectsOnlyExactConfig|TestDOSLaunchLocksMenuOrSelectedDeterministicBundle|TestDOSDirectoryGroupingProducesDeterministicBundleAndSafePrograms' -count=1",
    ),
    "ACC-SAVE-001": (
        180,
        "go test -tags=integration ./internal/saves -run '^TestManualStateRequiresAtomicNonEmptyStateAndScreenshot$' -count=1",
    ),
    "ACC-SAVE-002": (180, "scripts/acceptance/ui-case.sh ACC-SAVE-002"),
    "ACC-SAVE-003": (
        180,
        "go test -tags=integration ./internal/saves -run 'TestPersistentSaveLocksLaunchBaseAndEnforcesSequence|TestPersistentSaveNoneRejectsGetAndPutWithoutCreatingRows' -count=1 && make web-test",
    ),
    "ACC-NP-001": (120, "scripts/acceptance/netplay-case.sh ACC-NP-001"),
    "ACC-NP-002": (120, "scripts/acceptance/netplay-case.sh ACC-NP-002"),
    "ACC-NP-003": (240, "scripts/acceptance/netplay-case.sh ACC-NP-003"),
    "ACC-NP-004": (240, "scripts/acceptance/netplay-case.sh ACC-NP-004"),
    "ACC-NP-005": (300, "scripts/acceptance/netplay-case.sh ACC-NP-005"),
    "ACC-NP-006": (180, "scripts/acceptance/netplay-case.sh ACC-NP-006"),
    "ACC-NP-007": (120, "scripts/acceptance/netplay-case.sh ACC-NP-007"),
    "ACC-NP-008": (180, "scripts/acceptance/netplay-case.sh ACC-NP-008"),
    "ACC-NP-009": (120, "scripts/acceptance/netplay-case.sh ACC-NP-009"),
    "ACC-NP-010": (
        120,
        "python3 data/example/verify-fixtures.py && go test ./internal/netplay ./internal/httpapi "
        "-run 'TestAcceptanceNP010|TestDecodeClientMessageRejectsUnknownDuplicateDeepAndOversizeInput|TestStateFrameParsesRAStateAndBindsHeader|TestCoreStatePayloadRejectsMalformedAndMissingMemoryChunks|TestCredentialIsPurposeBoundAndStoredOwnerOnly' -count=1",
    ),
    "ACC-NP-011": (
        180,
        "python3 data/example/verify-fixtures.py && go test ./internal/netplay "
        "-run 'TestAcceptanceNP011' -count=1 && go test ./internal/config ./internal/httpapi "
        "-run 'TestParseNetplayCapacityAndFixedProtocolTimers|TestNetplayFeatureFlagHidesRoutesAndAuthProjection' -count=1 && "
        ".cache/tools/node-v24.18.0-linux-x64/bin/npm --prefix web test -- --run components/app-shell.test.tsx",
    ),
    "ACC-NP-012": (
        120,
        "go test ./internal/netplay -run '^TestAcceptanceNP012' -count=1 && "
        ".cache/tools/node-v24.18.0-linux-x64/bin/npm --prefix web test -- --run features/netplay/room-lobby.test.tsx",
    ),
    "ACC-NP-013": (180, "scripts/acceptance/netplay-single-regression.sh"),
    "ACC-PLAY-001": (120, "go test -tags=integration ./internal/launch -run '^TestPublishedGameLaunchLocksContentAndCredential$' -count=1"),
    "ACC-MDISC-001": (
        600,
        "go test -tags=integration ./internal/libraryimport -run '^TestMultiDiscDirectoryCreatesOrderedItemsAndPublishesCanonicalContent$' -count=1 -timeout=60s && make web-test",
    ),
    "ACC-MDISC-002": (
        600,
        "go test -tags=integration ./internal/libraryimport ./internal/httpapi -run 'TestMultiDiscMissingDiscIsBlockedWithoutPlaceholderBlob|TestMultiDiscAttachmentRejectsNonExactSetWithoutAdvancingDraft|TestMultiDiscAttachmentHTTPContractAndReviewProjection' -count=1 -timeout=60s",
    ),
    "ACC-MDISC-003": (
        600,
        "go test ./internal/multidisc -count=1 && go test -tags=integration ./internal/libraryimport -run '^TestMultiDiscAdmissionRejectsMissingPlaylistAndUnsupportedTargetWithoutConsumption$' -count=1 -timeout=60s",
    ),
    "ACC-MDISC-004": (
        600,
        "go test -tags=integration ./internal/libraryimport -run '^TestMultiDiscDirectoryCreatesOrderedItemsAndPublishesCanonicalContent$' -count=1 -timeout=60s && go test ./internal/httpapi -run '^TestRestrictedBinaryEndpointsRejectMultipleRanges$' -count=1",
    ),
    "ACC-MDISC-007": (600, "scripts/acceptance/multidisc-regression.sh"),
    "ACC-MDISC-008": (
        600,
        "go test -tags=integration ./internal/httpapi ./internal/libraryimport -run 'TestMultiDiscAttachmentHTTPContractAndReviewProjection|TestMultiDiscDirectoryCreatesOrderedItemsAndPublishesCanonicalContent' -count=1 -timeout=60s && go test ./internal/accounts ./internal/httpapi -run 'TestInvitationConcurrentConsumptionAndUserLifecycleRevocations|TestIdempotencyRecordsAreScopedToAuthenticatedUser|TestAccountAdministrationHTTPInvitationAndAuthorization' -count=1",
    ),
    "ACC-UI-001": (180, "scripts/acceptance/ui-case.sh ACC-UI-001"),
    "ACC-UI-002": (180, "scripts/acceptance/ui-case.sh ACC-UI-002"),
    "ACC-UI-003": (180, "scripts/acceptance/ui-case.sh ACC-UI-003"),
    "ACC-UI-004": (180, "scripts/acceptance/ui-case.sh ACC-UI-004"),
    "ACC-UI-005": (180, "scripts/acceptance/ui-case.sh ACC-UI-005"),
    "ACC-UI-006": (180, "scripts/acceptance/ui-case.sh ACC-UI-006"),
    "ACC-UI-007": (180, "scripts/acceptance/ui-case.sh ACC-UI-007"),
    "ACC-UI-008": (180, "scripts/acceptance/ui-case.sh ACC-UI-008"),
    "ACC-UI-009": (180, "scripts/acceptance/ui-case.sh ACC-UI-009"),
}

CORE_CASES = {
    "ACC-CORE-001": "fceumm",
    "ACC-CORE-002": "snes9x",
    "ACC-CORE-003": "gambatte",
    "ACC-CORE-004": "mgba",
    "ACC-CORE-005": "fbneo",
    "ACC-CORE-006": "mame2003",
    "ACC-CORE-007": "mame2003_plus",
    "ACC-CORE-008": "dosbox_pure",
    "ACC-CORE-009": "nestopia",
    "ACC-CORE-010": "melonds",
    "ACC-CORE-011": "desmume2015",
    "ACC-CORE-012": "desmume",
    "ACC-CORE-013": "a5200",
    "ACC-CORE-014": "pcsx_rearmed",
    "ACC-CORE-015": "mednafen_psx_hw",
    "ACC-CORE-016": "handy",
    "ACC-CORE-017": "yabause",
    "ACC-CORE-018": "genesis_plus_gx",
    "ACC-CORE-019": "mupen64plus_next",
    "ACC-CORE-020": "parallel_n64",
    "ACC-CORE-021": "opera",
    "ACC-CORE-022": "prosystem",
    "ACC-CORE-023": "stella2014",
    "ACC-CORE-024": "picodrive",
    "ACC-CORE-025": "mednafen_pce",
    "ACC-CORE-026": "mednafen_pcfx",
    "ACC-CORE-027": "mednafen_ngp",
    "ACC-CORE-028": "ppsspp",
    "ACC-CORE-029": "beetle_vb",
    "ACC-CORE-030": "mednafen_wswan",
    "ACC-CORE-031": "smsplus",
    "ACC-CORE-032": "fbalpha2012_cps1",
    "ACC-CORE-033": "fbalpha2012_cps2",
    "ACC-CORE-034": "genesis_plus_gx_wide",
    "ACC-CORE-035": "azahar",
}

MULTIDISC_RUNTIME_CASES = {
    "ACC-MDISC-005": ("multidisc-saturn-2", "Saturn 双盘游戏画面；换盘 0→1→0 后继续推进"),
    "ACC-MDISC-006": ("multidisc-saturn-3", "Saturn 三盘游戏画面；全部盘可切换且跨盘恢复顺序正确"),
}

MULTIDISC_PRODUCT_COMMANDS = {
    "ACC-MDISC-005": "make web-test",
    "ACC-MDISC-006": (
        "go test -tags=integration ./internal/libraryimport "
        "-run '^TestMultiDiscDirectoryCreatesOrderedItemsAndPublishesCanonicalContent$' -count=1 -timeout=120s && "
        "go test -tags=integration ./internal/saves "
        "-run '^TestPersistentSaveLocksLaunchBaseAndEnforcesSequence$' -count=1 -timeout=120s && "
        "make web-test"
    ),
}

CORE_EXPECTATIONS = {
    "fceumm": "Family Computer/FDS 启动画面",
    "snes9x": "Dr. Mario 标题或玩家选择菜单",
    "gambatte": "Tetris 版权启动画面",
    "mgba": "Sudoku Advance 标题或菜单",
    "fbneo": "Lode Runner 标题或投币画面",
    "mame2003": "Lode Runner 标题或 attract 画面",
    "mame2003_plus": "Lode Runner 标题或投币画面",
    "dosbox_pure": "DOOM II 标题画面",
    "nestopia": "Family Computer/FDS 启动画面",
    "melonds": "Zoo Keeper 标题菜单而非 FreeBIOS 空白双屏",
    "desmume2015": "Zoo Keeper 标题菜单",
    "desmume": "Zoo Keeper 标题菜单",
    "a5200": "Super Breakout 游戏画面",
    "pcsx_rearmed": "PlayStation/游戏启动画面",
    "mednafen_psx_hw": "PlayStation/游戏启动画面",
    "handy": "Lode Runner 标题/游戏画面",
    "yabause": "Sega Saturn 游戏画面",
    "genesis_plus_gx": "Felix the Cat 标题画面",
    "mupen64plus_next": "Dr. Mario 64 标题画面",
    "parallel_n64": "Dr. Mario 64 标题画面",
    "opera": "Total Eclipse 游戏画面",
    "prosystem": "Asteroids 标题画面",
    "stella2014": "Freeway 游戏画面",
    "picodrive": "Felix the Cat 标题画面",
    "mednafen_pce": "Adventure Island 游戏画面",
    "mednafen_pcfx": "光盘内游戏菜单",
    "mednafen_ngp": "Pac-Man 游戏画面",
    "ppsspp": "CSO 与 ISO 均到达 Sheep Defense 标题画面",
    "beetle_vb": "Panic Bomber 动画开场",
    "mednafen_wswan": "Mingle Magnet 标题画面",
    "smsplus": "Bank Panic 标题画面",
    "fbalpha2012_cps1": "1941 attract 或游戏画面",
    "fbalpha2012_cps2": "Pocket Fighter 动画开场",
    "genesis_plus_gx_wide": "Fix-It Felix Jr. high-score 或 attract 画面",
    "azahar": "Cave Story 2D 中文标题和菜单",
}

CORE_TIMEOUTS = {
    "pcsx_rearmed": 240,
    "mednafen_psx_hw": 240,
    "yabause": 240,
    "mupen64plus_next": 240,
    "parallel_n64": 240,
    "opera": 240,
    "mednafen_pcfx": 240,
    "ppsspp": 480,
    "azahar": 240,
}

CORE_PRODUCT_COMMANDS = {
    "fbalpha2012_cps1": "go test -tags=integration ./internal/libraryimport -run '^TestFBA2012RealDATImportVariantAndLaunchIsolation$/^CPS1$' -count=1 -timeout=120s",
    "fbalpha2012_cps2": "go test -tags=integration ./internal/libraryimport -run '^TestFBA2012RealDATImportVariantAndLaunchIsolation$/^CPS2$' -count=1 -timeout=120s",
}


def now_ms() -> int:
    return time.time_ns() // 1_000_000


def relative(path: Path, run_dir: Path) -> str:
    return path.relative_to(run_dir).as_posix()


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def git_state() -> tuple[str, bool]:
    commit = subprocess.run(
        ["git", "rev-parse", "HEAD"], cwd=ROOT, text=True, capture_output=True, check=False
    ).stdout.strip()
    dirty = bool(
        subprocess.run(
            ["git", "status", "--porcelain"], cwd=ROOT, text=True, capture_output=True, check=False
        ).stdout
    )
    return commit or "UNBORN", dirty


def all_cases() -> list[str]:
    document = (ROOT / "docs" / "project-acceptance.md").read_text(encoding="utf-8")
    heading_cases = CASE_PATTERN.findall(document)
    core_cases = CORE_CASE_ROW_PATTERN.findall(document)
    if core_cases != list(CORE_CASES):
        raise RuntimeError("ACCEPTANCE_CORE_CASE_CATALOG_INVALID")
    ui_start = heading_cases.index("ACC-UI-001")
    multidisc_start = heading_cases.index("ACC-MDISC-001")
    cases = (
        heading_cases[:ui_start]
        + core_cases
        + heading_cases[multidisc_start:]
        + heading_cases[ui_start:multidisc_start]
    )
    if len(cases) != len(set(cases)) or not cases:
        raise RuntimeError("ACCEPTANCE_CASE_CATALOG_INVALID")
    return cases


def current_run() -> tuple[str, Path]:
    if not CURRENT.is_file():
        raise RuntimeError("先运行 make acceptance-prepare")
    run_id = CURRENT.read_text(encoding="utf-8").strip()
    if not re.fullmatch(r"\d{8}T\d{6}Z-[0-9a-f]{8}", run_id):
        raise RuntimeError("ACCEPTANCE_RUN_ID_INVALID")
    run_dir = ARTIFACTS / run_id
    if not (run_dir / "run.json").is_file():
        raise RuntimeError("ACCEPTANCE_RUN_NOT_FOUND")
    return run_id, run_dir


def prepare() -> int:
    ARTIFACTS.mkdir(parents=True, exist_ok=True)
    run_id = time.strftime("%Y%m%dT%H%M%SZ", time.gmtime()) + "-" + secrets.token_hex(4)
    run_dir = ARTIFACTS / run_id
    (run_dir / "cases").mkdir(parents=True)
    (run_dir / "work" / "data" / "cas").mkdir(parents=True)
    (run_dir / "work" / "seed").mkdir(parents=True)
    (run_dir / "defects.json").write_text("[]\n", encoding="utf-8")

    fixture_manifest = ROOT / "data" / "example" / "fixtures.json"
    fixture_hash = sha256_file(fixture_manifest)
    fixed_seed = {
        "schemaVersion": 2,
        "accounts": [
            {
                "username": "test",
                "role": "ADMIN",
                "status": "ENABLED",
                "profileId": "01980000-0000-7000-8000-000000009991",
            },
            {
                "username": "alice",
                "role": "USER",
                "status": "ENABLED",
                "profileId": "01980000-0000-7000-8000-000000009992",
            },
            {
                "username": "disabled",
                "role": "USER",
                "status": "DISABLED",
                "profileId": "01980000-0000-7000-8000-000000009993",
            },
        ],
        "nowMs": 1786000000000,
        "fixtureManifestSha256": fixture_hash,
        "platformInstances": [
            "acc-arcade-fbneo", "acc-arcade-mame", "acc-nes-fceumm",
            "acc-snes-snes9x", "acc-gb-gambatte", "acc-gba-mgba", "acc-dos-pure",
        ],
    }
    (run_dir / "work" / "seed" / "manifest.json").write_text(
        json.dumps(fixed_seed, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
    )
    (run_dir / "work" / "seed" / "invalid-gba-bios.bin").write_bytes(b"retrom-invalid-bios\n")
    (run_dir / "work" / "seed" / "unsafe.xml").write_text(
        '<!DOCTYPE x [<!ENTITY e SYSTEM "file:///etc/passwd">]><x>&e;</x>\n', encoding="utf-8"
    )

    log_path = run_dir / "prepare.log"
    started = now_ms()
    result = subprocess.run(
        ["make", "deps-check"], cwd=ROOT, text=True, stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT, timeout=300, check=False
    )
    log_path.write_text(result.stdout, encoding="utf-8")
    finished = now_ms()
    commit, dirty = git_state()
    run = {
        "schemaVersion": 1,
        "runId": run_id,
        "status": "PREPARED" if result.returncode == 0 else "PREPARE_FAILED",
        "startedAtMs": started,
        "finishedAtMs": finished,
        "gitCommit": commit,
        "gitDirty": dirty,
        "fixtureManifestSha256": fixture_hash,
        "fakeClockNowMs": 1786000000000,
        "dataRoot": "work/data",
        "seedManifest": "work/seed/manifest.json",
        "prepareLog": "prepare.log",
        "caseCatalog": all_cases(),
    }
    (run_dir / "run.json").write_text(json.dumps(run, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    if result.returncode != 0:
        print(result.stdout, end="", file=sys.stderr)
        print(f"acceptance prepare failed; evidence: .artifacts/acceptance/{run_id}", file=sys.stderr)
        return 1
    CURRENT.write_text(run_id + "\n", encoding="utf-8")
    print(f"run_id={run_id}")
    print(f"evidence=.artifacts/acceptance/{run_id}")
    return 0


def archive_previous(case_dir: Path) -> None:
    result = case_dir / "result.json"
    if not result.exists():
        return
    attempts = case_dir / "attempts"
    attempts.mkdir(exist_ok=True)
    number = 1
    while (attempts / f"{number:03d}").exists():
        number += 1
    target = attempts / f"{number:03d}"
    target.mkdir()
    for name in ("result.json", "stdout.log", "network.json", "core-result.json", "runtime-result.json"):
        source = case_dir / name
        if source.exists():
            shutil.move(source, target / name)
    screenshots = case_dir / "screenshots"
    if screenshots.exists():
        shutil.move(screenshots, target / "screenshots")


def run_command(
    command: str,
    timeout_seconds: int,
    log_path: Path,
    extra_environment: dict[str, str] | None = None,
    append: bool = False,
) -> tuple[int, bool]:
    environment = os.environ.copy()
    if extra_environment:
        environment.update(extra_environment)
    process = subprocess.Popen(
        ["bash", "-lc", command], cwd=ROOT, env=environment,
        text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
        start_new_session=True,
    )
    timed_out = False
    try:
        output, _ = process.communicate(timeout=timeout_seconds)
    except subprocess.TimeoutExpired:
        timed_out = True
        os.killpg(process.pid, signal.SIGTERM)
        try:
            output, _ = process.communicate(timeout=5)
        except subprocess.TimeoutExpired:
            os.killpg(process.pid, signal.SIGKILL)
            output, _ = process.communicate()
    if append:
        with log_path.open("a", encoding="utf-8") as log:
            log.write(output)
    else:
        log_path.write_text(output, encoding="utf-8")
    return process.returncode if process.returncode is not None else 1, timed_out


def conditional_status(case_id: str) -> tuple[str, str] | None:
    if case_id == "ACC-NET-002" and not os.environ.get("RETROM_ACCEPTANCE_BASE_URL"):
        return "NOT_APPLICABLE", "未提供由实际 NG 暴露的 RETROM_ACCEPTANCE_BASE_URL"
    if case_id == "ACC-DAT-006":
        version_dirs = sorted((ROOT / "data" / "dat" / "emulatorjs").glob("*/manifest.json"))
        if len(version_dirs) < 2:
            return "NOT_APPLICABLE", "仓库只有一个已接受的 EmulatorJS/DAT 版本，没有版本升级输入"
    return None


def execute_case(case_id: str) -> int:
    catalog = all_cases()
    if case_id not in catalog:
        print(f"unknown acceptance case: {case_id}", file=sys.stderr)
        return 2
    _, run_dir = current_run()
    run = json.loads((run_dir / "run.json").read_text(encoding="utf-8"))
    if run["status"] != "PREPARED":
        raise RuntimeError("ACCEPTANCE_RUN_NOT_PREPARED")
    case_dir = run_dir / "cases" / case_id.lower()
    case_dir.mkdir(parents=True, exist_ok=True)
    archive_previous(case_dir)
    (case_dir / "screenshots").mkdir(exist_ok=True)
    log_path = case_dir / "stdout.log"
    started = now_ms()
    command = ""
    timed_out = False
    status = "FAIL"
    reason = ""

    conditional = conditional_status(case_id)
    if conditional:
        status, reason = conditional
        log_path.write_text(reason + "\n", encoding="utf-8")
        return_code = 0
    elif case_id in MULTIDISC_RUNTIME_CASES:
        run_id, _expectation = MULTIDISC_RUNTIME_CASES[case_id]
        product_command = MULTIDISC_PRODUCT_COMMANDS[case_id]
        log_path.write_text("[deterministic product contract]\n", encoding="utf-8")
        product_code, product_timeout = run_command(product_command, 300, log_path, append=True)
        verify = f"python3 data/example/verify-fixtures.py {run_id}"
        if product_code != 0 or product_timeout:
            status, reason, command, timed_out, return_code = (
                "FAIL", "多盘产品契约回归测试失败", product_command, product_timeout, product_code
            )
        else:
            with log_path.open("a", encoding="utf-8") as log:
                log.write("\n[controlled runtime fixture verification]\n")
            verify_code, verify_timeout = run_command(verify, 120, log_path, append=True)
        if product_code == 0 and not product_timeout and (verify_code != 0 or verify_timeout):
            status, reason, command, timed_out, return_code = (
                "BLOCKED", "用户授权 Saturn 多盘 ROM/BIOS 夹具缺失或校验失败",
                f"{product_command} && {verify}", verify_timeout, verify_code
            )
        elif product_code == 0 and not product_timeout:
            smoke_command = f"node data/example/smoke-test.mjs {run_id}"
            command = f"{product_command} && {verify} && {smoke_command}"
            timeout = 600 if case_id == "ACC-MDISC-005" else 900
            smoke_output = case_dir / "smoke-output"
            with log_path.open("a", encoding="utf-8") as log:
                log.write("\n[real EmulatorJS multi-disc smoke]\n")
            return_code, timed_out = run_command(
                smoke_command,
                timeout,
                log_path,
                {"RETROM_EXAMPLE_RESULTS_DIR": str(smoke_output)},
                append=True,
            )
            smoke_passed = return_code == 0 and not timed_out
            status = "BLOCKED" if smoke_passed else "FAIL"
            reason = "多盘机器断言通过；必须复核本次截图后才能通过" if smoke_passed else "多盘 Saturn smoke 失败"
            initial_source = smoke_output / f"{run_id}-initial.png"
            post_switch_source = smoke_output / f"{run_id}.png"
            if initial_source.is_file():
                shutil.copy2(initial_source, case_dir / "screenshots" / f"{run_id}.png")
            if post_switch_source.is_file():
                shutil.copy2(post_switch_source, case_dir / "screenshots" / f"{run_id}-post-switch.png")
            latest = smoke_output / "latest.json"
            if latest.is_file():
                shutil.copy2(latest, case_dir / "runtime-result.json")
    elif case_id in CORE_CASES:
        core = CORE_CASES[case_id]
        verify = f"python3 data/example/verify-fixtures.py {core}"
        verify_code, verify_timeout = run_command(verify, 120, log_path)
        if verify_code != 0 or verify_timeout:
            status, reason, command, timed_out, return_code = (
                "BLOCKED", "用户授权 ROM/BIOS 夹具缺失或校验失败", verify, verify_timeout, verify_code
            )
        else:
            product_command = CORE_PRODUCT_COMMANDS.get(core)
            product_failed = False
            if product_command:
                product_code, product_timeout = run_command(
                    product_command, 120, log_path, append=True
                )
                if product_code != 0 or product_timeout:
                    status, reason, command, timed_out, return_code = (
                        "FAIL", "专属 Arcade DAT 产品链路失败", product_command,
                        product_timeout, product_code,
                    )
                    product_failed = True
            if not product_failed:
                smoke_command = f"node data/example/smoke-test.mjs {core}"
                command = " && ".join(filter(None, [verify, product_command, smoke_command]))
                smoke_output = case_dir / "smoke-output"
                return_code, timed_out = run_command(
                    smoke_command,
                    CORE_TIMEOUTS.get(core, 180),
                    log_path,
                    {"RETROM_EXAMPLE_RESULTS_DIR": str(smoke_output)},
                )
                smoke_passed = return_code == 0 and not timed_out
                status = "BLOCKED" if smoke_passed else "FAIL"
                reason = "单核心机器断言通过；必须对本次截图执行视觉复核后才能通过" if smoke_passed else "单核心 smoke 失败"
                run_ids = ["ppsspp-cso", "ppsspp-iso"] if core == "ppsspp" else [core]
                for run_id in run_ids:
                    source = smoke_output / f"{run_id}.png"
                    if source.is_file():
                        shutil.copy2(source, case_dir / "screenshots" / f"{run_id}.png")
                latest = smoke_output / "latest.json"
                if latest.is_file():
                    shutil.copy2(latest, case_dir / "core-result.json")
    elif case_id == "ACC-QA-003":
        command = "validate defects.json regression mappings"
        defects = json.loads((run_dir / "defects.json").read_text(encoding="utf-8"))
        missing = [item for item in defects if item.get("status") == "FIXED" and not all(item.get(key) for key in ("regressionTest", "redEvidence", "greenCommand"))]
        return_code = 0 if not missing else 1
        status = "PASS" if return_code == 0 else "FAIL"
        reason = "无缺陷，回归映射为空数组" if not defects else ("回归映射完整" if not missing else "存在缺少 red/green 映射的已修复缺陷")
        log_path.write_text(reason + "\n", encoding="utf-8")
    elif case_id in CASE_COMMANDS:
        timeout_seconds, command = CASE_COMMANDS[case_id]
        return_code, timed_out = run_command(
            command,
            timeout_seconds,
            log_path,
            {
                "RETROM_ACCEPTANCE_CASE_DIR": str(case_dir),
                "RETROM_ACCEPTANCE_RUN_DIR": str(run_dir),
            },
        )
        status = "PASS" if return_code == 0 and not timed_out else "FAIL"
        reason = "聚焦自动化断言通过" if status == "PASS" else ("命令超时" if timed_out else "聚焦自动化断言失败")
    else:
        return_code = 1
        reason = "UNIMPLEMENTED_ACCEPTANCE_CASE：尚无覆盖该 Case 全部通过标准的可执行检查"
        log_path.write_text(reason + "\n", encoding="utf-8")

    finished = now_ms()
    commit, dirty = git_state()
    evidence = [relative(log_path, run_dir)]
    for path in sorted((case_dir / "screenshots").glob("*.png")):
        evidence.append(relative(path, run_dir))
    core_result = case_dir / "core-result.json"
    if core_result.is_file():
        evidence.append(relative(core_result, run_dir))
    runtime_result = case_dir / "runtime-result.json"
    if runtime_result.is_file():
        evidence.append(relative(runtime_result, run_dir))
    result = {
        "caseId": case_id,
        "status": status,
        "startedAtMs": started,
        "finishedAtMs": finished,
        "durationMs": finished - started,
        "command": command,
        "exitCode": return_code,
        "timedOut": timed_out,
        "gitCommit": commit,
        "gitDirty": dirty,
        "fixtureManifestSha256": run["fixtureManifestSha256"],
        "assertions": [{"name": "registered-case-contract", "passed": status in {"PASS", "NOT_APPLICABLE"}, "details": reason}],
        "evidence": evidence,
    }
    (case_dir / "result.json").write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(f"{case_id}: {status} ({finished - started} ms)")
    print(f"evidence=.artifacts/acceptance/{run_dir.name}/cases/{case_id.lower()}")
    return 0 if status in {"PASS", "NOT_APPLICABLE"} else 1


def review_core(case_id: str, decision: str, observed: str) -> int:
    if case_id not in CORE_CASES or decision not in {"passed", "failed"} or not observed.strip():
        print("usage: run.py review-core ACC-CORE-NNN passed|failed OBSERVED", file=sys.stderr)
        return 2
    _, run_dir = current_run()
    case_dir = run_dir / "cases" / case_id.lower()
    result_path = case_dir / "result.json"
    core = CORE_CASES[case_id]
    run_ids = ["ppsspp-cso", "ppsspp-iso"] if core == "ppsspp" else [core]
    screenshots = [case_dir / "screenshots" / f"{run_id}.png" for run_id in run_ids]
    machine_result = case_dir / "core-result.json"
    if not result_path.is_file() or not all(path.is_file() for path in screenshots) or not machine_result.is_file():
        raise RuntimeError("CORE_REVIEW_EVIDENCE_MISSING：先运行对应 ACC-CORE Case")
    result = json.loads(result_path.read_text(encoding="utf-8"))
    if result.get("status") != "BLOCKED" or result.get("exitCode") != 0 or result.get("timedOut"):
        raise RuntimeError("CORE_MACHINE_ASSERTIONS_NOT_PASSED")
    payload = json.loads(machine_result.read_text(encoding="utf-8"))
    records = [next((item for item in payload.get("results", []) if item.get("core") == run_id), None) for run_id in run_ids]
    if any(not record or record.get("status") != "passed" or record.get("failure") is not None for record in records):
        raise RuntimeError("CORE_MACHINE_RESULT_INVALID")
    reviewed_at = now_ms()
    passed = decision == "passed"
    result["status"] = "PASS" if passed else "FAIL"
    result["finishedAtMs"] = reviewed_at
    result["durationMs"] = reviewed_at - result["startedAtMs"]
    result["assertions"] = [
        {
            "name": "machine-core-contract",
            "passed": True,
            "details": "本次单核心 smoke 的帧、画布、画面统计、隔离和 artifact 断言通过",
        },
        {
            "name": "current-screenshot-visual-review",
            "passed": passed,
            "details": observed.strip(),
        },
    ]
    result["visualReview"] = {
        "reviewedAtMs": reviewed_at,
        "decision": decision,
        "expected": CORE_EXPECTATIONS[core],
        "observed": observed.strip(),
        "screenshots": [
            {"runId": run_id, "sha256": sha256_file(screenshot), "path": relative(screenshot, run_dir)}
            for run_id, screenshot in zip(run_ids, screenshots, strict=True)
        ],
    }
    result_path.write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(f"{case_id}: {result['status']} (visual review recorded)")
    print(f"evidence=.artifacts/acceptance/{run_dir.name}/cases/{case_id.lower()}")
    return 0 if passed else 1


def review_multidisc(case_id: str, decision: str, observed: str) -> int:
    if case_id not in MULTIDISC_RUNTIME_CASES or decision not in {"passed", "failed"} or not observed.strip():
        print("usage: run.py review-multidisc ACC-MDISC-005|006 passed|failed OBSERVED", file=sys.stderr)
        return 2
    _, run_dir = current_run()
    run_id, expectation = MULTIDISC_RUNTIME_CASES[case_id]
    case_dir = run_dir / "cases" / case_id.lower()
    result_path = case_dir / "result.json"
    screenshot = case_dir / "screenshots" / f"{run_id}.png"
    post_switch_screenshot = case_dir / "screenshots" / f"{run_id}-post-switch.png"
    machine_result = case_dir / "runtime-result.json"
    if not result_path.is_file() or not screenshot.is_file() or not post_switch_screenshot.is_file() or not machine_result.is_file():
        raise RuntimeError("MULTIDISC_REVIEW_EVIDENCE_MISSING：先运行对应 ACC-MDISC Case")
    result = json.loads(result_path.read_text(encoding="utf-8"))
    if result.get("status") != "BLOCKED" or result.get("exitCode") != 0 or result.get("timedOut"):
        raise RuntimeError("MULTIDISC_MACHINE_ASSERTIONS_NOT_PASSED")
    payload = json.loads(machine_result.read_text(encoding="utf-8"))
    record = next((item for item in payload.get("results", []) if item.get("core") == run_id), None)
    if not record or record.get("status") != "passed" or record.get("failure") is not None:
        raise RuntimeError("MULTIDISC_MACHINE_RESULT_INVALID")
    reviewed_at = now_ms()
    passed = decision == "passed"
    result["status"] = "PASS" if passed else "FAIL"
    result["finishedAtMs"] = reviewed_at
    result["durationMs"] = reviewed_at - result["startedAtMs"]
    result["assertions"] = [
        {"name": "machine-multidisc-contract", "passed": True, "details": "盘数、换盘回读、帧推进、artifact 与隔离断言通过"},
        {"name": "game-and-post-switch-screenshot-visual-review", "passed": passed, "details": observed.strip()},
    ]
    result["visualReview"] = {
        "reviewedAtMs": reviewed_at,
        "decision": decision,
        "expected": expectation,
        "observed": observed.strip(),
        "screenshots": [
            {"runId": run_id, "phase": "game-before-switch", "sha256": sha256_file(screenshot), "path": relative(screenshot, run_dir)},
            {"runId": run_id, "phase": "post-switch", "sha256": sha256_file(post_switch_screenshot), "path": relative(post_switch_screenshot, run_dir)},
        ],
    }
    result_path.write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(f"{case_id}: {result['status']} (visual review recorded)")
    print(f"evidence=.artifacts/acceptance/{run_dir.name}/cases/{case_id.lower()}")
    return 0 if passed else 1


def report() -> int:
    _, run_dir = current_run()
    catalog = all_cases()
    results: dict[str, dict] = {}
    for case_id in catalog:
        path = run_dir / "cases" / case_id.lower() / "result.json"
        if path.is_file():
            results[case_id] = json.loads(path.read_text(encoding="utf-8"))
    missing = [case_id for case_id in catalog if case_id not in results]
    failed = [case_id for case_id, item in results.items() if item["status"] == "FAIL"]
    blocked = [case_id for case_id, item in results.items() if item["status"] == "BLOCKED"]
    invalid_na = [case_id for case_id, item in results.items() if item["status"] == "NOT_APPLICABLE" and case_id not in CONDITIONAL_CASES]
    passed = [case_id for case_id, item in results.items() if item["status"] == "PASS"]
    not_applicable = [case_id for case_id, item in results.items() if item["status"] == "NOT_APPLICABLE"]
    overall = "PASS" if not missing and not failed and not blocked and not invalid_na else "FAIL"
    defects = json.loads((run_dir / "defects.json").read_text(encoding="utf-8"))
    payload = {
        "schemaVersion": 1,
        "runId": run_dir.name,
        "status": overall,
        "generatedAtMs": now_ms(),
        "counts": {"total": len(catalog), "passed": len(passed), "failed": len(failed), "blocked": len(blocked), "notApplicable": len(not_applicable), "missing": len(missing)},
        "failedCaseIds": failed,
        "blockedCaseIds": blocked,
        "missingCaseIds": missing,
        "notApplicableCaseIds": not_applicable,
        "defects": defects,
    }
    (run_dir / "report.json").write_text(json.dumps(payload, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    lines = [
        f"# Retrom acceptance {run_dir.name}", "", f"Overall: **{overall}**", "",
        f"PASS {len(passed)} / FAIL {len(failed)} / BLOCKED {len(blocked)} / NOT_APPLICABLE {len(not_applicable)} / MISSING {len(missing)}",
        "", f"Failed: {', '.join(failed) or 'none'}", f"Blocked: {', '.join(blocked) or 'none'}",
        f"Missing: {', '.join(missing) or 'none'}", f"Not applicable: {', '.join(not_applicable) or 'none'}", "",
    ]
    (run_dir / "report.md").write_text("\n".join(lines), encoding="utf-8")
    print("\n".join(lines), end="")
    print(f"evidence=.artifacts/acceptance/{run_dir.name}/report.json")
    return 0 if overall == "PASS" else 1


def main() -> int:
    os.chdir(ROOT)
    if len(sys.argv) < 2:
        print("usage: run.py prepare | case CASE_ID | review-core CASE_ID passed|failed OBSERVED | review-multidisc CASE_ID passed|failed OBSERVED | report", file=sys.stderr)
        return 2
    try:
        if sys.argv[1] == "prepare" and len(sys.argv) == 2:
            return prepare()
        if sys.argv[1] == "case" and len(sys.argv) == 3:
            return execute_case(sys.argv[2])
        if sys.argv[1] == "report" and len(sys.argv) == 2:
            return report()
        if sys.argv[1] == "review-core" and len(sys.argv) == 5:
            return review_core(sys.argv[2], sys.argv[3], sys.argv[4])
        if sys.argv[1] == "review-multidisc" and len(sys.argv) == 5:
            return review_multidisc(sys.argv[2], sys.argv[3], sys.argv[4])
    except (OSError, RuntimeError, ValueError, json.JSONDecodeError, subprocess.TimeoutExpired) as error:
        print(str(error), file=sys.stderr)
        return 1
    print("usage: run.py prepare | case CASE_ID | review-core CASE_ID passed|failed OBSERVED | review-multidisc CASE_ID passed|failed OBSERVED | report", file=sys.stderr)
    return 2


if __name__ == "__main__":
    raise SystemExit(main())
