import assert from "node:assert/strict";
import {test} from "node:test";
import {finalizeUpload, replayable} from "../idempotency_replay.mjs";

function response(status, body, headers = {}) {
  return {status: () => status, text: async () => body, headers: () => headers};
}

test("upload completion obeys the no-body contract and freezes the original ETag", async () => {
  let reads = 0, posts = 0;
  const client = {
    writeHeaders: () => ({"Idempotency-Key": "key"}),
    raw: async (method, path, options) => {
      if (method === "GET") {
        reads++; assert.equal(path, "/api/v1/admin/uploads/upload");
        return response(200, "", {etag: '"v2"'});
      }
      assert.equal(path, "/api/v1/admin/uploads/upload/complete");
      assert.equal(options.data, undefined, "the OpenAPI operation has no requestBody");
      assert.equal(options.headers["If-Match"], '"v2"');
      posts++;
      return response(202, '{"state":"FINALIZING","jobId":"job"}\n', {
        "content-type": "application/json; charset=utf-8",
        ...(posts > 1 ? {"x-retrom-idempotent-replay": "true"} : {}),
      });
    },
  };
  const report = {operations: []};
  const completed = await finalizeUpload(client, report, "upload");
  await completed.replay();
  assert.equal(completed.value.state, "FINALIZING");
  assert.equal(reads, 1); assert.equal(posts, 3); assert.equal(report.operations.length, 1);
});

test("replay verification rejects later resource state replacing the original bytes", async () => {
  let calls = 0;
  const client = {writeHeaders: () => ({}), raw: async () => {
    calls++;
    return response(202, calls === 1 ? '{"state":"QUEUED"}' : '{"state":"SUCCEEDED"}', {
      "x-retrom-idempotent-replay": "true",
    });
  }};
  await assert.rejects(() => replayable(client, {operations: []}, "POST", "/command", {}, 202),
    /IDEMPOTENCY_REPLAY_BYTES/u);
});
