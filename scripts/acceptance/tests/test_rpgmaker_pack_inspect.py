from __future__ import annotations

import hashlib
import importlib.util
import json
import os
import sqlite3
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from scripts.acceptance.tests.test_rpgmaker_case import pack_evidence_payload, pack_job, pack_uuid


MODULE_PATH = Path(__file__).resolve().parents[1] / "rpgmaker_pack_inspect.py"
SPEC = importlib.util.spec_from_file_location("rpgmaker_pack_inspect", MODULE_PATH)
assert SPEC and SPEC.loader
inspector = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(inspector)


SCHEMA = """
CREATE TABLE upload_sessions(id TEXT,purpose TEXT,state TEXT,source_type TEXT,total_files INTEGER,total_bytes INTEGER,finalize_job_id TEXT);
CREATE TABLE upload_consumptions(id TEXT,upload_session_id TEXT,consumer_type TEXT,consumer_id TEXT,released_at_ms INTEGER,release_reason TEXT);
CREATE TABLE runtime_asset_pack_installations(id TEXT,status TEXT,definition_id TEXT,files_digest TEXT,file_count INTEGER,total_bytes INTEGER,bundle_sha256 TEXT,deleted_at_ms INTEGER);
CREATE TABLE runtime_asset_pack_files(installation_id TEXT);
CREATE TABLE jobs(id TEXT,kind TEXT,scope_type TEXT,scope_id TEXT,state TEXT,execution_no INTEGER,attempt_count INTEGER,finished_at_ms INTEGER);
CREATE TABLE job_events(id INTEGER PRIMARY KEY AUTOINCREMENT,job_id TEXT,event_type TEXT);
CREATE TABLE job_input_snapshots(job_id TEXT,execution_no INTEGER,input_json TEXT,input_digest TEXT);
CREATE TABLE games(id TEXT,status TEXT,content_source_ref_id TEXT);
CREATE TABLE game_variants(id TEXT,game_id TEXT,status TEXT,provider_id TEXT,target_id TEXT);
CREATE TABLE runtime_providers(provider_id TEXT PRIMARY KEY,bundle_sha256 TEXT);
CREATE TABLE runtime_targets(provider_id TEXT,target_id TEXT,checkpoint_json TEXT,PRIMARY KEY(provider_id,target_id));
CREATE TABLE game_variant_runtime_packs(game_variant_id TEXT,definition_id TEXT,installation_id TEXT);
CREATE TABLE save_states(id TEXT,game_id TEXT,checkpoint_format TEXT,deleted_at_ms INTEGER,payload_sha256 TEXT,payload_size_bytes INTEGER,source_launch_session_id TEXT);
CREATE TABLE launch_sessions(id TEXT,game_id TEXT,provider_id TEXT,target_id TEXT,bundle_sha256 TEXT);
CREATE TABLE rpgmaker_game_profiles(game_id TEXT,generation TEXT);
CREATE TABLE review_drafts(id TEXT,import_item_id TEXT,version INTEGER,selected_validation_id TEXT,effective_source_snapshot_id TEXT);
CREATE TABLE review_draft_runtime_pack_selections(review_draft_id TEXT,slot INTEGER,installation_id TEXT);
CREATE TABLE import_item_core_validations(id TEXT,import_item_id TEXT,source_snapshot_id TEXT,status TEXT,dependency_snapshot_json TEXT);
CREATE TABLE upload_files(upload_session_id TEXT,state TEXT,final_blob_id TEXT,payload_released_at_ms INTEGER);
CREATE TABLE blobs(id TEXT,sha256 TEXT);
CREATE TABLE blob_gc_candidates(blob_id TEXT,first_unreferenced_at_ms INTEGER,scheduled_at_ms INTEGER,deleted_at_ms INTEGER);
CREATE TABLE audit_events(actor_label TEXT,action TEXT,resource_type TEXT,resource_id TEXT);
"""


def insert_job(database: sqlite3.Connection, value: dict, scope_type: str, scope_id: str, payload: dict | None) -> None:
    database.execute(
        "INSERT INTO jobs VALUES(?,?,?,?,?,1,1,20)",
        (value["jobId"], value["kind"], scope_type, scope_id, "SUCCEEDED"),
    )
    events = ("SUCCEEDED",) if value["kind"] == "UPLOAD_FINALIZE" else ("QUEUED", "STARTED", "SUCCEEDED")
    for event in events:
        database.execute("INSERT INTO job_events(job_id,event_type) VALUES(?,?)", (value["jobId"], event))
    if payload is not None:
        encoded = json.dumps(payload, separators=(",", ":"))
        database.execute(
            "INSERT INTO job_input_snapshots VALUES(?,1,?,?)",
            (value["jobId"], encoded, hashlib.sha256(encoded.encode()).hexdigest()),
        )


def seed_database(database: sqlite3.Connection, observed: dict) -> None:
    database.executescript(SCHEMA)
    for role, upload in observed["uploads"].items():
        catalog = observed["installations"][role]
        zero = role == "zeroReference"
        database.execute(
            "INSERT INTO upload_sessions VALUES(?,?,?,?,1,10,?)",
            (upload["uploadId"], "RUNTIME_ASSET_PACK", "COMPLETE", "FILES", upload["finalizeJob"]["jobId"]),
        )
        consumption_id = pack_uuid(80 + sorted(observed["uploads"]).index(role) + 1)
        database.execute(
            "INSERT INTO upload_consumptions VALUES(?,?,?,?,?,?)",
            (consumption_id, upload["uploadId"], "RUNTIME_ASSET_PACK_INSTALLATION", upload["installationId"],
             10 if zero else None, "UPLOAD_CONSUMED" if zero else None),
        )
        database.execute(
            "INSERT INTO runtime_asset_pack_installations VALUES(?,?,?,?,1,10,?,?)",
            (upload["installationId"], "DELETED" if zero else "READY", catalog["definitionId"],
             catalog["filesDigest"], None if zero else catalog["bundleSha256"], 9 if zero else None),
        )
        if not zero:
            database.execute("INSERT INTO runtime_asset_pack_files VALUES(?)", (upload["installationId"],))
        database.execute(
            "INSERT INTO upload_files VALUES(?,?,?,?)",
            (upload["uploadId"], "PURGED" if zero else "COMPLETE", None if zero else "blob", 10 if zero else None),
        )
        insert_job(database, upload["finalizeJob"], "UPLOAD_SESSION", upload["uploadId"], None)
        insert_job(
            database, upload["validationJob"], "RUNTIME_ASSET_PACK_INSTALLATION", upload["installationId"],
            {"installationId": upload["installationId"], "schemaVersion": 1},
        )
    seed_protected_references(database, observed)
    seed_review_relations(database, observed)
    seed_zero_release(database, observed)
    database.commit()


def seed_protected_references(database: sqlite3.Connection, observed: dict) -> None:
    protected = observed["protectedReferences"]
    for index, (role, definition) in enumerate((
        ("publishedVariant", "rgss1_standard"), ("restorableCheckpoint", "rgss2_rpgvx"),
    )):
        reference = protected[role]
        variant_id = pack_uuid(210 + index)
        target_id = "rpgmaker-xp" if role == "publishedVariant" else "rpgmaker-vx"
        bundle_sha = "7" * 64
        database.execute(
            "INSERT OR IGNORE INTO runtime_providers VALUES('retrom-runtime',?)", (bundle_sha,),
        )
        database.execute(
            "INSERT INTO runtime_targets VALUES('retrom-runtime',?,?)",
            (target_id, json.dumps({"writeFormat": "provider-checkpoint-v1", "readFormats": ["provider-checkpoint-v1"], "maxBytes": 1024})),
        )
        database.execute(
            "INSERT INTO runtime_asset_pack_installations VALUES(?,?,?,?,1,10,?,NULL)",
            (reference["installationId"], "READY", definition, f"{index + 90:064x}", f"{index + 92:064x}"),
        )
        database.execute("INSERT INTO games VALUES(?,'PUBLISHED',?)", (reference["gameId"], pack_uuid(230 + index)))
        database.execute(
            "INSERT INTO game_variants VALUES(?,?,'READY','retrom-runtime',?)",
            (variant_id, reference["gameId"], target_id),
        )
        database.execute(
            "INSERT INTO game_variant_runtime_packs VALUES(?,?,?)",
            (variant_id, definition, reference["installationId"]),
        )
        if role == "restorableCheckpoint":
            launch_id = pack_uuid(240)
            database.execute(
                "INSERT INTO launch_sessions VALUES(?,?,'retrom-runtime',?,?)",
                (launch_id, reference["gameId"], target_id, bundle_sha),
            )
            database.execute(
                "INSERT INTO save_states VALUES(?,?,?,NULL,?,?,?)",
                (reference["saveStateId"], reference["gameId"], "provider-checkpoint-v1",
                 "a" * 64, 64, launch_id),
            )


def seed_review_relations(database: sqlite3.Connection, observed: dict) -> None:
    for index, item in enumerate(observed["reviews"]["published"]):
        variant_id = pack_uuid(260 + index)
        target_id = "rpgmaker-xp"
        database.execute(
            "INSERT OR IGNORE INTO runtime_providers VALUES('retrom-runtime',?)", ("7" * 64,),
        )
        database.execute(
            "INSERT OR IGNORE INTO runtime_targets VALUES('retrom-runtime',?,?)", (target_id, "null"),
        )
        database.execute("INSERT INTO games VALUES(?,'PUBLISHED',?)", (item["gameId"], item["itemId"]))
        database.execute(
            "INSERT INTO game_variants VALUES(?,?,'READY','retrom-runtime',?)",
            (variant_id, item["gameId"], target_id),
        )
        database.execute("INSERT INTO rpgmaker_game_profiles VALUES(?,?)", (item["gameId"], item["generation"]))
    selected = [
        item for item in observed["reviews"]["matcherRejections"]
        if item.get("matcher") in {"SELECTED", "AMBIGUOUS"}
    ]
    for index, item in enumerate(selected):
        draft_id = pack_uuid(300 + index)
        validation_id, snapshot_id = pack_uuid(310 + index), pack_uuid(330 + index)
        database.execute("INSERT INTO review_drafts VALUES(?,?,2,?,?)",
                         (draft_id, item["itemId"], validation_id, snapshot_id))
        database.execute(
            "INSERT INTO review_draft_runtime_pack_selections VALUES(?,1,?)",
            (draft_id, item["installationId"]),
        )
        database.execute(
            "INSERT INTO import_item_core_validations VALUES(?,?,?,'READY',?)",
            (validation_id, item["itemId"], snapshot_id, json.dumps({
                "schemaVersion": 1, "bindings": [{"slot": 1, "installationId": item["installationId"]}],
            })),
        )


def seed_zero_release(database: sqlite3.Connection, observed: dict) -> None:
    zero = observed["uploads"]["zeroReference"]
    zero_catalog = observed["installations"]["zeroReference"]
    consumption_id = pack_uuid(80 + sorted(observed["uploads"]).index("zeroReference") + 1)
    release = pack_job(190, "PAYLOAD_RELEASE", True)
    release_input = {
        "schemaVersion": 1, "kind": "PAYLOAD_RELEASE",
        "scope": {"type": "UPLOAD_CONSUMPTION", "id": consumption_id},
        "executionId": pack_uuid(191), "inputs": {"scopeVersion": 1, "reason": "UPLOAD_CONSUMED"},
    }
    insert_job(database, release, "UPLOAD_CONSUMPTION", consumption_id, release_input)
    database.execute("INSERT INTO blobs VALUES('zero-bundle',?)", (zero_catalog["bundleSha256"],))
    database.execute("INSERT INTO blob_gc_candidates VALUES('zero-bundle',10,20,NULL)")
    database.execute(
        "INSERT INTO audit_events VALUES('payload-release-worker','PAYLOAD_RELEASE_COMPLETED','UPLOAD_CONSUMPTION',?)",
        (consumption_id,),
    )


class RPGMakerPackInspectTests(unittest.TestCase):
    def test_population_evidence_rejects_missing_changed_or_duplicate_original_rows(self) -> None:
        population = {"games": [{"id": pack_uuid(500), "sha256": "a" * 64}], "saves": [], "reviews": []}
        inspector.validate_population_preservation({"before": population, "after": population})
        for value in (None, {}, {"before": population, "after": {}},
                      {"before": {"games": []}, "after": {"games": []}}):
            with self.subTest(value=value), self.assertRaises(inspector.InspectError):
                inspector.validate_population_preservation(value)
        population["games"].append(population["games"][0])
        with self.assertRaises(inspector.InspectError):
            inspector.validate_population_preservation({"before": population, "after": population})

    def test_inspector_uses_provider_target_identity(self) -> None:
        source = MODULE_PATH.read_text()
        self.assertNotIn("core_artifacts", source)
        self.assertNotIn("core_artifact_id", source)
        self.assertIn("provider.bundle_sha256", source)
        self.assertIn('"providerId"', source)
        self.assertIn('"targetId"', source)

    def test_inspector_proves_database_relations_without_writing(self) -> None:
        observed = pack_evidence_payload()
        observed["status"] = "OBSERVED"
        plan = {
            "schemaVersion": 2, "uploads": {role: {} for role in observed["uploads"]},
            "reviewIds": {}, "protectedReferences": observed["protectedReferences"],
        }
        with tempfile.TemporaryDirectory() as directory:
            database_path = Path(directory) / "retrom.db"
            database = sqlite3.connect(database_path)
            seed_database(database, observed)
            database.commit()
            database.close()
            evidence_path = Path(directory) / "provision.json"
            evidence = {
                "schemaVersion": 1, "caseId": "ACC-RPG-009", "status": "PROVISIONED",
                "generatorInputIdentity": {
                    "schemaVersion": 1, "license": "MIT",
                    "licenseSource": "testdata/public-roms/rpgmaker-smoke/LICENSE",
                    "copyright": "Copyright (c) 2026 Retrom contributors",
                    "inputs": plan["uploads"], "reviewProjects": {},
                    "protectedPackInputs": {"publishedVariant": {}, "restorableCheckpoint": {}},
                    "protectedProjects": {"publishedVariant": {}, "restorableCheckpoint": {}},
                },
                "planIdentity": plan,
                "populationPreservation": {
                    "before": {"games": [], "saves": [], "reviews": []},
                    "after": {"games": [], "saves": [], "reviews": []},
                },
                "counts": {
                    "protectedInstallationCount": 2, "protectedGameCount": 2,
                    "reviewItemCount": 13, "selfContainedOrNoRtpReviewCount": 5,
                },
                "repository": {
                    "gitCommit": "1" * 40, "gitDirty": False,
                    "gitDirtySummary": {
                        "fileCount": 0,
                        "sha256": hashlib.sha256(b"[]").hexdigest(), "entries": [],
                    },
                },
            }
            evidence_path.write_text(json.dumps(evidence), encoding="utf-8")
            with mock.patch.dict(os.environ, {"RETROM_ACC_RPG_009_PROVISION_EVIDENCE": str(evidence_path)}):
                result = inspector.inspect(database_path, plan, observed, timeout=0.1)
            self.assertEqual("UPLOAD_CONSUMED", result["zeroReferenceRelease"]["releaseReason"])
            self.assertEqual(5, len(result["publishedReviews"]))
            self.assertEqual(8, len(result["selectedReviews"]))
            self.assertEqual("rgss2_rpgvx", result["protectedReferences"]["restorableCheckpoint"]["definitionId"])
            self.assertEqual(evidence, result["provisioningEvidence"]["payload"])

    def test_selected_review_requires_current_source_and_ready_dependency(self) -> None:
        observed = pack_evidence_payload()
        with sqlite3.connect(":memory:") as database:
            database.row_factory = sqlite3.Row
            seed_database(database, observed)
            self.assertEqual(8, len(inspector.selected_reviews(database, observed)))
            for column, value in (("source_snapshot_id", "wrong-source"), ("status", "BLOCKED"),
                                  ("dependency_snapshot_json", '{"schemaVersion":1,"bindings":[]}')):
                with self.subTest(column=column):
                    database.execute("SAVEPOINT mutation")
                    database.execute(f"UPDATE import_item_core_validations SET {column}=?", (value,))
                    with self.assertRaisesRegex(inspector.InspectError, "REVIEW_SELECTION_INVALID"):
                        inspector.selected_reviews(database, observed)
                    database.execute("ROLLBACK TO mutation")
                    database.execute("RELEASE mutation")

    def test_inspector_rejects_checkpoint_source_from_another_game(self) -> None:
        observed = pack_evidence_payload()
        observed["status"] = "OBSERVED"
        plan = {
            "schemaVersion": 2, "uploads": {role: {} for role in observed["uploads"]},
            "protectedReferences": observed["protectedReferences"],
        }
        with tempfile.TemporaryDirectory() as directory:
            database_path = Path(directory) / "retrom.db"
            database = sqlite3.connect(database_path)
            seed_database(database, observed)
            database.execute("UPDATE launch_sessions SET game_id='another-game'")
            database.commit()
            database.close()
            with self.assertRaisesRegex(inspector.InspectError, "CHECKPOINT_REFERENCE_INVALID"):
                inspector.inspect(database_path, plan, observed, timeout=0.1)


if __name__ == "__main__":
    unittest.main()
