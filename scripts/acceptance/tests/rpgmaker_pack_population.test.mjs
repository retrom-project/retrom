import assert from "node:assert/strict";
import test from "node:test";
import {assertProvisionedState, capturePackBaseline} from "../rpgmaker_pack_provision_product.mjs";
import {assertPackCasePopulation, checkedPopulationPreservation} from "../rpgmaker_pack_population.mjs";

function fixture() {
  const rows = {
    games: [{gameId: "existing-game", version: 3}],
    saves: [{saveStateId: "existing-save", sizeBytes: 42}],
    reviews: [{itemId: "existing-review", reviewVersion: 2}],
    installations: [],
  };
  const client = {async json(method, path) {
    assert.equal(method, "GET");
    if (path.endsWith("runtime-asset-packs")) {return {installations: rows.installations};}
    const kind = ["games", "saves", "reviews"].find((key) => path.includes(`/${key}?`));
    assert.ok(kind, path);
    return {items: structuredClone(rows[kind]), nextCursor: null};
  }};
  const references = {publishedVariant: {installationId: "pack-xp", gameId: "new-xp"},
    restorableCheckpoint: {installationId: "pack-vx", gameId: "new-vx", saveStateId: "new-save"}};
  const reviews = Object.fromEntries(Array.from({length: 13}, (_, i) => [`role-${i}`, `new-review-${i}`]));
  function append() {
    rows.games.push({gameId: "new-xp", version: 1}, {gameId: "new-vx", version: 1});
    rows.saves.push({saveStateId: "new-save"});
    rows.reviews.push(...Object.values(reviews).map((itemId) => ({itemId, reviewVersion: 1})));
    rows.installations.push(...["pack-xp", "pack-vx"].map((installationId) =>
      ({installationId, status: "READY", references: {variantCount: 1, checkpointCount: 1}})));
  }
  return {rows, client, references, reviews, append};
}

test("resource-pack provision appends the exact matrix to an unchanged populated instance", async () => {
  const setup = fixture();
  const baseline = await capturePackBaseline(setup.client);
  setup.append();
  const preservation = await assertProvisionedState(setup.client, setup.references, setup.reviews, baseline);
  assert.deepEqual(preservation.before, preservation.after);
  assert.equal(preservation.before.games[0].id, "existing-game");
  assert.match(preservation.before.games[0].sha256, /^[0-9a-f]{64}$/);
});

test("changed or missing old data and unexpected new items fail preservation", async () => {
  for (const mutate of [
    (rows) => {rows.games[0].version++;},
    (rows) => {rows.saves.shift();},
    (rows) => {rows.reviews[0].reviewVersion++;},
    (rows) => {rows.games.push({gameId: "unexpected"});},
    (rows) => {rows.games[1].gameId = "same-count-wrong-id";},
  ]) {
    const setup = fixture();
    const baseline = await capturePackBaseline(setup.client);
    setup.append();
    mutate(setup.rows);
    await assert.rejects(assertProvisionedState(setup.client, setup.references, setup.reviews, baseline),
      /POPULATION_CHANGED|FINAL_CARDINALITY_INVALID/);
  }
});

test("existing packs still reject the ambiguous-installation fixture precondition", async () => {
  const setup = fixture();
  setup.rows.installations.push({installationId: "existing"});
  await assert.rejects(capturePackBaseline(setup.client), /PACK_CATALOG_NOT_EMPTY/);
});

test("pagination is complete and repeated cursors fail closed", async () => {
  const setup = fixture();
  const ordinary = setup.client.json;
  setup.client.json = async (method, path) => path.includes("/games?")
    ? {items: [{gameId: path.includes("cursor=") ? "second" : "first"}], nextCursor: path.includes("cursor=") ? null : "next"}
    : ordinary(method, path);
  assert.equal((await capturePackBaseline(setup.client)).games.length, 2);
  setup.client.json = async (method, path) => path.includes("/games?")
    ? {items: [], nextCursor: "repeating"} : ordinary(method, path);
  await assert.rejects(capturePackBaseline(setup.client), /POPULATION_PAGINATION_INVALID/);
});

test("duplicate IDs across pages are rejected", async () => {
  const setup = fixture();
  const ordinary = setup.client.json;
  setup.client.json = async (method, path) => path.includes("/games?")
    ? {items: [{gameId: "same"}], nextCursor: path.includes("cursor=") ? null : "next"} : ordinary(method, path);
  await assert.rejects(capturePackBaseline(setup.client), /POPULATION_INVALID/);
});

test("availability and dependency re-evaluation do not change stable business data hashes", async () => {
  const setup = fixture();
  const baseline = await capturePackBaseline(setup.client);
  setup.rows.saves[0].availability = {status: "AVAILABLE"};
  setup.rows.reviews[0].validationStatus = "READY";
  setup.rows.reviews[0].updatedAtMs = 100;
  setup.rows.games[0].runtimeStatus = "AVAILABLE";
  assert.deepEqual(await capturePackBaseline(setup.client), baseline);
});

test("formal Case retains original population through five publications and rejects evidence tampering", async () => {
  const setup = fixture();
  const id = (index) => `00000000-0000-4000-8000-${String(index).padStart(12, "0")}`;
  setup.rows.games[0].gameId = id(1);
  setup.rows.saves[0].saveStateId = id(2);
  setup.rows.reviews[0].itemId = id(3);
  const baseline = await capturePackBaseline(setup.client);
  const proof = {before: baseline, after: structuredClone(baseline)};
  setup.append();
  const published = Object.values(setup.reviews).slice(0, 5).map((itemId, index) => ({itemId, gameId: `published-${index}`}));
  setup.rows.games.push(...published.map(({gameId}) => ({gameId})));
  setup.rows.reviews = setup.rows.reviews.filter((row) => !published.some((item) => item.itemId === row.itemId));
  assert.deepEqual(await assertPackCasePopulation(setup.client, proof, setup.references, setup.reviews, published), proof);
  setup.rows.saves[0].sizeBytes++;
  await assert.rejects(assertPackCasePopulation(setup.client, proof, setup.references, setup.reviews, published), /POPULATION_CHANGED/);
  assert.throws(() => checkedPopulationPreservation(undefined), /PRESERVATION_INVALID/);
  proof.after.games[0].sha256 = "b".repeat(64);
  assert.throws(() => checkedPopulationPreservation(proof), /PRESERVATION_INVALID/);
});
