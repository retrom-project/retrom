import assert from "node:assert/strict";
import test from "node:test";
import {cleanupSymbianFixture} from "./symbian_fixture.mjs";

const source = {sha256: "a".repeat(64), sizeBytes: 123};
function clientFor(game, deleteStatus = 202) {
  const calls = [];
  let removed = false;
  return {calls, client: {
    writeHeaders: () => ({"X-Retrom-Csrf": "test"}),
    async raw(method, path, options) {
      calls.push({method, path, options});
      if (method === "DELETE") {removed = deleteStatus === 202;}
      return {status: () => method === "DELETE" ? deleteStatus : removed && !path.includes("/admin/") ? 404 : 200,
        headers: () => ({etag: '"v3"'}), json: async () => game};
    },
  }};
}
const fixture = () => ({platformId: "symbian", title: "Owned SIS", variants: [{coreId: "eka2l1"}],
  files: [{role: "CONTENT", ...source}], deleteImpact: {impactDigest: "impact"}});

test("owned Symbian fixture is conditionally removed so the same SIS can be imported again", async () => {
  const {client, calls} = clientFor(fixture());
  assert.equal(await cleanupSymbianFixture(client, "owned-id", source), true);
  assert.deepEqual(calls.map(({method}) => method), ["GET", "DELETE", "GET"]);
  assert.deepEqual(calls[1].options, {headers: {"X-Retrom-Csrf": "test", "If-Match": '"v3"'},
    data: {confirmTitle: "Owned SIS", impactDigest: "impact"}});
});

for (const change of [game => {game.platformId = "gba";}, game => {game.files[0].sha256 = "b".repeat(64);},
  game => {game.variants[0].coreId = "mgba";}]) {
  test(`cleanup refuses a changed fixture identity: ${change.toString()}`, async () => {
    const game = fixture();change(game);
    const {client, calls} = clientFor(game);
    await assert.rejects(cleanupSymbianFixture(client, "owned-id", source));
    assert.equal(calls.some(call => call.method === "DELETE"), false);
  });
}

test("fixture cleanup does not report success when conditional deletion is rejected", async () => {
  const {client} = clientFor(fixture(), 412);
  await assert.rejects(cleanupSymbianFixture(client, "owned-id", source), /SYMBIAN_FIXTURE_DELETE_FAILED/u);
});
