#!/usr/bin/env python3
"""Read-only database evidence inspector for ACC-RPG-009.

This command never creates acceptance state.  It verifies product-created HTTP
observations against the dedicated Retrom database and emits a safe JSON
projection suitable for the Case evidence bundle.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import sqlite3
import sys
import time
from pathlib import Path
from typing import Any


UPLOAD_ROLES = {
    "rpg2000Rtp", "rpg2003Rtp", "rgss1StandardV1", "rgss1StandardV2", "rgss1Custom",
    "rgss2StandardV1", "rgss2StandardV2", "rgss2Custom", "rgss3StandardV1",
    "rgss3StandardV2", "rgss3Custom", "zeroReference",
}
UUID = re.compile(r"^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$")
SHA256 = re.compile(r"^[0-9a-f]{64}$")


class InspectError(RuntimeError):
    """The observed product state does not satisfy ACC-RPG-009."""


def load_json(path: Path, label: str) -> dict[str, Any]:
    if not path.is_absolute() or not path.is_file() or path.is_symlink():
        raise InspectError(f"RPG_ACCEPTANCE_PACK_{label}_INVALID")
    value = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(value, dict):
        raise InspectError(f"RPG_ACCEPTANCE_PACK_{label}_INVALID")
    return value


def without_source_paths(rows: dict[str, Any]) -> dict[str, Any]:
    return {
        role: {key: value for key, value in row.items() if key != "sourcePath"}
        for role, row in rows.items() if isinstance(row, dict)
    }


def validate_repository_provenance(value: Any) -> None:
    if not isinstance(value, dict) or set(value) != {"gitCommit", "gitDirty", "gitDirtySummary"} or \
            not re.fullmatch(r"(?:[0-9a-f]{40}|UNBORN)", str(value.get("gitCommit"))):
        raise InspectError("RPG_ACCEPTANCE_PACK_PROVISION_REPOSITORY_INVALID")
    summary = value.get("gitDirtySummary")
    if not isinstance(summary, dict) or set(summary) != {"fileCount", "sha256", "entries"} or \
            not SHA256.fullmatch(str(summary.get("sha256"))):
        raise InspectError("RPG_ACCEPTANCE_PACK_PROVISION_REPOSITORY_INVALID")
    entries = summary.get("entries")
    if not isinstance(entries, list) or summary.get("fileCount") != len(entries) or \
            value.get("gitDirty") is not bool(entries):
        raise InspectError("RPG_ACCEPTANCE_PACK_PROVISION_REPOSITORY_INVALID")
    for entry in entries:
        if not isinstance(entry, dict) or set(entry) != {"status", "path"} or \
                not isinstance(entry.get("status"), str) or not isinstance(entry.get("path"), str) or \
                Path(entry["path"]).is_absolute():
            raise InspectError("RPG_ACCEPTANCE_PACK_PROVISION_REPOSITORY_INVALID")
    encoded = json.dumps(entries, ensure_ascii=False, separators=(",", ":")).encode()
    if hashlib.sha256(encoded).hexdigest() != summary["sha256"]:
        raise InspectError("RPG_ACCEPTANCE_PACK_PROVISION_REPOSITORY_INVALID")


def provisioning_evidence(plan: dict[str, Any]) -> dict[str, Any]:
    value = os.environ.get("RETROM_ACC_RPG_009_PROVISION_EVIDENCE", "")
    evidence_path = Path(value)
    evidence = load_json(evidence_path, "PROVISION_EVIDENCE")
    expected_keys = {
        "schemaVersion", "caseId", "status", "generatorInputIdentity", "planIdentity", "counts", "repository",
    }
    if set(evidence) != expected_keys or evidence.get("schemaVersion") != 1 or \
            evidence.get("caseId") != "ACC-RPG-009" or evidence.get("status") != "PROVISIONED":
        raise InspectError("RPG_ACCEPTANCE_PACK_PROVISION_EVIDENCE_INVALID")
    plan_identity = evidence.get("planIdentity")
    expected_plan = {
        "schemaVersion": plan.get("schemaVersion"),
        "uploads": without_source_paths(plan.get("uploads", {})),
        "reviewIds": plan.get("reviewIds"),
        "protectedReferences": plan.get("protectedReferences"),
    }
    if plan_identity != expected_plan:
        raise InspectError("RPG_ACCEPTANCE_PACK_PROVISION_PLAN_IDENTITY_INVALID")
    inputs = evidence.get("generatorInputIdentity")
    input_keys = {
        "schemaVersion", "license", "licenseSource", "copyright", "inputs", "reviewProjects",
        "protectedPackInputs", "protectedProjects",
    }
    if not isinstance(inputs, dict) or set(inputs) != input_keys or inputs.get("schemaVersion") != 1 or \
            inputs.get("license") != "MIT" or inputs.get("licenseSource") != \
            "testdata/public-roms/rpgmaker-smoke/LICENSE" or inputs.get("inputs") != expected_plan["uploads"] or \
            set(inputs.get("reviewProjects", {})) != set(plan.get("reviewIds", {})) or \
            set(inputs.get("protectedPackInputs", {})) != {"publishedVariant", "restorableCheckpoint"} or \
            set(inputs.get("protectedProjects", {})) != {"publishedVariant", "restorableCheckpoint"}:
        raise InspectError("RPG_ACCEPTANCE_PACK_PROVISION_INPUT_IDENTITY_INVALID")
    counts = evidence.get("counts")
    if counts != {
        "protectedInstallationCount": 2, "protectedGameCount": 2,
        "reviewItemCount": 13, "readyUnapprovedReviewCount": 5,
    }:
        raise InspectError("RPG_ACCEPTANCE_PACK_PROVISION_COUNTS_INVALID")
    validate_repository_provenance(evidence.get("repository"))
    stack = [evidence]
    while stack:
        item = stack.pop()
        if isinstance(item, dict):
            if "sourcePath" in item:
                raise InspectError("RPG_ACCEPTANCE_PACK_PROVISION_PATH_LEAK")
            stack.extend(item.values())
        elif isinstance(item, list):
            stack.extend(item)
    return {
        "documentSha256": hashlib.sha256(evidence_path.read_bytes()).hexdigest(),
        "payload": evidence,
    }


def open_read_only(path: Path) -> sqlite3.Connection:
    if not path.is_absolute() or not path.is_file() or path.is_symlink():
        raise InspectError("RPG_ACCEPTANCE_PACK_DATABASE_INVALID")
    connection = sqlite3.connect(f"{path.resolve().as_uri()}?mode=ro", uri=True, timeout=2)
    connection.row_factory = sqlite3.Row
    connection.execute("PRAGMA query_only=ON")
    connection.execute("PRAGMA foreign_keys=ON")
    return connection


def one(connection: sqlite3.Connection, query: str, arguments: tuple[Any, ...], label: str) -> sqlite3.Row:
    rows = connection.execute(query, arguments).fetchall()
    if len(rows) != 1:
        raise InspectError(f"RPG_ACCEPTANCE_PACK_{label}_INVALID")
    return rows[0]


def job_evidence(
    connection: sqlite3.Connection,
    job_id: str,
    kind: str,
    scope_type: str,
    scope_id: str,
    expected_input: dict[str, Any] | None,
) -> dict[str, Any]:
    row = one(connection, """
SELECT id,kind,scope_type,scope_id,state,execution_no,attempt_count,finished_at_ms
FROM jobs WHERE id=?
""", (job_id,), "JOB")
    if (row["kind"], row["scope_type"], row["scope_id"], row["state"]) != (
        kind, scope_type, scope_id, "SUCCEEDED",
    ) or row["finished_at_ms"] is None:
        raise InspectError("RPG_ACCEPTANCE_PACK_JOB_STATE_INVALID")
    events = [item["event_type"] for item in connection.execute(
        "SELECT event_type FROM job_events WHERE job_id=? ORDER BY id", (job_id,),
    )]
    required_events = {"SUCCEEDED"} if kind == "UPLOAD_FINALIZE" else {"QUEUED", "STARTED", "SUCCEEDED"}
    if not events or events[-1] != "SUCCEEDED" or not required_events <= set(events):
        raise InspectError("RPG_ACCEPTANCE_PACK_JOB_EVENTS_INVALID")
    evidence: dict[str, Any] = {
        "jobId": row["id"], "kind": row["kind"], "scopeType": row["scope_type"],
        "scopeId": row["scope_id"], "state": row["state"], "executionNo": row["execution_no"],
        "attemptCount": row["attempt_count"], "events": events,
    }
    if expected_input is not None:
        snapshot = one(connection, """
SELECT input_json,input_digest FROM job_input_snapshots WHERE job_id=? AND execution_no=?
""", (job_id, row["execution_no"]), "JOB_INPUT")
        encoded = snapshot["input_json"].encode("utf-8")
        decoded = json.loads(encoded)
        if hashlib.sha256(encoded).hexdigest() != snapshot["input_digest"] or decoded != expected_input:
            raise InspectError("RPG_ACCEPTANCE_PACK_JOB_INPUT_INVALID")
        if kind == "PAYLOAD_RELEASE" and not UUID.fullmatch(str(decoded.get("executionId"))):
            raise InspectError("RPG_ACCEPTANCE_PACK_JOB_INPUT_INVALID")
        evidence["inputDigest"] = snapshot["input_digest"]
    return evidence


def installation_evidence(
    connection: sqlite3.Connection,
    role: str,
    observed: dict[str, Any],
) -> dict[str, Any]:
    upload_id = observed.get("uploadId")
    installation_id = observed.get("installationId")
    if not UUID.fullmatch(str(upload_id)) or not UUID.fullmatch(str(installation_id)):
        raise InspectError("RPG_ACCEPTANCE_PACK_UPLOAD_ID_INVALID")
    session = one(connection, """
SELECT purpose,state,source_type,total_files,total_bytes,finalize_job_id
FROM upload_sessions WHERE id=?
""", (upload_id,), "UPLOAD_SESSION")
    if session["purpose"] != "RUNTIME_ASSET_PACK" or session["state"] != "COMPLETE":
        raise InspectError("RPG_ACCEPTANCE_PACK_UPLOAD_SESSION_INVALID")
    consumption = one(connection, """
SELECT id,consumer_type,consumer_id,released_at_ms,release_reason
FROM upload_consumptions WHERE upload_session_id=?
""", (upload_id,), "UPLOAD_CONSUMPTION")
    if (consumption["consumer_type"], consumption["consumer_id"]) != (
        "RUNTIME_ASSET_PACK_INSTALLATION", installation_id,
    ):
        raise InspectError("RPG_ACCEPTANCE_PACK_UPLOAD_CONSUMPTION_INVALID")
    installation = one(connection, """
SELECT status,definition_id,files_digest,file_count,total_bytes,bundle_sha256,deleted_at_ms
FROM runtime_asset_pack_installations WHERE id=?
""", (installation_id,), "INSTALLATION")
    file_count = connection.execute(
        "SELECT count(*) FROM runtime_asset_pack_files WHERE installation_id=?", (installation_id,),
    ).fetchone()[0]
    is_zero = role == "zeroReference"
    if is_zero:
        if installation["status"] != "DELETED" or installation["deleted_at_ms"] is None or file_count != 0:
            raise InspectError("RPG_ACCEPTANCE_PACK_ZERO_INSTALLATION_INVALID")
    else:
        if installation["status"] != "READY" or file_count != installation["file_count"]:
            raise InspectError("RPG_ACCEPTANCE_PACK_READY_INSTALLATION_INVALID")
        if consumption["released_at_ms"] is not None or consumption["release_reason"] is not None:
            raise InspectError("RPG_ACCEPTANCE_PACK_UPLOAD_CONSUMPTION_INVALID")
    browser_installation = observed.get("catalog")
    if not isinstance(browser_installation, dict):
        raise InspectError("RPG_ACCEPTANCE_PACK_CATALOG_EVIDENCE_INVALID")
    for key, column in (("definitionId", "definition_id"), ("filesDigest", "files_digest")):
        if browser_installation.get(key) != installation[column]:
            raise InspectError("RPG_ACCEPTANCE_PACK_CATALOG_EVIDENCE_INVALID")
    finalize = observed.get("finalizeJob")
    validation = observed.get("validationJob")
    if not isinstance(finalize, dict) or not isinstance(validation, dict) or \
            session["finalize_job_id"] != finalize.get("jobId") or validation.get("jobId") != observed.get("jobId"):
        raise InspectError("RPG_ACCEPTANCE_PACK_JOB_RELATION_INVALID")
    finalize_job = job_evidence(
        connection, finalize["jobId"], "UPLOAD_FINALIZE", "UPLOAD_SESSION", upload_id, None,
    )
    validation_job = job_evidence(
        connection, validation["jobId"], "RUNTIME_ASSET_PACK_VALIDATE",
        "RUNTIME_ASSET_PACK_INSTALLATION", installation_id,
        {"installationId": installation_id, "schemaVersion": 1},
    )
    return {
        "uploadId": upload_id, "installationId": installation_id,
        "consumptionId": consumption["id"], "sessionState": session["state"],
        "sourceType": session["source_type"], "sourceFileCount": session["total_files"],
        "sourceSizeBytes": session["total_bytes"], "consumptionReleasedAtMs": consumption["released_at_ms"],
        "consumptionReleaseReason": consumption["release_reason"],
        "finalizeJob": finalize_job, "validationJob": validation_job,
    }


def protected_variant(
    connection: sqlite3.Connection,
    reference: dict[str, Any],
) -> dict[str, Any]:
    row = one(connection, """
SELECT revision.id AS revision_id,game.status AS game_status,revision.status AS revision_status,
 artifact.available_for_launch,pack.definition_id,pack.installation_id,installation.files_digest
FROM games game
JOIN game_variants variant ON variant.game_id=game.id
JOIN game_variant_revisions revision ON revision.id=variant.current_revision_id
JOIN core_artifacts artifact ON artifact.id=revision.core_artifact_id
JOIN game_variant_revision_runtime_packs pack ON pack.game_variant_revision_id=revision.id
JOIN runtime_asset_pack_installations installation ON installation.id=pack.installation_id
WHERE game.id=? AND pack.installation_id=?
""", (reference["gameId"], reference["installationId"]), "PUBLISHED_REFERENCE")
    if row["game_status"] != "PUBLISHED" or row["revision_status"] != "READY" or \
            row["available_for_launch"] != 1 or row["definition_id"] != "rgss1_standard":
        raise InspectError("RPG_ACCEPTANCE_PACK_PUBLISHED_REFERENCE_INVALID")
    return {
        "gameId": reference["gameId"], "installationId": reference["installationId"],
        "variantRevisionId": row["revision_id"], "definitionId": row["definition_id"],
        "filesDigest": row["files_digest"], "gameStatus": row["game_status"],
        "variantStatus": row["revision_status"], "availableForLaunch": True,
    }


def protected_checkpoint(
    connection: sqlite3.Connection,
    reference: dict[str, Any],
) -> dict[str, Any]:
    row = one(connection, """
SELECT save.game_variant_revision_id AS revision_id,game.status AS game_status,
 revision.status AS revision_status,artifact.available_for_launch,pack.definition_id,
 pack.installation_id,installation.files_digest,source.purpose AS source_purpose,
 save.payload_sha256,save.payload_size_bytes
FROM save_states save
JOIN games game ON game.id=save.game_id
JOIN game_variant_revisions revision ON revision.id=save.game_variant_revision_id
JOIN core_artifacts artifact ON artifact.id=save.core_artifact_id
JOIN game_variant_revision_runtime_packs pack ON pack.game_variant_revision_id=save.game_variant_revision_id
JOIN runtime_asset_pack_installations installation ON installation.id=pack.installation_id
JOIN launch_sessions source ON source.id=save.source_launch_session_id
  AND source.game_id=save.game_id AND source.game_variant_revision_id=save.game_variant_revision_id
WHERE save.id=? AND save.game_id=? AND save.deleted_at_ms IS NULL AND pack.installation_id=?
""", (reference["saveStateId"], reference["gameId"], reference["installationId"]), "CHECKPOINT_REFERENCE")
    if row["game_status"] != "PUBLISHED" or row["revision_status"] != "READY" or \
            row["available_for_launch"] != 1 or row["definition_id"] != "rgss2_rpgvx" or \
            row["source_purpose"] != "PRODUCT" or not SHA256.fullmatch(row["payload_sha256"]):
        raise InspectError("RPG_ACCEPTANCE_PACK_CHECKPOINT_REFERENCE_INVALID")
    return {
        "gameId": reference["gameId"], "saveStateId": reference["saveStateId"],
        "installationId": reference["installationId"], "variantRevisionId": row["revision_id"],
        "definitionId": row["definition_id"], "filesDigest": row["files_digest"],
        "payloadSha256": row["payload_sha256"], "payloadSizeBytes": row["payload_size_bytes"],
        "gameStatus": row["game_status"], "variantStatus": row["revision_status"],
        "availableForLaunch": True, "sourceLaunchPurpose": row["source_purpose"],
    }


def published_reviews(connection: sqlite3.Connection, observed: dict[str, Any]) -> list[dict[str, Any]]:
    result = []
    for item in observed.get("reviews", {}).get("published", []):
        row = one(connection, """
SELECT game.status,content.source_ref_id,profile.generation
FROM games game
JOIN game_content_revisions content ON content.id=game.current_content_revision_id
JOIN game_variants variant ON variant.game_id=game.id
JOIN game_variant_revisions revision ON revision.id=variant.current_revision_id
JOIN rpgmaker_variant_profiles profile ON profile.game_variant_revision_id=revision.id
WHERE game.id=?
""", (item.get("gameId"),), "PUBLISHED_REVIEW")
        if row["status"] != "PUBLISHED" or row["source_ref_id"] != item.get("itemId") or \
                row["generation"] != item.get("generation"):
            raise InspectError("RPG_ACCEPTANCE_PACK_PUBLISHED_REVIEW_INVALID")
        result.append({
            "role": item.get("role"), "itemId": item.get("itemId"), "gameId": item.get("gameId"),
            "generation": row["generation"], "status": row["status"],
        })
    return result


def selected_reviews(connection: sqlite3.Connection, observed: dict[str, Any]) -> list[dict[str, Any]]:
    result = []
    for item in observed.get("reviews", {}).get("matcherRejections", []):
        if item.get("matcher") not in {"SELECTED", "AMBIGUOUS"}:
            continue
        row = one(connection, """
SELECT selection.slot,selection.installation_id,draft.runtime_binding_revision,
 validation.runtime_binding_revision AS validation_binding_revision
FROM review_drafts draft
JOIN review_draft_runtime_pack_selections selection ON selection.review_draft_id=draft.id
LEFT JOIN rpgmaker_runtime_validations validation ON validation.id=(
 SELECT current.id FROM rpgmaker_runtime_validations current
 WHERE current.import_item_id=draft.import_item_id
 ORDER BY current.created_at_ms DESC,current.id DESC LIMIT 1)
WHERE draft.import_item_id=?
""", (item.get("itemId"),), "REVIEW_SELECTION")
        if row["installation_id"] != item.get("installationId") or \
                row["runtime_binding_revision"] == row["validation_binding_revision"]:
            raise InspectError("RPG_ACCEPTANCE_PACK_REVIEW_SELECTION_INVALID")
        result.append({
            "role": item.get("role"), "itemId": item.get("itemId"),
            "slot": row["slot"], "installationId": row["installation_id"],
            "runtimeBindingRevision": row["runtime_binding_revision"],
            "validationBindingRevision": row["validation_binding_revision"],
        })
    return result


def zero_release(
    connection: sqlite3.Connection,
    upload: dict[str, Any],
    browser_installation: dict[str, Any],
    deadline: float,
) -> dict[str, Any]:
    consumption_id = upload["consumptionId"]
    while True:
        row = connection.execute("""
SELECT consumption.released_at_ms,consumption.release_reason,job.id AS job_id,job.state,
 job.finished_at_ms
FROM upload_consumptions consumption
LEFT JOIN jobs job ON job.kind='PAYLOAD_RELEASE' AND job.scope_type='UPLOAD_CONSUMPTION'
 AND job.scope_id=consumption.id
WHERE consumption.id=?
""", (consumption_id,)).fetchone()
        if row is not None and row["state"] in {"SUCCEEDED", "FAILED", "CANCELLED"}:
            break
        if time.monotonic() >= deadline:
            raise InspectError("RPG_ACCEPTANCE_PACK_PAYLOAD_RELEASE_TIMEOUT")
        time.sleep(0.25)
    if row["state"] != "SUCCEEDED" or row["released_at_ms"] is None or \
            row["release_reason"] != "UPLOAD_CONSUMED":
        raise InspectError("RPG_ACCEPTANCE_PACK_PAYLOAD_RELEASE_INVALID")
    release_job = job_evidence(
        connection, row["job_id"], "PAYLOAD_RELEASE", "UPLOAD_CONSUMPTION", consumption_id,
        {
            "schemaVersion": 1, "kind": "PAYLOAD_RELEASE",
            "scope": {"type": "UPLOAD_CONSUMPTION", "id": consumption_id},
            "executionId": one(connection, """
SELECT json_extract(input_json,'$.executionId') AS execution_id
FROM job_input_snapshots WHERE job_id=? AND execution_no=1
""", (row["job_id"],), "PAYLOAD_RELEASE_INPUT")["execution_id"],
            "inputs": {"scopeVersion": 1, "reason": "UPLOAD_CONSUMED"},
        },
    )
    files = one(connection, """
SELECT count(*) AS total,sum(CASE WHEN state='PURGED' AND final_blob_id IS NULL
 AND payload_released_at_ms IS NOT NULL THEN 1 ELSE 0 END) AS purged
FROM upload_files WHERE upload_session_id=?
""", (upload["uploadId"],), "PURGED_UPLOAD_FILES")
    if files["total"] < 1 or files["purged"] != files["total"]:
        raise InspectError("RPG_ACCEPTANCE_PACK_UPLOAD_PURGE_INVALID")
    bundle_sha = browser_installation.get("bundleSha256")
    if not SHA256.fullmatch(str(bundle_sha)):
        raise InspectError("RPG_ACCEPTANCE_PACK_BUNDLE_DIGEST_INVALID")
    gc = one(connection, """
SELECT candidate.first_unreferenced_at_ms,candidate.scheduled_at_ms,candidate.deleted_at_ms
FROM blobs blob JOIN blob_gc_candidates candidate ON candidate.blob_id=blob.id
WHERE blob.sha256=?
""", (bundle_sha,), "GC_CANDIDATE")
    if gc["deleted_at_ms"] is not None or gc["scheduled_at_ms"] <= gc["first_unreferenced_at_ms"]:
        raise InspectError("RPG_ACCEPTANCE_PACK_GC_RETENTION_INVALID")
    audits = connection.execute("""
SELECT count(*) FROM audit_events
WHERE actor_label='payload-release-worker' AND action='PAYLOAD_RELEASE_COMPLETED'
 AND resource_type='UPLOAD_CONSUMPTION' AND resource_id=?
""", (consumption_id,)).fetchone()[0]
    if audits != 1:
        raise InspectError("RPG_ACCEPTANCE_PACK_PAYLOAD_RELEASE_AUDIT_INVALID")
    return {
        "consumptionId": consumption_id, "releasedAtMs": row["released_at_ms"],
        "releaseReason": row["release_reason"], "job": release_job,
        "uploadFileCount": files["total"], "purgedFileCount": files["purged"],
        "bundleSha256": bundle_sha, "gcFirstUnreferencedAtMs": gc["first_unreferenced_at_ms"],
        "gcScheduledAtMs": gc["scheduled_at_ms"], "completionAuditCount": audits,
    }


def inspect(database: Path, plan: dict[str, Any], observed: dict[str, Any], timeout: float = 45) -> dict[str, Any]:
    if plan.get("schemaVersion") != 2 or set(plan.get("uploads", {})) != UPLOAD_ROLES or \
            set(plan.get("protectedReferences", {})) != {"publishedVariant", "restorableCheckpoint"} or \
            observed.get("schemaVersion") != 1 or observed.get("caseId") != "ACC-RPG-009" or \
            observed.get("status") != "OBSERVED" or set(observed.get("uploads", {})) != UPLOAD_ROLES or \
            set(observed.get("installations", {})) != UPLOAD_ROLES:
        raise InspectError("RPG_ACCEPTANCE_PACK_INSPECT_INPUT_INVALID")
    connection = open_read_only(database)
    try:
        if connection.execute("PRAGMA quick_check").fetchone()[0] != "ok":
            raise InspectError("RPG_ACCEPTANCE_PACK_DATABASE_INTEGRITY_INVALID")
        upload_evidence: dict[str, dict[str, Any]] = {}
        for role in sorted(UPLOAD_ROLES):
            browser = dict(observed["uploads"][role])
            browser["catalog"] = observed["installations"][role]
            upload_evidence[role] = installation_evidence(connection, role, browser)
        references = plan.get("protectedReferences", {})
        protected = {
            "publishedVariant": protected_variant(connection, references.get("publishedVariant", {})),
            "restorableCheckpoint": protected_checkpoint(
                connection, references.get("restorableCheckpoint", {}),
            ),
        }
        zero = zero_release(
            connection, upload_evidence["zeroReference"], observed["installations"]["zeroReference"],
            time.monotonic() + timeout,
        )
        upload_evidence["zeroReference"]["consumptionReleasedAtMs"] = zero["releasedAtMs"]
        upload_evidence["zeroReference"]["consumptionReleaseReason"] = zero["releaseReason"]
        return {
            "schemaVersion": 1, "uploads": upload_evidence,
            "publishedReviews": published_reviews(connection, observed),
            "selectedReviews": selected_reviews(connection, observed),
            "protectedReferences": protected, "zeroReferenceRelease": zero,
            "provisioningEvidence": provisioning_evidence(plan),
        }
    finally:
        connection.close()


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--database", type=Path, required=True)
    parser.add_argument("--plan", type=Path, required=True)
    parser.add_argument("--evidence", type=Path, required=True)
    parser.add_argument("--timeout-seconds", type=float, default=45)
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    try:
        result = inspect(
            args.database, load_json(args.plan, "PLAN"), load_json(args.evidence, "EVIDENCE"),
            args.timeout_seconds,
        )
    except (InspectError, OSError, ValueError, sqlite3.Error, json.JSONDecodeError) as error:
        print(str(error), file=sys.stderr)
        return 1
    print(json.dumps(result, separators=(",", ":"), sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
