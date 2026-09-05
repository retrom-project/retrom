import {createHash} from "node:crypto";

const collections = {
  games: ["/api/v1/admin/games", "gameId"],
  saves: ["/api/v1/saves", "saveStateId"],
  reviews: ["/api/v1/admin/reviews", "itemId"],
};
const stableFields = {
  games: ["gameId", "title", "platform", "platformInstance", "defaultCore", "status", "version",
    "createdAtMs", "updatedAtMs", "coverUrl", "releaseYear", "metadataComplete", "tags"],
  saves: ["saveStateId", "gameId", "gameTitle", "name", "version", "createdAtMs", "discIndex",
    "activeDurationMs", "sizeBytes", "screenshotUrl", "core", "platformId", "platformInstance"],
  reviews: ["itemId", "reviewVersion", "importJobId", "sourceDisplayName", "draftTitle", "platformInstance",
    "sourceTotalSizeBytes", "sourceMd5", "sourceKind", "sourceLabel", "pegasusImportId", "emulationStationImportId"],
};

export async function readPopulation(client) {
  return Object.fromEntries(await Promise.all(Object.entries(collections).map(async ([kind, [route, idKey]]) => {
    const rows = [];
    const cursors = new Set();
    let cursor = null;
    const limit = kind === "reviews" ? 20 : 100;
    for (let page = 0; page < 10_000 / limit; page++) {
      const response = await client.json("GET", `${route}?limit=${limit}${kind === "saves" ? "&availability=ALL" : ""}${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`);
      if (!Array.isArray(response.items)) {throw new Error("RPG_009_PROVISION_POPULATION_INVALID");}
      rows.push(...response.items.map((item) => ({id: item[idKey], sha256: digest(
        Object.fromEntries(stableFields[kind].filter((key) => key in item).map((key) => [key, item[key]])),
      )})));
      cursor = response.nextCursor;
      if (cursor === null) {
        if (rows.some((row) => typeof row.id !== "string" || !row.id) || new Set(rows.map((row) => row.id)).size !== rows.length) {
          throw new Error("RPG_009_PROVISION_POPULATION_INVALID");
        }
        return [kind, rows.sort((a, b) => a.id.localeCompare(b.id))];
      }
      if (typeof cursor !== "string" || !cursor || cursors.has(cursor)) {break;}
      cursors.add(cursor);
    }
    throw new Error("RPG_009_PROVISION_POPULATION_PAGINATION_INVALID");
  })));
}

export function preservedPopulation(before, after, expectedNew) {
  const retained = {};
  for (const kind of Object.keys(collections)) {
    const oldIds = new Set(before[kind].map((row) => row.id));
    retained[kind] = after[kind].filter((row) => oldIds.has(row.id));
    if (JSON.stringify(before[kind]) !== JSON.stringify(retained[kind])) {
      throw new Error("RPG_009_PROVISION_POPULATION_CHANGED");
    }
    const added = after[kind].filter((row) => !oldIds.has(row.id)).map((row) => row.id).sort();
    if (JSON.stringify(added) !== JSON.stringify([...expectedNew[kind]].sort())) {
      throw new Error("RPG_009_PROVISION_FINAL_CARDINALITY_INVALID");
    }
  }
  return {before, after: retained};
}

export async function assertPackCasePopulation(client, provision, references, reviewIds, published) {
  const baseline = checkedPopulationPreservation(provision).before;
  const publishedIds = new Set(published.map((row) => row.itemId));
  const population = await readPopulation(client);
  return preservedPopulation(baseline, population, {
    games: [...Object.values(references).map((row) => row.gameId), ...published.map((row) => row.gameId)],
    saves: [references.restorableCheckpoint.saveStateId],
    reviews: Object.values(reviewIds).filter((id) => !publishedIds.has(id)),
  });
}

export function checkedPopulationPreservation(value) {
  const invalid = () => {throw new Error("RPG_ACCEPTANCE_PACK_POPULATION_PRESERVATION_INVALID");};
  if (!value || Object.keys(value).sort().join() !== "after,before" ||
      JSON.stringify(value.before) !== JSON.stringify(value.after) ||
      !value.before || Object.keys(value.before).sort().join() !== "games,reviews,saves") {invalid();}
  for (const rows of Object.values(value.before)) {
    if (!Array.isArray(rows) || rows.length > 10_000) {invalid();}
    let previous = "";
    for (const row of rows) {
      if (!row || Object.keys(row).sort().join() !== "id,sha256" ||
          !/^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(row.id) ||
          !/^[0-9a-f]{64}$/.test(row.sha256) || row.id <= previous) {invalid();}
      previous = row.id;
    }
  }
  return value;
}

function digest(value) {
  return createHash("sha256").update(JSON.stringify(canonical(value))).digest("hex");
}

function canonical(value) {
  if (Array.isArray(value)) {return value.map(canonical);}
  if (!value || typeof value !== "object") {return value;}
  return Object.fromEntries(Object.keys(value).sort().map((key) => [key, canonical(value[key])]));
}
