import assert from "node:assert/strict";
import test from "node:test";
import {requestValidatedLaunch} from "./emulatorjs-launch.mjs";

const response = (status, body) => ({status: () => status, ok: () => status < 400, json: async () => body});
const ready = {launchId: "launch", playUrl: "/play/launch"};

test("waits for a newly selected core validation before navigating", async () => {
  const responses = [response(202, {validationJobId: "pending"}), response(201, ready)];
  let waits = 0;
  assert.deepEqual(await requestValidatedLaunch(async () => responses.shift(), async () => {waits++;}), ready);
  assert.equal(waits, 1);
});

test("rejects an invalid successful response before arming browser waiters", async () => {
  await assert.rejects(requestValidatedLaunch(async () => response(201, {}), async () => {}), /SMOKE_LAUNCH_INVALID/);
});

test("does not retry a blocked launch", async () => {
  let sends = 0;
  await assert.rejects(requestValidatedLaunch(async () => {sends++; return response(409, {});}, async () => {}), /409/);
  assert.equal(sends, 1);
});

test("bounds a validation that never completes", async () => {
  let sends = 0;
  await assert.rejects(requestValidatedLaunch(async () => {sends++; return response(202, {});}, async () => {}), /SMOKE_VALIDATION_DEADLINE/);
  assert.equal(sends, 60);
});
