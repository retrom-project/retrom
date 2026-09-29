import {readFileSync} from "node:fs";
import {resolve} from "node:path";
import {expect, it} from "vitest";
import {parseLaunchEnvelopeJSON} from "./envelope";

it.each(["NATIVE_WEB", "ISOLATED_WEB"])("accepts %s only with a stable relative content index", kind => {
  const fixture = JSON.parse(readFileSync(resolve(process.cwd(), "../api/runtime-provider/v1/fixtures/valid/single-minimal.json"), "utf8"));
  fixture.resources = [{kind, role: "game", ordinal: 0, contentDigest: "a".repeat(64),
    origin: "https://launch.test", entryUrl: "https://launch.test/bootstrap", cleanupUrl: null,
    bootstrapTicket: "t".repeat(48), indexUrl: "/runtime/content/web/identity/index.json"}];
  expect(parseLaunchEnvelopeJSON(JSON.stringify(fixture)).resources[0]).toEqual(fixture.resources[0]);
  for (const indexUrl of [undefined, "https://other.test/index.json", "//other.test/index.json", ""]) {
    fixture.resources[0].indexUrl = indexUrl;
    expect(() => parseLaunchEnvelopeJSON(JSON.stringify(fixture))).toThrow("PLAYER_LAUNCH_ENVELOPE_INVALID");
  }
});
