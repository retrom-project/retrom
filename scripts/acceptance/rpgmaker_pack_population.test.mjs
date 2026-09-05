import assert from "node:assert/strict";
import {test} from "node:test";
import {readPopulation} from "./rpgmaker_pack_population.mjs";

test("pack preservation paginates Review with its current 20-row queue contract", async () => {
  const calls = [];
  const client = {json: async (_method, target) => {
    const url = new URL(target, "http://example.test");
    calls.push(url);
    if (url.pathname !== "/api/v1/admin/reviews") {return {items: [], nextCursor: null};}
    assert.equal(url.searchParams.get("limit"), "20", "the review API rejects limit=100");
    if (url.searchParams.has("cursor")) {
      assert.equal(url.searchParams.get("cursor"), "page/2");
      return {items: [{itemId: "21", reviewVersion: 1}], nextCursor: null};
    }
    return {items: Array.from({length: 20}, (_, index) => ({itemId: String(index+1), reviewVersion: 1})), nextCursor: "page/2"};
  }};
  const population = await readPopulation(client);
  assert.equal(population.reviews.length, 21);
  assert.equal(calls.filter(url => url.pathname.endsWith("/reviews")).length, 2);
  assert.equal(calls.find(url => url.pathname.endsWith("/saves")).searchParams.get("availability"), "ALL");
});

test("pack preservation rejects repeated cursors instead of accepting a partial population", async () => {
  const client = {json: async () => ({items: [], nextCursor: "repeat"})};
  await assert.rejects(readPopulation(client), /POPULATION_PAGINATION_INVALID/u);
});
