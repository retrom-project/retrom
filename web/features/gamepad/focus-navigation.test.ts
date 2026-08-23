import { afterEach, describe, expect, it, vi } from "vitest";
import {
  activateControllerFocus,
  changeEditableController,
  controllerBackInScope,
  controllerGroupAction,
  findControllerNeighbor,
  focusControllerDefault,
  moveControllerFocus,
} from "./focus-navigation";

function button(key: string, rect: Partial<DOMRect>, label = key) {
  const element = document.createElement("button");
  element.dataset.gamepadKey = key;
  element.textContent = label;
  element.getBoundingClientRect = () => ({
    x: 0, y: 0, width: 100, height: 40, top: 0, right: 100, bottom: 40, left: 0,
    toJSON: () => ({}), ...rect,
  });
  document.body.append(element);
  return element;
}

function visible(element: HTMLElement) {
  element.getBoundingClientRect = () => ({
    x: 0, y: 0, width: 200, height: 40, top: 0, right: 200, bottom: 40, left: 0,
    toJSON: () => ({}),
  });
}

afterEach(() => {
  document.body.replaceChildren();
  vi.restoreAllMocks();
});

describe("controller focus navigation", () => {
  it("prefers aligned candidates, then main distance and DOM order", () => {
    const current = button("current", { x: 0, y: 0, left: 0, right: 100, top: 0, bottom: 40 });
    const aligned = button("aligned", { x: 130, y: 0, left: 130, right: 230, top: 0, bottom: 40 });
    button("diagonal", { x: 105, y: 90, left: 105, right: 205, top: 90, bottom: 130 });
    expect(findControllerNeighbor(current, "right")).toBe(aligned);
  });

  it("uses explicit neighbors before geometry", () => {
    const current = button("current", { x: 0, y: 0, left: 0, right: 100 });
    const explicit = button("explicit", { x: 400, y: 200, left: 400, right: 500, top: 200, bottom: 240 });
    button("near", { x: 120, y: 0, left: 120, right: 220 });
    current.dataset.gamepadRight = "explicit";
    expect(findControllerNeighbor(current, "right")).toBe(explicit);
  });

  it("focuses defaults, moves real DOM focus and activates existing semantics", () => {
    const first = button("first", { x: 0, left: 0, right: 100 });
    const next = button("next", { x: 130, left: 130, right: 230 });
    first.dataset.gamepadDefault = "true";
    const click = vi.fn();
    next.addEventListener("click", click);
    expect(focusControllerDefault()).toBe(first);
    expect(document.activeElement).toBe(first);
    expect(moveControllerFocus("right")).toBe(next);
    expect(activateControllerFocus()).toBe(true);
    expect(click).toHaveBeenCalledOnce();
  });

  it("keeps focus inside the top controller scope and safely closes it", () => {
    button("outside", { x: 0, left: 0, right: 100 });
    const scope = document.createElement("section");
    scope.dataset.gamepadScope = "";
    scope.dataset.gamepadOpen = "true";
    scope.getBoundingClientRect = () => ({
      x: 0, y: 0, width: 300, height: 300, top: 0, right: 300, bottom: 300, left: 0, toJSON: () => ({}),
    });
    document.body.append(scope);
    const close = button("close", { x: 0, left: 0, right: 100 }, "关闭");
    close.dataset.gamepadBack = "true";
    scope.append(close);
    const clicked = vi.fn();
    close.addEventListener("click", clicked);
    focusControllerDefault();
    expect(document.activeElement).toBe(close);
    expect(controllerBackInScope()).toBe(true);
    expect(clicked).toHaveBeenCalledOnce();
  });

  it("edits native selects without changing their keyboard behavior", () => {
    document.body.innerHTML = "<select><option>A</option><option>B</option></select>";
    const select = document.querySelector<HTMLSelectElement>("select")!;
    visible(select);
    select.focus();
    expect(activateControllerFocus()).toBe(true);
    expect(changeEditableController("down")).toBe(true);
    expect(select.selectedIndex).toBe(1);
    expect(controllerBackInScope()).toBe(true);
    expect(select.selectedIndex).toBe(0);
  });

  it("adjusts range controls by five and group-jumps by twenty", () => {
    document.body.innerHTML = '<input type="range" min="0" max="100" value="50" data-gamepad-step="5" data-gamepad-group-step="20">';
    const range = document.querySelector<HTMLInputElement>("input")!;
    visible(range);
    range.focus();
    expect(activateControllerFocus()).toBe(true);
    expect(changeEditableController("right")).toBe(true);
    expect(range.value).toBe("55");
    expect(controllerGroupAction(true)).toBe(true);
    expect(range.value).toBe("75");
    expect(controllerBackInScope()).toBe(true);
    expect(range.value).toBe("50");
  });
});
