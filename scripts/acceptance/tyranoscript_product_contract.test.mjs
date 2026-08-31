import assert from "node:assert/strict";
import test from "node:test";

import {
  assertTyranoScriptProductEvidence,
  tyranoScriptProductStages,
} from "./tyranoscript_product_contract.mjs";

const screenshot = {height: 720, nonBlackPixels: 10000, pngSha256: "a".repeat(64), width: 1280};
const state = (marker) => ({marker, order: 10, scenario: "first.ks"});

function evidence() {
  return {
    schemaVersion: 1, caseId: "ACC-TYRANOSCRIPT-001", status: "PASS",
    stages: [...tyranoScriptProductStages],
    ids: {
      importItemId: "01a05123-1234-7123-8123-123456789abc",
      gameId: "01a05123-1234-7123-8123-223456789abc",
      saveStateId: "01a05123-1234-7123-8123-323456789abc",
      originalLaunchId: "01a05123-1234-7123-8123-423456789abc",
      restoreLaunchId: "01a05123-1234-7123-8123-523456789abc",
    },
    checkpoint: {payloadKind: "RUNTIME_STATE", sizeBytes: 1024},
    state: {b: state("B"), c: state("C"), restoredB: state("B")},
    resources: {contentDigest: "b".repeat(64), engineAsset200Count: 1, failedResponseCount: 0},
    screenshots: {preview: screenshot, product: screenshot, restored: screenshot},
    browser: {pageErrorCount: 0, consoleErrorCount: 0, dialogCount: 0, ignoredSandboxAlertCount: 1},
  };
}

test("accepts the complete TyranoScript product chain", () => {
  assert.doesNotThrow(() => assertTyranoScriptProductEvidence(evidence()));
  assert.doesNotThrow(() => assertTyranoScriptProductEvidence({
    ...evidence(), checkpoint: {payloadKind: "RUNTIME_STATE", sizeBytes: 32 * 1024 * 1024},
  }));
});

test("rejects restore, resource, screenshot and browser regressions", () => {
  const invalid = [
    {...evidence(), state: {...evidence().state, restoredB: state("C")}},
    {...evidence(), resources: {...evidence().resources, engineAsset200Count: 0}},
    {...evidence(), screenshots: {...evidence().screenshots, restored: {...screenshot, nonBlackPixels: 0}}},
    {...evidence(), browser: {...evidence().browser, consoleErrorCount: 1}},
    {...evidence(), checkpoint: {payloadKind: "RUNTIME_STATE", sizeBytes: 32 * 1024 * 1024 + 1}},
  ];
  for (const value of invalid) {
    assert.throws(() => assertTyranoScriptProductEvidence(value), /TYRANOSCRIPT_ACCEPTANCE_/u);
  }
});
