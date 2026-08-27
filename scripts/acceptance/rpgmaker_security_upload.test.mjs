import assert from "node:assert/strict";
import test from "node:test";

import {
  requireFreshImportReview,
  SecurityInputBlocked,
} from "./rpgmaker_security_upload.mjs";

test("duplicate project import requires a fresh acceptance database", () => {
  assert.throws(
    () => requireFreshImportReview({ items: [] }, { alreadyImportedItemCount: 1 }),
    (error) => error instanceof SecurityInputBlocked &&
      error.message === "RPG_ACCEPTANCE_SECURITY_FRESH_DATABASE_REQUIRED",
  );
});

test("unexpected review cardinality remains a product failure", () => {
  assert.doesNotThrow(
    () => requireFreshImportReview({ items: [{ itemId: "one" }, { itemId: "two" }] }, {
      alreadyImportedItemCount: 0,
    }),
  );
});
