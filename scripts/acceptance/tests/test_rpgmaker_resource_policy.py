import copy
import unittest

from scripts.acceptance.rpgmaker_case import ContractError, validate_resource_policy_evidence


def evidence():
    projects = []
    for index, generation in enumerate(("RPG2000", "RPG2003", "RPGXP", "RPGVX", "RPGVXACE")):
        projects.append({
            "generation": generation,
            "selfContainedGameId": f"01980000-0000-7000-8000-{index * 3 + 1:012d}",
            "externalItemId": f"01980000-0000-7000-8000-{index * 3 + 2:012d}",
            "confirmedGameId": f"01980000-0000-7000-8000-{index * 3 + 3:012d}",
            "rejectedStatus": 409, "rejectedCode": "REVIEW_VALIDATION_STALE",
            "confirmed": True, "clearedBlocked": True,
        })
    return {
        "schemaVersion": 1, "caseId": "ACC-RPG-009", "status": "PASS", "projects": projects,
        "retired": {"routes": [{"method": method, "status": 404} for method in ("GET", "POST", "DELETE")], "uploadStatus": 400},
        "screenshots": ["screenshots/rpgmaker-bios-only.png", "screenshots/rpgmaker-self-contained-confirmation.png"],
    }


class ResourcePolicyEvidenceTests(unittest.TestCase):
    def test_complete_policy_matrix_is_valid(self):
        validate_resource_policy_evidence(evidence())

    def test_missing_confirmation_or_clear_cannot_pass(self):
        for key in ("confirmed", "clearedBlocked"):
            sample = evidence()
            sample["projects"][0][key] = False
            with self.assertRaises(ContractError):
                validate_resource_policy_evidence(sample)

    def test_duplicate_identity_or_omitted_generation_cannot_pass(self):
        sample = evidence()
        duplicate = copy.deepcopy(sample)
        duplicate["projects"][1]["externalItemId"] = duplicate["projects"][0]["externalItemId"]
        sample["projects"].pop()
        for invalid in (duplicate, sample):
            with self.assertRaises(ContractError):
                validate_resource_policy_evidence(invalid)

    def test_live_installation_api_cannot_pass(self):
        sample = evidence()
        sample["retired"]["routes"][1]["status"] = 202
        with self.assertRaises(ContractError):
            validate_resource_policy_evidence(sample)
