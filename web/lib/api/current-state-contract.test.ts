import {readFileSync} from "node:fs";
import {resolve} from "node:path";
import {describe, expect, it} from "vitest";

const schema = readFileSync(resolve(process.cwd(), "lib/api/generated/schema.d.ts"), "utf8");

describe("current-state HTTP contract", () => {
  it("publishes current content replacement without business revision identities", () => {
    expect(schema).toContain('"/api/v1/admin/games/{gameId}/content-replacement"');
    expect(schema).toContain("postAdminGameContentReplacement");

    for (const forbidden of [
      "content-revisions",
      "postAdminGameContentRevision",
      "metadataRevisionId",
      "contentRevisionId",
      "variantRevisionId",
      "runtimeBindingRevision",
      "targetContractSha256",
      "gameCompatibilityLine",
      "netplayCompatibilityLine",
    ]) {
      expect(schema).not.toContain(forbidden);
    }
  });
});
