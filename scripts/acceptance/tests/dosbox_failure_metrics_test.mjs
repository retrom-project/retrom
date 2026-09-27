import test from "node:test";
import assert from "node:assert/strict";
import {validateDOSForcedCleanup} from "../dosbox_failure_metrics.mjs";

test("Forced cleanup needs actual Worker closure and complete surviving client accounting", () => {
  const cleanup = {mode: "FORCED_WORKER_TERMINATION", worker: {workerClosed: true, workerSha256: "a".repeat(64)},
    clientResources: {active: 0, queued: 0, pending: 0, waiters: 0, lruBytes: 0, channels: 0, files: 0, managementPending: 0, jobs: 0, syncChannels: 0}};
  validateDOSForcedCleanup(cleanup);
  assert.throws(() => validateDOSForcedCleanup({...cleanup, worker: {...cleanup.worker, workerClosed: false}}), /CLOSE_NOT_OBSERVED/u);
  assert.throws(() => validateDOSForcedCleanup({...cleanup, clientResources: {...cleanup.clientResources, pending: 1}}), /RESOURCE_LEAK/u);
  assert.throws(() => validateDOSForcedCleanup({...cleanup, clientResources: {}}));
});
