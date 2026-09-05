import type {RuntimeNetplayPortV1} from "./contract";
import {playerRuntimeError} from "./errors";

type CanonicalPlayers = readonly [readonly number[], readonly number[], readonly number[], readonly number[]];

export interface NetplayRuntimePort {
  pauseAtBoundary(): Promise<number>;
  captureState(frame: number): Promise<Uint8Array>;
  loadStateAndWait(state: Uint8Array, frame: number): Promise<void>;
  runFrame(players: CanonicalPlayers, frame: number, suppressOutput?: boolean): Promise<void>;
  sampleLocalControls(): number[];
  resetLocalControls(): void;
  close(): Promise<void>;
}

const playerCount = 4;
const controlCount = 24;

export class RuntimeNetplayPortAdapter implements NetplayRuntimePort {
  private closePromise: Promise<void> | null = null;

  constructor(private readonly port: RuntimeNetplayPortV1) {
    if (!validPort(port) || port.controlCount !== controlCount) {contractError();}
  }

  async pauseAtBoundary() {
    this.requireOpen();
    const frame = await this.port.pauseAtBoundary();
    if (!validFrame(frame)) {contractError();}
    return frame;
  }

  async captureState(frame: number) {
    this.requireFrame(frame);
    const state = await this.port.captureState(frame);
    if (!(state instanceof Uint8Array) || state.byteLength < 1) {contractError();}
    return new Uint8Array(state);
  }

  async loadStateAndWait(state: Uint8Array, frame: number) {
    this.requireFrame(frame);
    if (!(state instanceof Uint8Array) || state.byteLength < 1) {contractError();}
    await this.port.loadStateAndWait(new Uint8Array(state), frame);
  }

  async runFrame(players: CanonicalPlayers, frame: number, suppressOutput = false) {
    this.requireFrame(frame);
    if (!Array.isArray(players) || players.length !== playerCount || typeof suppressOutput !== "boolean") {
      contractError();
    }
    const controls = new Int16Array(playerCount * controlCount);
    for (let player = 0; player < playerCount; player += 1) {
      const values = players[player];
      if (!Array.isArray(values) || values.length !== controlCount) {contractError();}
      for (let control = 0; control < controlCount; control += 1) {
        const value = values[control];
        if (!Number.isSafeInteger(value) || value! < -32768 || value! > 32767) {contractError();}
        controls[player * controlCount + control] = value!;
      }
    }
    await this.port.runFrame(controls, frame, suppressOutput);
  }

  sampleLocalControls() {
    this.requireOpen();
    const controls = this.port.sampleLocalControls();
    if (!(controls instanceof Int16Array) || controls.length !== controlCount) {contractError();}
    return [...controls];
  }

  resetLocalControls() {this.requireOpen(); this.port.resetLocalControls();}

  close() {
    this.closePromise ??= Promise.resolve(this.port.close());
    return this.closePromise;
  }

  private requireFrame(frame: number) {
    this.requireOpen();
    if (!validFrame(frame)) {contractError();}
  }

  private requireOpen() {if (this.closePromise) {contractError();}}
}

function validPort(value: RuntimeNetplayPortV1) {
  return value !== null && typeof value === "object" && Number.isSafeInteger(value.controlCount) &&
    ["pauseAtBoundary", "captureState", "loadStateAndWait", "runFrame", "sampleLocalControls",
      "resetLocalControls", "close"].every((method) =>
      typeof (value as unknown as Record<string, unknown>)[method] === "function");
}

function validFrame(frame: number) {return Number.isSafeInteger(frame) && frame >= 0;}
function contractError(): never {throw playerRuntimeError("PLAYER_RUNTIME_CONTRACT_INVALID");}
