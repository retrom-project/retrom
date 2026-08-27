import assert from "node:assert/strict";
import test from "node:test";

import {
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
