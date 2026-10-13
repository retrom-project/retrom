import {expect, it} from "vitest";
import {runtimeFrameSources} from "./runtime-frame-sources";

it("permits the configured isolated runtime port used by the real product browser", () => {
  expect(runtimeFrameSources("http://{runId}.rpg.localhost:37139"))
    .toBe("http://*.localhost:3000 http://*.rpg.localhost:37139");
});

it("keeps the standard PFB source and supports a configured PFB hostname", () => {
  expect(runtimeFrameSources(undefined)).toBe("http://*.localhost:3000");
  expect(runtimeFrameSources("http://{runId}.feature.rpg.localhost:3000"))
    .toBe("http://*.localhost:3000 http://*.feature.rpg.localhost:3000");
});

it("never adds external, malformed or injected origins to the HTTP frame policy", () => {
  for (const template of ["https://{runId}.example.com", "http://{runId}.example.com:37139",
    "http://{runId}.rpg.localhost.evil.example:37139", "http://{runId}.rpg.localhost:65536",
    "http://{runId}.rpg.localhost:0", "http://{runId}.rpg.localhost:37139; script-src *",
    "http://{runId}.rpg.localhost:37139/path", "http://user@{runId}.rpg.localhost:37139"] ) {
    expect(runtimeFrameSources(template)).toBe("http://*.localhost:3000");
  }
});
