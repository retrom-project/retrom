import assert from "node:assert/strict";
import {test} from "node:test";
import {waitForAvailableCheckpoint} from "./player_checkpoint_readiness.mjs";

test("checkpoint readiness observes the ordinary disabled property without stealing game focus", async () => {
  let polls = 0;
  const page = {
    locator: () => {assert.fail("readiness must not interact with the HUD");},
    getByRole: (role, options) => {
      assert.equal(role, "button");
      assert.deepEqual(options, {name: "创建存档", exact: true, includeHidden: true});
      return {isEnabled: async () => ++polls === 2};
    },
    waitForTimeout: async (milliseconds) => {assert.equal(milliseconds, 100);},
  };
  await waitForAvailableCheckpoint(page);
  assert.equal(polls, 2);
});
