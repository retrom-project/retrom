import {expect, it, vi, afterEach} from "vitest";
import {createElement} from "react";
import {cleanup, render} from "@testing-library/react";
import {PlayerStartupTasks} from "./player-startup-tasks";
import type {RuntimeStartupTaskV1} from "./runtime/contract";
import {startupScrollSlots} from "./startup-task-motion";

it("advances surviving rows together when a new task arrives or history is evicted", () => {
  expect(startupScrollSlots([], ["a"])).toBe(1);
  expect(startupScrollSlots(["a"], ["a", "b"])).toBe(1);
  expect(startupScrollSlots(["a", "b", "c"], ["b", "c", "d"])).toBe(1);
  expect(startupScrollSlots(["a", "b", "c"], ["c", "d", "e"])).toBe(2);
});

it("does not slide rows through one another on completion reorder or progress updates", () => {
  expect(startupScrollSlots(["a", "b", "c"], ["b", "a", "c"])).toBe(0);
  expect(startupScrollSlots(["a", "b"], ["a", "b"])).toBe(0);
  expect(startupScrollSlots(["a", "b"], ["c", "d"])).toBe(0);
});

afterEach(() => {cleanup(); vi.restoreAllMocks();});

it("moves only the whole list and does not restart scrolling on progress or completion updates", () => {
  const cancel = vi.fn();
  const animate = vi.fn(() => ({cancel}));
  Object.defineProperty(HTMLElement.prototype, "animate", {value: animate, configurable: true});
  const task = (id: string): RuntimeStartupTaskV1 => ({id, kind: "GAME_CONTENT", state: "RUNNING", progress: null});
  try {
    const {rerender, unmount} = render(createElement(PlayerStartupTasks, {tasks: [task("a")]}));
    expect(animate).toHaveBeenCalledTimes(1);
    rerender(createElement(PlayerStartupTasks, {tasks: [{...task("a"), progress: {loadedBytes: 50, totalBytes: 100}}]}));
    rerender(createElement(PlayerStartupTasks, {tasks: [{...task("a"), state: "COMPLETED"}]}));
    expect(animate).toHaveBeenCalledTimes(1);
    rerender(createElement(PlayerStartupTasks, {tasks: [task("a"), task("b")]}));
    rerender(createElement(PlayerStartupTasks, {tasks: [task("b"), task("c")]}));
    expect(animate).toHaveBeenCalledTimes(3);
    expect(cancel).toHaveBeenCalledTimes(2);
    expect(animate.mock.contexts.every(element => element instanceof HTMLOListElement)).toBe(true);
    unmount();
    expect(cancel).toHaveBeenCalledTimes(3);
  } finally {Reflect.deleteProperty(HTMLElement.prototype, "animate");}
});
