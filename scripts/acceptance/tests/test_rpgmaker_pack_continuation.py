import copy
import unittest

from scripts.acceptance.tests.test_rpgmaker_pack_inspect import inspector, pack_uuid


READY = {"rpg2000SelfContained": "RPG2000", "rpg2003SelfContained": "RPG2003",
         "rpgxpNoRtp": "RPGXP", "rpgvxNoRtp": "RPGVX", "rpgvxaceNoRtp": "RPGVXACE"}


def position(state):
    return {"mapId": 1, "playerX": state + 10, "playerY": 8, "fixtureState": state}


def fixture():
    roles = ["publishedVariant", "restorableCheckpoint"]
    refs = {role: {"installationId": pack_uuid(i + 1), "gameId": pack_uuid(i + 3)} for i, role in enumerate(roles)}
    refs[roles[1]]["saveStateId"] = pack_uuid(5)
    names = list(READY) + ["rpg2000Missing", "rpg2003Missing", "rpgxpStandardAmbiguous", "rpgxpCustom",
                           "rpgvxStandardAmbiguous", "rpgvxCustom", "rpgvxaceStandardAmbiguous", "rpgvxaceCustom"]
    reviews = {role: pack_uuid(i + 10) for i, role in enumerate(names)}
    inputs = {"protectedPackInputs": {role: {"sourceSha256": "a" * 64} for role in roles},
              "protectedProjects": {role: {"sourceSha256": "b" * 64} for role in roles},
              "reviewProjects": {role: {"sourceSha256": "c" * 64} for role in reviews}}
    baseline = {"games": [{"id": pack_uuid(30), "sha256": "d" * 64}], "saves": [], "reviews": []}
    previous = {"schemaVersion": 1, "mode": "EXPLICIT_PROTECTED_PREVIEW", "capturedAtMs": 1,
                "installations": {role: {"installationId": refs[role]["installationId"], "filesDigest": "e" * 64,
                                         "sourceSha256": "a" * 64} for role in roles},
                "review": {"itemId": pack_uuid(6), "version": 1, "sourceSha256": "b" * 64,
                           "populationRow": {"id": pack_uuid(6), "sha256": "f" * 64}}}
    population = {"games": [{"id": refs[role]["gameId"], "sha256": "d" * 64} for role in roles] + copy.deepcopy(baseline["games"]),
                  "saves": [{"id": pack_uuid(5), "sha256": "d" * 64}],
                  "reviews": sorted([{"id": value, "sha256": "d" * 64} for value in reviews.values()], key=lambda row: row["id"])}
    trials = {}
    for i, (role, generation) in enumerate(READY.items()):
        trials[role] = {"itemId": reviews[role], "generation": generation, "startedAtMs": 101, "finishedAtMs": 200,
                        "previewId": pack_uuid(40 + i * 2), "restoredPreviewId": pack_uuid(41 + i * 2),
                        "originalFrames": {"beforeFrame": 1, "afterFrame": 301},
                        "restoredFrames": {"beforeFrame": 2, "afterFrame": 302},
                        "checkpoint": {"sha256": "a" * 64, "sizeBytes": 1000, "format": "mkxp-state-compact-v1"},
                        "positions": [position(s) for s in [0, 1, 2, 1, 2]]}
    protected = {}
    for i, role in enumerate(roles):
        target = "rpgmaker-vx" if i else "rpgmaker-xp"
        fresh = {"launchId": pack_uuid(60 + i), "targetId": target, "saveStateId": None,
                 "frames": {"beforeFrame": 1, "afterFrame": 301}, "positions": [position(s) for s in [0, 1, 2]],
                 "startedAtMs": 101, "finishedAtMs": 200, "restore": None}
        restore = {**fresh, "launchId": pack_uuid(62), "saveStateId": pack_uuid(5),
                   "positions": [position(s) for s in [1, 2]],
                   "restore": {"sha256": "a" * 64, "sizeBytes": 1000, "format": "mkxp-state-compact-v1"}} if i else None
        protected[role] = {"gameId": refs[role]["gameId"], "fresh": fresh, "restore": restore}
    resume = {"schemaVersion": 2, "mode": "EXPLICIT_PARTIAL_PROVISION", "capturedAtMs": 100,
              "previous": previous, "installations": copy.deepcopy(previous["installations"]),
              "protectedReferences": refs, "reviewIds": reviews, "protectedPopulation": population,
              "sourceIdentities": {key: {role: row["sourceSha256"] for role, row in inputs[key].items()}
                                   for key in ["protectedProjects", "reviewProjects"]},
              "approvedReviewEventId": pack_uuid(7), "trials": {"protectedReferences": protected, "reviews": trials}}
    return resume, inputs, refs, reviews, baseline


class ContinuationEvidenceTests(unittest.TestCase):
    def test_complete_current_trials_link_to_both_preservation_boundaries(self):
        inspector.validate_resume_evidence(*fixture())

    def test_rejects_incomplete_replayed_or_disconnected_evidence(self):
        for change in [
            lambda r: r.pop("trials"),
            lambda r: r["trials"]["reviews"].pop("rpgxpNoRtp"),
            lambda r: r["trials"]["reviews"]["rpgxpNoRtp"].update(startedAtMs=99),
            lambda r: r["trials"]["reviews"]["rpgxpNoRtp"]["positions"].__setitem__(3, position(0)),
            lambda r: r["trials"]["protectedReferences"]["restorableCheckpoint"]["restore"].update(saveStateId=pack_uuid(9)),
            lambda r: r["protectedPopulation"]["games"][-1].update(sha256="f" * 64),
            lambda r: r["sourceIdentities"]["protectedProjects"].update(publishedVariant="f" * 64),
        ]:
            values = fixture()
            change(values[0])
            with self.assertRaisesRegex(inspector.InspectError, "RESUME_EVIDENCE_INVALID"):
                inspector.validate_resume_evidence(*values)
