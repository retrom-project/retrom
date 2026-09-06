import {readFileSync} from "node:fs";
import {resolve} from "node:path";
import {fileURLToPath} from "node:url";
import {request} from "../../web/node_modules/playwright/index.mjs";
import {createProductClient} from "./rpgmaker_security_upload.mjs";
import {assertPackCasePopulation} from "./rpgmaker_pack_population.mjs";
import {normalizedBase} from "./rpgmaker_url.mjs";

export function validateCatalog(catalog, plan, observed) {
  const expected = [...Object.values(plan.protectedReferences).map((row) => row.installationId),
    ...Object.values(observed.installations).map((row) => row.installationId)].sort();
  if (JSON.stringify(catalog.installations?.map((row) => row.installationId).sort()) !== JSON.stringify(expected)) {
    throw new Error("RPG_ACCEPTANCE_PACK_REINSPECT_CATALOG_INVALID");
  }
  for (const [role, prior] of Object.entries(observed.installations)) {
    const row = catalog.installations.find((item) => item.installationId === prior.installationId);
    if (row.status !== (role === "zeroReference" ? "DELETED" : "READY") ||
        ["definitionId", "filesDigest", "fileCount", "totalBytes", "sourceNote"].some((key) => row[key] !== prior[key])) {
      throw new Error("RPG_ACCEPTANCE_PACK_REINSPECT_CATALOG_INVALID");
    }
  }
}

async function run() {
  const base = normalizedBase(process.env.RETROM_ACCEPTANCE_BASE_URL);
  const load = (name) => JSON.parse(readFileSync(process.env[name], "utf8"));
  const plan = load("RETROM_ACC_RPG_009_PLAN"), provision = load("RETROM_ACC_RPG_009_PROVISION_EVIDENCE");
  const observed = JSON.parse(readFileSync(resolve(process.env.RETROM_RPG_CASE_DIR, "rpgmaker-product.json"), "utf8"));
  const context = await request.newContext();
  try {
    const login = await context.post(`${base}/api/v1/auth/login`, {headers: {Origin: base},
      data: {username: process.env.RETROM_ACCEPTANCE_USERNAME, password: process.env.RETROM_ACCEPTANCE_PASSWORD}});
    if (login.status() !== 200) {throw new Error("RPG_ACCEPTANCE_PACK_REINSPECT_LOGIN_FAILED");}
    const client = createProductClient({request: context}, base, (await login.json()).csrfToken);
    await assertPackCasePopulation(client, provision.populationPreservation, plan.protectedReferences,
      plan.reviewIds, observed.reviews.published);
    validateCatalog(await client.json("GET", "/api/v1/admin/runtime-asset-packs"), plan, observed);
    console.log("RPG_ACCEPTANCE_PACK_REINSPECT_CURRENT_POPULATION_AND_CATALOG_VERIFIED");
  } finally {await context.dispose();}
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {await run();}
