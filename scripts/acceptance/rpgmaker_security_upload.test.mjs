import assert from "node:assert/strict";
import test from "node:test";

import {
  createProductClient,
  jobWaitAttemptsForBytes,
  reviewForImport,
  requireFreshImportReview,
  SecurityInputBlocked,
} from "./rpgmaker_security_upload.mjs";

test("review lookup tolerates a briefly empty import queue", async () => {
  let queueReads = 0;
  const client = {
    json: async (_method, path) => {
      if (path.startsWith("/api/v1/admin/reviews?")) {
        queueReads += 1;
        return queueReads === 1 ? { items: [] } : { items: [{ itemId: "review-one" }] };
      }
      if (path === "/api/v1/admin/imports/import-one") {
        return { state: "RUNNING", alreadyImportedItemCount: 0 };
      }
      if (path === "/api/v1/admin/reviews/review-one") {
        return { itemId: "review-one" };
      }
      throw new Error(`unexpected path: ${path}`);
    },
  };

  assert.deepEqual(
    await reviewForImport(client, "import-one", { attempts: 2, waitMs: 0 }),
    { itemId: "review-one" },
  );
  assert.equal(queueReads, 2);
});

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

test("large uploads receive a bounded extended finalization window", () => {
  assert.equal(jobWaitAttemptsForBytes(1_073_741_824), 600);
  assert.equal(jobWaitAttemptsForBytes(1_073_741_825), 6_000);
});
