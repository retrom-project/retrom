import assert from "node:assert/strict";
import test from "node:test";

import {
  browserNavigationStatus,
  runtimeBootstrapReplayStatus,
  runtimeProjectStatus,
  runtimeRequestStatus,
  requireLocalRuntimeSite,
  runtimeFrameEligible,
  runtimeFrameRoute,
} from "./rpgmaker_security_runtime.mjs";
import { SecurityInputBlocked } from "./rpgmaker_security_upload.mjs";

const runtimeOrigin = "http://01980000-0000-7000-8000-000000000001.rpg.localhost:18084";

test("runtime bootstrap and entry stay on the isolated Go origin", () => {
  assert.equal(runtimeFrameRoute(`${runtimeOrigin}/__retrom/bootstrap`, runtimeOrigin), "RUNTIME");
  assert.equal(runtimeFrameRoute(`${runtimeOrigin}/__retrom/entry`, runtimeOrigin), "RUNTIME");
  assert.equal(runtimeFrameRoute("about:blank", runtimeOrigin), "WAIT");
  assert.equal(runtimeFrameRoute("http://localhost:13004/login", runtimeOrigin), "WAIT");
});

test("embedded EasyRPG and mkxp frames do not require a native unique origin", () => {
  assert.equal(runtimeFrameEligible("about:blank", undefined), true);
  assert.equal(runtimeFrameEligible("http://localhost:13004/runtime/embedded", undefined), true);
});

test("an application login rendered on the runtime origin blocks the acceptance input", () => {
  assert.throws(
    () => runtimeFrameRoute(`${runtimeOrigin}/login?returnTo=%2F__retrom%2Fbootstrap`, runtimeOrigin),
    (error) => error instanceof SecurityInputBlocked &&
      error.message === "RPG_ACCEPTANCE_SECURITY_RUNTIME_ORIGIN_MISROUTED",
  );
});

test("local native runtime cookies require the shared rpg.localhost site", () => {
  assert.doesNotThrow(() => requireLocalRuntimeSite("http://retrom-app.rpg.localhost:13004", runtimeOrigin));
  for (const applicationOrigin of ["http://localhost:13004", "http://127.0.0.1:13004"]) {
    assert.throws(
      () => requireLocalRuntimeSite(applicationOrigin, runtimeOrigin),
      (error) => error instanceof SecurityInputBlocked &&
        error.message === "RPG_ACCEPTANCE_SECURITY_RUNTIME_SITE_MISMATCH",
    );
  }
});

test("runtime content probes execute inside the authenticated browser frame", async () => {
  const calls = [];
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async (path, init) => {
    calls.push({ path, init });
    return { status: path.endsWith("Game.exe") ? 404 : 204 };
  };
  const frame = {
    evaluate: async (callback, input) => callback(input),
  };
  try {
    assert.equal(await runtimeProjectStatus(frame, "Retrom Nested/Game.exe"), 404);
    assert.equal(await runtimeRequestStatus(frame, "/__retrom/cleanup", "POST"), 204);
  } finally {
    globalThis.fetch = originalFetch;
  }
  assert.deepEqual(calls, [
    {
      path: "/__retrom/project/Retrom%20Nested/Game.exe",
      init: { method: "GET", credentials: "same-origin", redirect: "manual" },
    },
    {
      path: "/__retrom/cleanup",
      init: { method: "POST", credentials: "same-origin", redirect: "manual" },
    },
  ]);
});

test("runtime frame requests reject paths outside the isolated protocol", async () => {
  const frame = { evaluate: async () => 200 };
  await assert.rejects(
    runtimeRequestStatus(frame, "https://example.invalid/api", "GET"),
    /RPG_ACCEPTANCE_SECURITY_RUNTIME_PATH_INVALID/,
  );
  await assert.rejects(
    runtimeRequestStatus(frame, "/__retrom/project/Game.exe", "DELETE"),
    /RPG_ACCEPTANCE_SECURITY_RUNTIME_METHOD_INVALID/,
  );
});

test("runtime bootstrap diagnostics stay in Chromium", async () => {
  const navigation = [];
  let closed = false;
  const page = {
    on: (event, callback) => {
      assert.equal(event, "response");
      page.responseCallback = callback;
    },
    goto: async (url) => {
      navigation.push(url);
      page.responseCallback({ url: () => url, status: () => 303 });
    },
    close: async () => { closed = true; },
  };
  const context = { newPage: async () => page };
  assert.equal(await browserNavigationStatus(context, `${runtimeOrigin}/__retrom/bootstrap`), 303);
  assert.deepEqual(navigation, [`${runtimeOrigin}/__retrom/bootstrap`]);
  assert.equal(closed, true);
});

test("bootstrap ticket replay executes inside the runtime frame", async () => {
  const calls = [];
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async (path, init) => {
    calls.push({ path, init });
    return { status: 410 };
  };
  const frame = { evaluate: async (callback, input) => callback(input) };
  try {
    assert.equal(await runtimeBootstrapReplayStatus(frame, "ticket-value"), 410);
  } finally {
    globalThis.fetch = originalFetch;
  }
  assert.deepEqual(calls, [{
    path: "/__retrom/bootstrap",
    init: {
      method: "POST", credentials: "same-origin", redirect: "manual",
      headers: { "Content-Type": "application/json" }, body: JSON.stringify({ ticket: "ticket-value" }),
    },
  }]);
});
