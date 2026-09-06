import {protectedRoles} from "./rpgmaker_pack_provision_plan.mjs";
import {assertCleanPlayer, openPlayer, productLaunch} from "./rpgmaker_pack_provision_product.mjs";
import {advanceFixture, finishPreview, observeFixturePosition, observePreviewFrames,
  waitForPreviewReady} from "./rpgmaker_preview_actions.mjs";

// Publication and the persistent save are already committed product actions.
// Verify their current consumers without importing again or writing another save.
export async function trialProtectedReferences(context, client, base, references) {
  const results = {};
  for (const [role, reference] of Object.entries(references)) {
    const [, , , targetId, generation] = protectedRoles[role];
    const fresh = await trialProduct(context, client, base, reference.gameId, null, targetId, generation, [0, 1, 2]);
    const restore = reference.saveStateId
      ? await trialProduct(context, client, base, reference.gameId, reference.saveStateId, targetId, generation, [1, 2]) : null;
    if (restore && (restore.launchId === fresh.launchId || !same(restore.positions[0], fresh.positions[1])) ) {
      throw new Error("RPG_009_CONTINUATION_PRODUCT_RESTORE_INVALID");
    }
    results[role] = {gameId: reference.gameId, fresh, restore};
  }
  return results;
}

async function trialProduct(context, client, base, gameId, saveStateId, targetId, generation, expected) {
  const startedAtMs = Date.now();
  const launch = await productLaunch(client, gameId, saveStateId);
  const page = await openPlayer(context, base, launch.playUrl);
  const envelope = page.__retromEnvelope;
  if (envelope.runtime.targetId !== targetId || (saveStateId && !envelope.restore)) {
    throw new Error("RPG_009_CONTINUATION_PRODUCT_TARGET_INVALID");
  }
  await waitForPreviewReady(page);
  const frames = await observePreviewFrames(page);
  const positions = [];
  for (const state of expected) {
    if (positions.length) {await advanceFixture(page, ["ArrowRight", "KeyX"]);}
    const position = await observeFixturePosition(page, generation, page.__retromOwnedFixture);
    if (position.fixtureState !== state || (positions.length && same(position, positions.at(-1)))) {
      throw new Error("RPG_009_CONTINUATION_PRODUCT_INPUT_INVALID");
    }
    positions.push(position);
  }
  await finishPreview(page, launch.launchId);
  assertCleanPlayer(page);
  return {launchId: launch.launchId, targetId, saveStateId, frames, positions, startedAtMs, finishedAtMs: Date.now(),
    restore: envelope.restore ? {sha256: envelope.restore.sha256, sizeBytes: envelope.restore.sizeBytes, format: envelope.restore.format} : null};
}

function same(a, b) {return JSON.stringify(a) === JSON.stringify(b);}
