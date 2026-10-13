import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { expect, it } from "vitest";
import {
  parseLaunchEnvelopeJSON,
  validateLaunchEnvelopeBoundary,
} from "./envelope";

const fixtureRoot = resolve(
  process.cwd(),
  "../api/runtime-provider/v1/fixtures",
);
it("preserves distinct public core and target identifiers", () => {
  const source = readFileSync(
    resolve(fixtureRoot, "valid/core-identifier.json"),
    "utf8",
  );
  const envelope = parseLaunchEnvelopeJSON(source);
  expect(envelope.runtime.coreId).toBe("fbalpha2012_cps1");
  expect(envelope.runtime.targetId).toBe("fbalpha2012-cps1");
  expect(() =>
    parseLaunchEnvelopeJSON(
      readFileSync(
        resolve(fixtureRoot, "invalid/core-identifier.json"),
        "utf8",
      ),
    ),
  ).toThrow("PLAYER_LAUNCH_ENVELOPE_INVALID");
  expect(() =>
    validateLaunchEnvelopeBoundary({
      ...envelope,
      runtime: { ...envelope.runtime, providerId: "emulator_js" },
    }),
  ).toThrow("PLAYER_LAUNCH_ENVELOPE_INVALID");
});

it("requires explicit checkpoint semantics at the public boundary", () => {
  const envelope = parseLaunchEnvelopeJSON(
    readFileSync(resolve(fixtureRoot, "valid/checkpoint-restore.json"), "utf8"),
  );
  expect(envelope.runtime.checkpoint?.semantics).toBe("INSTANT");
  expect(() =>
    parseLaunchEnvelopeJSON(
      readFileSync(
        resolve(fixtureRoot, "invalid/checkpoint-missing-semantics.json"),
        "utf8",
      ),
    ),
  ).toThrow("PLAYER_LAUNCH_ENVELOPE_INVALID");
  const native = parseLaunchEnvelopeJSON(
    readFileSync(resolve(fixtureRoot, "valid/game-save-restore.json"), "utf8"),
  );
  expect(native.runtime.checkpoint?.semantics).toBe("GAME_SAVE");
});

it("accepts revision-scoped development assets and rejects another module revision", () => {
  const envelope = parseLaunchEnvelopeJSON(readFileSync(resolve(fixtureRoot, "valid/single-minimal.json"), "utf8"));
  const base = `${envelope.runtime.runtimeBaseUrl}dev/${envelope.runtime.moduleSha256}/`;
  envelope.runtime.runtimeBaseUrl = base;
  envelope.runtime.moduleUrl = `${base}client.mjs`;
  expect(validateLaunchEnvelopeBoundary(envelope)).toBe(envelope);
  envelope.runtime.runtimeBaseUrl = base.replace(envelope.runtime.moduleSha256, "f".repeat(64));
  envelope.runtime.moduleUrl = `${envelope.runtime.runtimeBaseUrl}client.mjs`;
  expect(() => validateLaunchEnvelopeBoundary(envelope)).toThrow("PLAYER_LAUNCH_ENVELOPE_INVALID");
});
