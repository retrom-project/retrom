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
    "ACC-PKG-001": (900, "make build-backend-image && docker image inspect retrom:latest"),
    "ACC-PKG-002": (900, "make build-web-image && docker image inspect retrom-web:latest"),
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
    "ACC-SEC-003": (120, "go test ./internal/httpapi -run '^TestWritesIgnoreBrowserOriginWithoutEnablingCORS$' -count=1"),
    "ACC-SEC-004": (120, "go test ./internal/hasheous -run 'TestLookupNormalizesBoundedResponse|TestLookupClassifiesMissAndOversize|TestFetchAssetValidatesImageAndEveryRedirect' -count=1"),
    "ACC-API-001": (120, "go test ./internal/httpapi ./internal/cursor -count=1"),
    "ACC-OPS-001": (
        120,
        "go test ./internal/config ./internal/httpapi -run 'TestRejectUnknownVariablesAllowsToolPrefixesOnly|TestDiagnosticsUsesClosedSnapshotSchemaAndRequiredHeaders' -count=1 && go test -tags=integration ./internal/httpapi -run '^TestReadinessGatesBusinessRoutesDuringDATIndexing$' -count=1",
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
        "go test -tags=integration ./internal/metadatascrape ./internal/libraryimport -run 'TestImportPersistsHasheousEvidenceCandidateAndAsset|TestArcadeHasheousEvidenceUsesMatchedDATEntriesOnly|TestImportGroupsSingleArchiveMemberAndReportsEveryFile|TestArcadeGroupingBuildsCoreScopedParentAndBIOSClosure' -count=1",
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
        "go test -tags=integration ./internal/libraryimport ./internal/metadatascrape -run 'TestUploadImportReviewPublishPipeline|TestImportPersistsHasheousEvidenceCandidateAndAsset' -count=1",
    ),
    "ACC-IMP-008": (180, "go test ./internal/jobs -run '^TestCancelAndRetryEnforceVersionedState$' -count=1"),
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
    "ACC-BIOS-001": (120, "go test -tags=integration ./internal/firmware -run '^TestStaticBIOSHashMismatchIsInstalledAsWarning$' -count=1"),
    "ACC-BIOS-002": (
        180,
        "go test -tags=integration ./internal/dependencies ./internal/launch ./internal/libraryimport ./internal/firmware -run 'TestBIOSActivationOptionsRejectConflictingSeed|TestPublishedGameLaunchLocksContentAndCredential|TestArcadeGroupingBuildsCoreScopedParentAndBIOSClosure|TestStaticBIOSHashMismatchIsInstalledAsWarning' -count=1",
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
        "go test -tags=integration ./internal/saves -run '^TestPersistentSaveLocksLaunchBaseAndEnforcesSequence$' -count=1 && make web-test",
    ),
    "ACC-PLAY-001": (120, "go test -tags=integration ./internal/launch -run '^TestPublishedGameLaunchLocksContentAndCredential$' -count=1"),
    "ACC-UI-001": (180, "scripts/acceptance/ui-case.sh ACC-UI-001"),
    "ACC-UI-002": (180, "scripts/acceptance/ui-case.sh ACC-UI-002"),
    "ACC-UI-003": (180, "scripts/acceptance/ui-case.sh ACC-UI-003"),
    "ACC-UI-004": (180, "scripts/acceptance/ui-case.sh ACC-UI-004"),
    "ACC-UI-005": (180, "scripts/acceptance/ui-case.sh ACC-UI-005"),
    "ACC-UI-006": (180, "scripts/acceptance/ui-case.sh ACC-UI-006"),
    "ACC-UI-007": (180, "scripts/acceptance/ui-case.sh ACC-UI-007"),
    "ACC-UI-008": (180, "scripts/acceptance/ui-case.sh ACC-UI-008"),
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
    cases = heading_cases[:ui_start] + core_cases + heading_cases[ui_start:]
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
        "schemaVersion": 1,
        "profile": "local",
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
    for name in ("result.json", "stdout.log", "network.json", "core-result.json"):
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
    elif case_id in CORE_CASES:
        core = CORE_CASES[case_id]
        verify = "python3 data/example/verify-fixtures.py"
        verify_code, verify_timeout = run_command(verify, 120, log_path)
        if verify_code != 0 or verify_timeout:
            status, reason, command, timed_out, return_code = (
                "BLOCKED", "用户授权 ROM/BIOS 夹具缺失或校验失败", verify, verify_timeout, verify_code
            )
        else:
            command = f"node data/example/smoke-test.mjs {core}"
            return_code, timed_out = run_command(command, 180, log_path)
            smoke_passed = return_code == 0 and not timed_out
            status = "BLOCKED" if smoke_passed else "FAIL"
            reason = "单核心机器断言通过；必须对本次截图执行视觉复核后才能通过" if smoke_passed else "单核心 smoke 失败"
            source = ROOT / "data" / "example" / "results" / f"{core}.png"
            if source.is_file():
                shutil.copy2(source, case_dir / "screenshots" / f"{core}.png")
            latest = ROOT / "data" / "example" / "results" / "latest.json"
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
    screenshot = case_dir / "screenshots" / f"{CORE_CASES[case_id]}.png"
    machine_result = case_dir / "core-result.json"
    if not result_path.is_file() or not screenshot.is_file() or not machine_result.is_file():
        raise RuntimeError("CORE_REVIEW_EVIDENCE_MISSING：先运行对应 ACC-CORE Case")
    result = json.loads(result_path.read_text(encoding="utf-8"))
    if result.get("status") != "BLOCKED" or result.get("exitCode") != 0 or result.get("timedOut"):
        raise RuntimeError("CORE_MACHINE_ASSERTIONS_NOT_PASSED")
    payload = json.loads(machine_result.read_text(encoding="utf-8"))
    core = CORE_CASES[case_id]
    record = next((item for item in payload.get("results", []) if item.get("core") == core), None)
    if not record or record.get("status") != "passed" or record.get("failure") is not None:
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
        "screenshotSha256": sha256_file(screenshot),
        "screenshot": relative(screenshot, run_dir),
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
        print("usage: run.py prepare | case CASE_ID | review-core CASE_ID passed|failed OBSERVED | report", file=sys.stderr)
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
    except (OSError, RuntimeError, ValueError, json.JSONDecodeError, subprocess.TimeoutExpired) as error:
        print(str(error), file=sys.stderr)
        return 1
    print("usage: run.py prepare | case CASE_ID | review-core CASE_ID passed|failed OBSERVED | report", file=sys.stderr)
    return 2


if __name__ == "__main__":
    raise SystemExit(main())
