import {expect, it} from "vitest";
import type {RuntimeStartupTaskV1} from "./runtime/contract";
import {StartupTimeline} from "./startup-task-model";

const task = (id: string, state: RuntimeStartupTaskV1["state"] = "RUNNING"): RuntimeStartupTaskV1 => ({id, kind: "GAME_CONTENT", state, progress: null});

it("updates a row in place and evicts history without reviving hidden parallel work", () => {
  const timeline = new StartupTimeline();
  for (const id of ["1", "2", "3", "4"]) {timeline.receive(task(id));}
  expect(timeline.receive({...task("1"), progress: {loadedBytes: 50, totalBytes: 100}}).map(row => row.id)).toEqual(["2", "3", "4"]);
  expect(timeline.receive(task("1", "COMPLETED")).map(row => row.id)).toEqual(["2", "3", "4"]);
  expect(timeline.receive(task("3", "COMPLETED")).map(row => row.id)).toEqual(["2", "3", "4"]);
  const rows = timeline.receive({...task("4"), progress: {loadedBytes: 100, totalBytes: 100}});
  expect(rows.at(-1)?.state).toBe("RUNNING");
});

it("ignores invalid and late events instead of inventing progress or reopening completion", () => {
  const timeline = new StartupTimeline(); timeline.receive(task("1")); timeline.receive(task("1", "COMPLETED"));
  expect(timeline.receive(task("1"))[0].state).toBe("COMPLETED");
  expect(timeline.receive({...task("2"), progress: {loadedBytes: 2, totalBytes: 1}})).toHaveLength(1);
  expect(timeline.receive(task("missing", "COMPLETED"))).toHaveLength(1);
});

it("does not resurrect completed work after it leaves the visible window", () => {
  const timeline = new StartupTimeline();
  timeline.receive(task("parallel"));
  for (const id of ["2", "3", "4"]) {timeline.receive(task(id));}
  timeline.receive(task("parallel", "COMPLETED"));
  expect(timeline.receive(task("parallel")).map(row => row.id)).toEqual(["2", "3", "4"]);
  timeline.receive(task("2", "COMPLETED")); timeline.receive(task("5"));
  expect(timeline.receive(task("2")).map(row => row.id)).toEqual(["3", "4", "5"]);
});
