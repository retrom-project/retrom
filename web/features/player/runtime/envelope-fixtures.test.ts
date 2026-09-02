import {readFileSync, readdirSync} from "node:fs";
import {resolve} from "node:path";
import {describe, expect, it} from "vitest";

import {parseLaunchEnvelopeJSON} from "./envelope";

const fixtureRoot = resolve(process.cwd(), "../api/runtime-provider/v1/fixtures");

describe("shared Launch Envelope V1 fixtures", () => {
  for (const name of readdirSync(resolve(fixtureRoot, "valid")).sort()) {
    it(`accepts ${name}`, () => {
      expect(() => parseLaunchEnvelopeJSON(readFileSync(resolve(fixtureRoot, "valid", name), "utf8")))
        .not.toThrow();
    });
  }

  for (const name of readdirSync(resolve(fixtureRoot, "invalid")).sort()) {
    it(`rejects ${name}`, () => {
      expect(() => parseLaunchEnvelopeJSON(readFileSync(resolve(fixtureRoot, "invalid", name), "utf8")))
        .toThrow("PLAYER_LAUNCH_ENVELOPE_INVALID");
    });
  }
});
