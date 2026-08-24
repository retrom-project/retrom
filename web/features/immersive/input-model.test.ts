import { describe, expect, it } from "vitest";
import { GamepadClaimModel, NavigationInputModel, isStandardGamepad, type GamepadSnapshot } from "./input-model";

function gamepad(options: { axes?: number[]; buttons?: number[]; connected?: boolean; index?: number; mapping?: string } = {}): GamepadSnapshot {
  const pressed = new Set(options.buttons ?? []);
  return {
    axes: options.axes ?? [0, 0],
    buttons: Array.from({ length: 16 }, (_, index) => ({ pressed: pressed.has(index), value: pressed.has(index) ? 1 : 0 })),
    connected: options.connected ?? true,
    index: options.index ?? 0,
    mapping: options.mapping ?? "standard",
  };
}

function arm(model: NavigationInputModel, startAt = 0) {
  model.update(gamepad(), startAt);
  return model.update(gamepad(), startAt + 120);
}

describe("immersive navigation input model", () => {
  it("accepts only connected finite standard pads with the required buttons", () => {
    expect(isStandardGamepad(gamepad())).toBe(true);
    expect(isStandardGamepad(gamepad({ mapping: "" }))).toBe(false);
    expect(isStandardGamepad(gamepad({ connected: false }))).toBe(false);
    expect(isStandardGamepad({ ...gamepad(), axes: [Number.NaN, 0] })).toBe(false);
    expect(isStandardGamepad({ ...gamepad(), buttons: gamepad().buttons.slice(0, 15) })).toBe(false);
  });

  it("arms after 120 ms neutral and then emits confirm on A", () => {
    const model = new NavigationInputModel();
    expect(model.update(gamepad(), 0).neutralReady).toBe(false);
    expect(model.update(gamepad(), 119).neutralReady).toBe(false);
    expect(model.update(gamepad(), 120).neutralReady).toBe(true);
    expect(model.update(gamepad({ buttons: [0] }), 121).actions).toEqual(["confirm"]);
  });

  it("arms after neutral and emits a direction without clearing the armed state", () => {
    const model = new NavigationInputModel();
    arm(model);
    expect(model.update(gamepad({ axes: [-0.61, 0] }), 121).actions).toEqual(["left"]);
    expect(model.update(gamepad({ axes: [-0.4, 0] }), 470).actions).toEqual([]);
    expect(model.update(gamepad({ axes: [-0.4, 0] }), 471).actions).toEqual(["left"]);
    expect(model.update(gamepad({ axes: [-0.34, 0] }), 472).actions).toEqual([]);
  });

  it("does not arm while a button is held across the gate", () => {
    const model = new NavigationInputModel();
    expect(model.update(gamepad({ buttons: [0] }), 0).actions).toEqual([]);
    expect(model.update(gamepad({ buttons: [0] }), 500).neutralReady).toBe(false);
    expect(model.update(gamepad(), 600).neutralReady).toBe(false);
    expect(model.update(gamepad(), 719).neutralReady).toBe(false);
    expect(model.update(gamepad(), 720).neutralReady).toBe(true);
    expect(model.update(gamepad({ buttons: [0] }), 721).actions).toEqual(["confirm"]);
  });

  it("accepts a safe direction immediately after Player return without accepting A or B", () => {
    const model = new NavigationInputModel(true);
    const update = model.update(gamepad({ buttons: [0, 13] }), 0);
    expect(update).toMatchObject({ actions: ["down"], neutralReady: false });
    expect(model.update(gamepad(), 20).neutralReady).toBe(false);
    expect(model.update(gamepad(), 140).neutralReady).toBe(true);
    model.reset();
    expect(model.update(gamepad({ buttons: [13] }), 141).actions).toEqual([]);
  });

  it("repeats held directions at 350 ms then every 120 ms and reacts immediately to reversal", () => {
    const model = new NavigationInputModel();
    arm(model);
    expect(model.update(gamepad({ buttons: [15] }), 200).actions).toEqual(["right"]);
    expect(model.update(gamepad({ buttons: [15] }), 549).actions).toEqual([]);
    expect(model.update(gamepad({ buttons: [15] }), 550).actions).toEqual(["right"]);
    expect(model.update(gamepad({ buttons: [15] }), 670).actions).toEqual(["right"]);
    expect(model.update(gamepad({ buttons: [14] }), 671).actions).toEqual(["left"]);
  });

  it("orders a same-frame direction before A and gives B precedence over A", () => {
    const model = new NavigationInputModel();
    arm(model);
    expect(model.update(gamepad({ buttons: [0, 15] }), 121).actions).toEqual(["right", "confirm"]);
    model.update(gamepad(), 122);
    expect(model.update(gamepad({ buttons: [0, 1] }), 123).actions).toEqual(["cancel"]);
  });

  it("emits Select as a single menu edge only after the neutral gate", () => {
    const model = new NavigationInputModel();
    expect(model.update(gamepad({ buttons: [8] }), 0).actions).toEqual([]);
    expect(model.update(gamepad({ buttons: [8] }), 500).neutralReady).toBe(false);
    arm(model, 600);
    expect(model.update(gamepad({ buttons: [8] }), 721).actions).toEqual(["menu"]);
    expect(model.update(gamepad({ buttons: [8] }), 1_200).actions).toEqual([]);
  });

  it("emits Y as favorite and gives A, B and Select precedence", () => {
    const model = new NavigationInputModel();
    arm(model);
    expect(model.update(gamepad({ buttons: [3] }), 121).actions).toEqual(["favorite"]);
    model.update(gamepad(), 122);
    expect(model.update(gamepad({ buttons: [0, 3] }), 123).actions).toEqual(["confirm"]);
    model.update(gamepad(), 124);
    expect(model.update(gamepad({ buttons: [1, 3] }), 125).actions).toEqual(["cancel"]);
    model.update(gamepad(), 126);
    expect(model.update(gamepad({ buttons: [3, 8] }), 127).actions).toEqual(["menu"]);
  });

  it("resets edges and the neutral gate after disconnect", () => {
    const model = new NavigationInputModel();
    arm(model);
    expect(model.update(gamepad({ buttons: [1] }), 121).actions).toEqual(["cancel"]);
    expect(model.update(null, 122).neutralReady).toBe(false);
    expect(model.update(gamepad({ buttons: [1] }), 123).actions).toEqual([]);
  });
});

describe("gamepad claim model", () => {
  it("claims the lowest standard index on simultaneous button edges", () => {
    const claims = new GamepadClaimModel();
    claims.update([gamepad({ index: 4 }), gamepad({ index: 2 })]);
    expect(claims.update([gamepad({ index: 4, buttons: [3] }), gamepad({ index: 2, buttons: [7] })])).toEqual({
      claimedIndex: 2,
      unsupportedEdge: false,
    });
  });

  it("reports but does not claim an unknown mapping", () => {
    const claims = new GamepadClaimModel();
    claims.update([gamepad({ mapping: "" })]);
    expect(claims.update([gamepad({ mapping: "", buttons: [0] })])).toEqual({ claimedIndex: null, unsupportedEdge: true });
  });
});
