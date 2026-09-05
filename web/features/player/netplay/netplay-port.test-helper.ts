import {vi} from "vitest";
import type {NetplayRuntimePort} from "../runtime/netplay-port-adapter";

type CanonicalPlayers = readonly [readonly number[], readonly number[], readonly number[], readonly number[]];
type Overrides = {
  pauseAtBoundary?: () => unknown;
  captureState?: (frame: number) => Uint8Array | Promise<Uint8Array>;
  loadStateAndWait?: (state: Uint8Array, frame: number) => unknown;
  runNetplayFrame?: (players: CanonicalPlayers, frame: number, suppressOutput: boolean) => unknown;
  runFrame?: (players: CanonicalPlayers, frame: number, suppressOutput: boolean) => unknown;
  sampleLocalControls?: () => number[];
  resetLocalControls?: () => unknown;
  close?: () => unknown;
};

export function testNetplayPort(overrides: Overrides = {}): NetplayRuntimePort & {runNetplayFrame: NonNullable<Overrides["runNetplayFrame"]>} {
  const pauseAtBoundary = overrides.pauseAtBoundary ?? vi.fn(async () => 0);
  const captureState = overrides.captureState ?? vi.fn(async () => Uint8Array.of(0));
  const loadStateAndWait = overrides.loadStateAndWait ?? vi.fn(async () => undefined);
  const runNetplayFrame = overrides.runNetplayFrame ?? overrides.runFrame ?? vi.fn(async () => undefined);
  const sampleLocalControls = overrides.sampleLocalControls ?? vi.fn(() => Array(24).fill(0));
  const resetLocalControls = overrides.resetLocalControls ?? vi.fn();
  const close = overrides.close ?? vi.fn(async () => undefined);
  return {
    pauseAtBoundary: vi.fn(async () => Number(await pauseAtBoundary()) || 0),
    captureState: vi.fn(async (frame) => new Uint8Array(await captureState(frame))),
    loadStateAndWait: vi.fn(async (state, frame) => {await loadStateAndWait(state, frame);}),
    runFrame: vi.fn(async (players, frame, suppressOutput = false) => {await runNetplayFrame(players, frame, suppressOutput);}),
    sampleLocalControls: vi.fn(() => [...sampleLocalControls()]),
    resetLocalControls: vi.fn(() => {resetLocalControls();}),
    close: vi.fn(async () => {await close();}),
    runNetplayFrame,
  };
}

export function opaqueState(bytes: number[]) {return new Uint8Array(bytes);}
