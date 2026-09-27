import assert from "node:assert/strict";
import {test} from "node:test";
import {launchCart} from "../fantasy_product_client.mjs";

test("a DOS product launch carries the reviewed entry instead of selecting the program menu", async () => {
  let request;
  const client = {writeHeaders: () => ({}), json: async (method, path, options) => {
    request = {method, path, ...options}; return {launchId: "owned-launch"};
  }};
  await launchCart(client, "owned-game", null, "DOOM2/DOOM2.EXE");
  assert.equal(request.path, "/api/v1/launches"); assert.equal(request.data.dosEntry, "DOOM2/DOOM2.EXE");
  assert.equal(request.data.saveStateId, null);
});

test("restore leaves DOS entry selection to the saved checkpoint", async () => {
  let data;
  const client = {writeHeaders: () => ({}), json: async (_method, _path, options) => {data = options.data;}};
  await launchCart(client, "owned-game", "owned-save");
  assert.equal(data.saveStateId, "owned-save"); assert.equal(data.dosEntry, null);
});
