import assert from "node:assert/strict";
import test from "node:test";

import {
  createProductClient,
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

test("product client forwards a bounded timeout for large import requests", async () => {
  let observed;
  const context = {
    request: {
      fetch: async (_url, options) => {
        observed = options;
        return { status: () => 202 };
      },
    },
  };
  const client = createProductClient(context, "https://retrom.example", "csrf");

  await client.raw("POST", "/api/v1/admin/imports", {
    data: { uploadId: "upload" }, headers: client.writeHeaders(), timeout: 120_000,
  });

  assert.equal(observed.timeout, 120_000);
});
