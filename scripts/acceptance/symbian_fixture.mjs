import assert from "node:assert/strict";

// Only pass the game ID returned by this Case's fresh approval.
export async function cleanupSymbianFixture(client, gameId, source) {
  const path = `/api/v1/admin/games/${gameId}`;
  const response = await client.raw("GET", path);
  assert.equal(response.status(), 200, "SYMBIAN_FIXTURE_LOOKUP_FAILED");
  const game = await response.json(), etag = response.headers().etag;
  assert.ok(etag && game.platformId === "symbian" && game.variants.some(row => row.coreId === "eka2l1") &&
    game.files.some(file => file.role === "CONTENT" && file.sha256 === source.sha256 && file.sizeBytes === source.sizeBytes),
  "SYMBIAN_FIXTURE_IDENTITY_CHANGED");
  const removed = await client.raw("DELETE", path, {headers: {...client.writeHeaders(), "If-Match": etag},
    data: {confirmTitle: game.title, impactDigest: game.deleteImpact.impactDigest}});
  assert.equal(removed.status(), 202, "SYMBIAN_FIXTURE_DELETE_FAILED");
  // Admin detail retains the tombstone; the public game route must be gone.
  assert.equal((await client.raw("GET", `/api/v1/games/${gameId}`)).status(), 404, "SYMBIAN_FIXTURE_STILL_PUBLISHED");
  return true;
}
