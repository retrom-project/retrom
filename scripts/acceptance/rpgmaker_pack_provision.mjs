#!/usr/bin/env node
import { chromium } from "../../web/node_modules/playwright/index.mjs";
import { createProductClient } from "./rpgmaker_security_upload.mjs";
import {
  assertPlanTarget, buildPlan, buildProvisionEvidence, loadGeneratorInputs, protectedRoles, reviewRoles,
  writePlan, writeProvisionEvidence,
} from "./rpgmaker_pack_provision_plan.mjs";
import { gitProvenance } from "./rpgmaker_evidence_provenance.mjs";
import { isLocalAcceptanceHostname } from "./rpgmaker_url.mjs";
import {
  approveReview, assertFreshInstance, assertProvisionedState, createProductSave, importReview,
  installRuntimePack, rpgPlatformInstances, selectRuntimePack, validateReview,
} from "./rpgmaker_pack_provision_product.mjs";

const arguments_ = parseArguments(process.argv.slice(2));
const inputs = loadGeneratorInputs(arguments_.inputs);
assertPlanTarget(arguments_.plan);
assertPlanTarget(arguments_.evidence);
if (arguments_.plan === arguments_.evidence) { throw new Error("RPG_009_PROVISION_OUTPUT_COLLISION"); }
const baseUrl = normalizedBase(required("RETROM_ACCEPTANCE_BASE_URL"));
const browser = await chromium.launch({ executablePath: required("RETROM_CHROME_EXECUTABLE"), headless: true });

try {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  const loginResponse = await context.request.post(`${baseUrl}/api/v1/auth/login`, {
    headers: { Origin: baseUrl },
    data: { username: required("RETROM_ACCEPTANCE_USERNAME"), password: required("RETROM_ACCEPTANCE_PASSWORD") },
    failOnStatusCode: false,
  });
  if (loginResponse.status() !== 200) { throw new Error(`RPG_009_PROVISION_LOGIN_${loginResponse.status()}`); }
  const login = await loginResponse.json();
  if (!login.csrfToken) { throw new Error("RPG_009_PROVISION_CSRF_MISSING"); }
  const client = createProductClient(context, baseUrl, login.csrfToken);
  await assertFreshInstance(client);
  const coreIds = [...new Set(Object.values(reviewRoles).map((item) => item[0]))];
  const instances = await rpgPlatformInstances(client, coreIds);
  const installations = await installProtectedPacks(client, inputs);
  const protectedReferences = await createProtectedReferences(
    context, client, baseUrl, inputs, instances, installations,
  );
  const reviewIds = await createReviewMatrix(context, client, baseUrl, inputs, instances);
  await assertProvisionedState(client, protectedReferences, reviewIds);
  const plan = buildPlan(inputs, reviewIds, protectedReferences);
  writePlan(arguments_.plan, plan);
  const evidence = buildProvisionEvidence(inputs, plan, gitProvenance());
  writeProvisionEvidence(arguments_.evidence, evidence);
  process.stdout.write(`${JSON.stringify(evidence, null, 2)}\n`);
} finally {
  await browser.close();
}

async function installProtectedPacks(client, manifest) {
  const installations = {};
  for (const [role, identity] of Object.entries(protectedRoles)) {
    installations[role] = await installRuntimePack(
      client, manifest.protectedPackInputs[role], identity[5],
    );
  }
  const catalog = await client.json("GET", "/api/v1/admin/runtime-asset-packs");
  if (catalog.installations?.length !== 2) { throw new Error("RPG_009_PROVISION_PACK_CARDINALITY_INVALID"); }
  return installations;
}

async function createProtectedReferences(context, client, base, manifest, instances, installations) {
  const references = {};
  for (const [role, identity] of Object.entries(protectedRoles)) {
    let review = await importReview(client, manifest.protectedProjects[role].sourcePath, instance(instances, identity[3]));
    assertProtectedReview(role, review, identity);
    review = await selectRuntimePack(client, review, installations[role].installationId);
    await validateReview(context, client, base, review, identity[4]);
    const gameId = await approveReview(client, review.itemId);
    references[role] = { installationId: installations[role].installationId, gameId };
    if (role === "restorableCheckpoint") {
      references[role].saveStateId = await createProductSave(context, client, base, gameId, identity[3]);
    }
  }
  return references;
}

async function createReviewMatrix(context, client, base, manifest, instances) {
  const reviews = {};
  for (const [role, identity] of Object.entries(reviewRoles)) {
    const review = await importReview(client, manifest.reviewProjects[role].sourcePath, instance(instances, identity[0]));
    assertReviewRole(role, review, identity);
    reviews[role] = review;
  }
  await validateReadyReviews(context, client, base, reviews);
  await assertReviewMatrixUnmodified(client, reviews);
  return Object.fromEntries(Object.entries(reviews).map(([role, review]) => [role, review.itemId]));
}

async function validateReadyReviews(context, client, base, reviews) {
  for (const [role, review] of Object.entries(reviews)) {
    const identity = reviewRoles[role];
    if (identity[2] !== "ready") { continue; }
    await validateReview(context, client, base, review, identity[1]);
    const current = await client.json("GET", `/api/v1/admin/reviews/${review.itemId}`);
    if (!current.canApprove || !current.rpgMaker?.runtimeValidationCurrent
        || current.rpgMaker.runtimeValidation?.state !== "PASSED") {
      throw new Error("RPG_009_PROVISION_READY_REVIEW_INVALID");
    }
  }
}

async function assertReviewMatrixUnmodified(client, reviews) {
  for (const [role, review] of Object.entries(reviews)) {
    const current = await client.json("GET", `/api/v1/admin/reviews/${review.itemId}`);
    const ready = reviewRoles[role][2] === "ready";
    if (current.canApprove !== ready || current.rpgMaker?.runtimePackSelections?.length !== 0) {
      throw new Error(`RPG_009_PROVISION_REVIEW_MUTATED_${role}`);
    }
    if (!ready
        && (current.rpgMaker.runtimeValidationCurrent || current.rpgMaker.runtimeValidation)) {
      throw new Error(`RPG_009_PROVISION_UNREADY_VALIDATION_PRESENT_${role}`);
    }
  }
}

function assertProtectedReview(role, review, identity) {
  const rpg = review.rpgMaker;
  if (review.canApprove || rpg?.selectedCoreId !== identity[3]
      || rpg.generation !== identity[4] || rpg.runtimePackRequirements?.length !== 1
      || rpg.runtimePackSelections?.length !== 0 || rpg.selfContainedOverride) {
    throw new Error(`RPG_009_PROVISION_PROTECTED_REVIEW_INVALID_${role}`);
  }
}

function assertReviewRole(role, review, identity) {
  const rpg = review.rpgMaker;
  if (review.canApprove || rpg?.selectedCoreId !== identity[0]
      || rpg.generation !== identity[1] || rpg.runtimePackSelections?.length !== 0
      || rpg.selfContainedOverride) {
    throw new Error(`RPG_009_PROVISION_REVIEW_ROLE_INVALID_${role}`);
  }
  assertReviewRequirements(role, rpg);
}

function assertReviewRequirements(role, rpg) {
  const requirements = rpg.runtimePackRequirements ?? [];
  if (role.endsWith("SelfContained")) {
    if (!rpg.selfContained || requirements.length) { throw new Error("RPG_009_PROVISION_SELF_CONTAINED_INVALID"); }
  } else if (role.endsWith("NoRtp")) {
    if (rpg.selfContained || requirements.length) { throw new Error("RPG_009_PROVISION_NO_RTP_INVALID"); }
  } else if (requirements.length !== 1 || requirements[0].declaredName !== declaredName(role)) {
    throw new Error("RPG_009_PROVISION_REQUIREMENT_INVALID");
  }
}

function declaredName(role) {
  return {
    rpg2000Missing: "RPG2000_RTP", rpg2003Missing: "RPG2003_RTP",
    rpgxpStandardAmbiguous: "Standard", rpgxpCustom: "RetromCustomXP",
    rpgvxStandardAmbiguous: "RPGVX", rpgvxCustom: "RetromCustomVX",
    rpgvxaceStandardAmbiguous: "RPGVXAce", rpgvxaceCustom: "RetromCustomVXAce",
  }[role];
}

function instance(instances, coreId) {
  const identifier = instances.get(coreId);
  if (!identifier) { throw new Error(`RPG_009_PROVISION_PLATFORM_MISSING_${coreId}`); }
  return identifier;
}

function parseArguments(values) {
  if (values.length !== 6 || values[0] !== "--inputs" || values[2] !== "--plan" || values[4] !== "--evidence") {
    throw new Error("usage: rpgmaker_pack_provision.mjs --inputs <absolute-inputs.json> --plan <absolute-new-plan.json> --evidence <absolute-new-evidence.json>");
  }
  return { inputs: values[1], plan: values[3], evidence: values[5] };
}

function normalizedBase(value) {
  const parsed = new URL(value);
  if (parsed.username || parsed.password || parsed.search || parsed.hash || parsed.pathname !== "/") {
    throw new Error("RPG_009_PROVISION_BASE_URL_INVALID");
  }
  if (parsed.protocol !== "https:" && !isLocalAcceptanceHostname(parsed.hostname)) {
    throw new Error("RPG_009_PROVISION_BASE_URL_REQUIRES_HTTPS");
  }
  return parsed.origin;
}

function required(name) {
  const value = process.env[name];
  if (!value) { throw new Error(`RPG_009_PROVISION_ENV_MISSING_${name}`); }
  return value;
}
