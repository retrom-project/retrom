import assert from "node:assert/strict";
import test from "node:test";
import {mkdtemp, readFile, rm, writeFile} from "node:fs/promises";
import {join} from "node:path";
import {importRuffleMovie} from "./ruffle_product_client.mjs";
import {ruffleMovieTags} from "./ruffle_run_movie.mjs";

test("repeated Flash product imports preserve movie actions and get distinct content identities", async () => {
  const directory = await mkdtemp(".cache/ruffle-import-test-");
  const path = join(directory, "owned.swf");
  // Project-owned parser bytes: RECT, frame rate/count, opaque action and End.
  const body = Buffer.from([8, 0, 0, 30, 1, 0, 0x03, 0x03, 1, 2, 3, 0, 0]);
  const header = Buffer.alloc(8); header.write("FWS"); header[3] = 9; header.writeUInt32LE(body.length + 8, 4);
  const original = Buffer.concat([header, body]), uploads = [];
  const client = {
    writeHeaders: () => ({}),
    upload: async files => {uploads.push(await readFile(files[0].path)); return "upload";},
    json: async (_method, url) => {
      if (url.includes("platform-instances?")) return {items: [{id: "platform", enabled: true, defaultCoreId: "ruffle"}]};
      if (url.endsWith("/imports")) return {importJobId: "import"};
      if (url.includes("reviews?")) return {items: [{itemId: "review"}]};
      return {};
    },
  };
  try {
    await writeFile(path, original);
    await importRuffleMovie(client, path); await importRuffleMovie(client, path);
    assert.notDeepEqual(uploads[0], uploads[1], "each product run must get a fresh import review");
    for (const bytes of uploads) {
      assert.deepEqual(ruffleMovieTags(bytes).tags.find(tag => tag.code === 12).bytes,
        ruffleMovieTags(original).tags.find(tag => tag.code === 12).bytes);
    }
    assert.deepEqual(await readFile(path), original);
  } finally {await rm(directory, {recursive: true});}
});
