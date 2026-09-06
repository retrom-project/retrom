"""Validate evidence for explicit continuation of a populated RTP provision."""

import re


READY = {"rpg2000SelfContained": "RPG2000", "rpg2003SelfContained": "RPG2003",
         "rpgxpNoRtp": "RPGXP", "rpgvxNoRtp": "RPGVX", "rpgvxaceNoRtp": "RPGVXACE"}
UUID = re.compile(r"^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$")
SHA = re.compile(r"^[0-9a-f]{64}$")


def validate_partial_resume(resume, inputs, refs, reviews, baseline, validate_previous, validate_population, error):
    def require(condition):
        if not condition:
            raise error("RPG_ACCEPTANCE_PACK_RESUME_EVIDENCE_INVALID")

    require(set(resume) == {"schemaVersion", "mode", "capturedAtMs", "previous", "installations", "protectedReferences",
                            "reviewIds", "protectedPopulation", "sourceIdentities", "approvedReviewEventId", "trials"})
    require(resume["mode"] == "EXPLICIT_PARTIAL_PROVISION" and type(resume["capturedAtMs"]) is int and
            resume["capturedAtMs"] > 0 and UUID.fullmatch(str(resume["approvedReviewEventId"])))
    validate_previous(resume["previous"], inputs, refs, reviews, baseline)
    require(resume["previous"]["capturedAtMs"] < resume["capturedAtMs"] and
            resume["installations"] == resume["previous"]["installations"] and
            resume["protectedReferences"] == refs and resume["reviewIds"] == reviews and len(reviews) == 13)
    require(resume["sourceIdentities"] == {
        key: {role: row["sourceSha256"] for role, row in inputs[key].items()}
        for key in ("protectedProjects", "reviewProjects")})
    population = resume["protectedPopulation"]
    validate_population({"before": population, "after": population})
    expected = {"games": sorted(row["gameId"] for row in refs.values()),
                "saves": [refs["restorableCheckpoint"]["saveStateId"]], "reviews": sorted(reviews.values())}
    for kind, added in expected.items():
        original = {row["id"] for row in baseline[kind]}
        require([row for row in population[kind] if row["id"] in original] == baseline[kind] and
                sorted(row["id"] for row in population[kind] if row["id"] not in original) == added)
    trials = resume["trials"]
    require(isinstance(trials, dict) and set(trials) == {"protectedReferences", "reviews"} and
            isinstance(trials["reviews"], dict) and set(trials["reviews"]) == set(READY) and
            isinstance(trials["protectedReferences"], dict) and set(trials["protectedReferences"]) == set(refs))
    sessions = []
    for role, generation in READY.items():
        trial = trials["reviews"][role]
        require(isinstance(trial, dict) and set(trial) == {
            "itemId", "generation", "startedAtMs", "finishedAtMs", "previewId", "restoredPreviewId",
            "originalFrames", "restoredFrames", "checkpoint", "positions"})
        require(trial["itemId"] == reviews[role] and trial["generation"] == generation)
        timing(trial, resume["capturedAtMs"], require)
        frames(trial["originalFrames"], require)
        frames(trial["restoredFrames"], require)
        checkpoint(trial["checkpoint"], require)
        positions(trial["positions"], [0, 1, 2, 1, 2], require)
        require(trial["positions"][1] == trial["positions"][3])
        sessions.extend([trial["previewId"], trial["restoredPreviewId"]])
    for role, ref in refs.items():
        trial = trials["protectedReferences"][role]
        require(isinstance(trial, dict) and set(trial) == {"gameId", "fresh", "restore"} and trial["gameId"] == ref["gameId"])
        target = "rpgmaker-xp" if role == "publishedVariant" else "rpgmaker-vx"
        product_trial(trial["fresh"], target, None, resume["capturedAtMs"], sessions, require)
        if role == "publishedVariant":
            require(trial["restore"] is None)
        else:
            product_trial(trial["restore"], target, ref["saveStateId"], resume["capturedAtMs"], sessions, require)
            require(trial["restore"]["positions"][0] == trial["fresh"]["positions"][1])
    require(len(sessions) == len(set(sessions)) and all(UUID.fullmatch(str(value)) for value in sessions))


def product_trial(value, target, save, captured, sessions, require):
    require(isinstance(value, dict) and set(value) == {"launchId", "targetId", "saveStateId", "frames", "positions",
                                                      "startedAtMs", "finishedAtMs", "restore"})
    require(value["targetId"] == target and value["saveStateId"] == save)
    timing(value, captured, require)
    frames(value["frames"], require)
    positions(value["positions"], [1, 2] if save else [0, 1, 2], require)
    if save:
        checkpoint(value["restore"], require)
    else:
        require(value["restore"] is None)
    sessions.append(value["launchId"])


def timing(value, captured, require):
    require(type(value["startedAtMs"]) is int and type(value["finishedAtMs"]) is int and
            captured <= value["startedAtMs"] < value["finishedAtMs"] <= value["startedAtMs"] + 300_000)


def frames(value, require):
    require(isinstance(value, dict) and set(value) == {"beforeFrame", "afterFrame"} and
            type(value["beforeFrame"]) is int and type(value["afterFrame"]) is int and
            value["beforeFrame"] >= 0 and value["afterFrame"] - value["beforeFrame"] >= 300)


def checkpoint(value, require):
    require(isinstance(value, dict) and set(value) == {"sha256", "sizeBytes", "format"} and
            SHA.fullmatch(str(value["sha256"])) and type(value["sizeBytes"]) is int and
            0 < value["sizeBytes"] <= 268_435_456 and
            re.fullmatch(r"[a-z0-9][a-z0-9.-]{0,63}", str(value["format"])))


def positions(values, states, require):
    require(isinstance(values, list) and len(values) == len(states))
    for value, state in zip(values, states):
        require(isinstance(value, dict) and set(value) == {"mapId", "playerX", "playerY", "fixtureState"} and
                all(type(item) is int and item >= 0 for item in value.values()) and value["fixtureState"] == state)
