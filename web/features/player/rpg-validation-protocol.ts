import { newUuid } from "@/lib/crypto";
import type { components } from "@/lib/api/generated/schema";

export type RpgGate = components["schemas"]["RpgRuntimeGate"];
export type RpgGateEvidence = components["schemas"]["RpgRuntimeGateEvidence"];
export type RpgPosition = components["schemas"]["RpgPositionEvidence"];

type GatePhase = components["schemas"]["RpgRuntimeGatePhase"];
type FetchGate = (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>;

export const rpgValidationGates: readonly RpgGate[] = [
  "RUNTIME_READY",
  "ENGINE_PROFILE",
  "FRAMES_300",
  "INPUT",
  "AUDIO",
  "INITIAL_POSITION_RECORDED",
  "SAVE_POINT_RECORDED",
  "CHECKPOINT_CREATED",
  "POST_SAVE_STATE_DIVERGED",
  "ORIGINAL_LAUNCH_ENDED",
  "RESTORE_STARTED",
  "RESTORE_POSITION_VERIFIED",
  "RESTORE_SCREENSHOT",
  "RESTORE_INPUT",
];

export const originalValidationEventCount = 20;

export class RpgValidationGateClient {
  private sequence: number;
  private readonly launchId: string;
  private readonly signal: AbortSignal;
  private readonly fetchGate: FetchGate;

  constructor(launchId: string, lastSequence: number, signal: AbortSignal, fetchGate: FetchGate = globalThis.fetch) {
    this.launchId = launchId;
    this.signal = signal;
    this.fetchGate = fetchGate;
    this.sequence = lastSequence;
  }

  begin(gate: RpgGate) {
    return this.submit(gate, "BEGIN", {});
  }

  pass(gate: RpgGate, evidence: RpgGateEvidence) {
    return this.submit(gate, "PASS", evidence);
  }

  fail(gate: RpgGate) {
    return this.submit(gate, "FAIL", {});
  }

  private async submit(gate: RpgGate, phase: GatePhase, evidence: RpgGateEvidence) {
    const body = {
      sequence: this.sequence + 1,
      eventId: newUuid(),
      gate,
      phase,
      observedAtMs: Date.now(),
      evidence,
    };
    const accepted = await submitGateWithReplay(this.fetchGate, this.launchId, body, this.signal);
    this.sequence = accepted.sequence;
  }
}

async function submitGateWithReplay(
  fetchGate: FetchGate,
  launchId: string,
  body: components["schemas"]["RpgMakerGateEventRequest"],
  signal: AbortSignal,
) {
  let networkError: unknown;
  for (let attempt = 0; attempt < 2; attempt += 1) {
    try {
      const response = await fetchGate(`/runtime/launches/${launchId}/rpgmaker-gates/events`, {
        method: "POST",
        credentials: "same-origin",
        cache: "no-store",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
        signal,
      });
      if (!response.ok) {throw new Error(await responseErrorCode(response));}
      const accepted: unknown = await response.json();
      if (!isAcceptedGate(accepted, body)) {throw new Error("RPG_RUNTIME_PROTOCOL_VIOLATION");}
      return accepted;
    } catch (error) {
      if (signal.aborted || error instanceof Error && error.message.startsWith("RPG_")) {throw error;}
      networkError = error;
    }
  }
  throw networkError instanceof Error ? networkError : new Error("RPG_RUNTIME_GATE_UNAVAILABLE");
}

function isAcceptedGate(
  value: unknown,
  request: components["schemas"]["RpgMakerGateEventRequest"],
): value is components["schemas"]["RpgMakerGateEventAccepted"] {
  if (!value || typeof value !== "object" || Array.isArray(value)) {return false;}
  const accepted = value as Partial<components["schemas"]["RpgMakerGateEventAccepted"]>;
  const keys = Object.keys(value).sort().join(",");
  return keys === "eventId,idempotentReplay,sequence,validationState" &&
    accepted.sequence === request.sequence && accepted.eventId === request.eventId &&
    isValidationState(accepted.validationState) && typeof accepted.idempotentReplay === "boolean";
}

function isValidationState(value: unknown): value is components["schemas"]["RpgRuntimeValidationState"] {
  return value === "CREATED" || value === "STARTING" || value === "RUNNING" || value === "CHECKPOINTED" ||
    value === "RESTORED" || value === "AWAITING_DECISION" || value === "PASSED" || value === "FAILED" ||
    value === "EXPIRED";
}

async function responseErrorCode(response: Response) {
  try {
    const value: unknown = await response.json();
    if (value && typeof value === "object" && !Array.isArray(value)) {
      const error = (value as { error?: unknown }).error;
      if (error && typeof error === "object" && !Array.isArray(error) && typeof (error as { code?: unknown }).code === "string") {
        return (error as { code: string }).code;
      }
    }
  } catch { /* HTTP status remains the stable fallback. */ }
  return `RPG_RUNTIME_GATE_HTTP_${response.status}`;
}

export function validateRpgPosition(value: RpgPosition) {
  const coordinates = [value.mapId, value.playerX, value.playerY, value.fixtureState];
  return coordinates.every((item) => Number.isSafeInteger(item) && item >= -2_147_483_648 && item <= 2_147_483_647) &&
    value.mapId > 0 && value.playerX >= 0 && value.playerY >= 0;
}

export function sameRpgPosition(left: RpgPosition, right: RpgPosition) {
  return left.mapId === right.mapId && left.playerX === right.playerX && left.playerY === right.playerY &&
    left.fixtureState === right.fixtureState;
}

export function rpgEngineProfile(generation: components["schemas"]["RpgGeneration"]) {
  const profiles = {
    RPG2000: "rpg2k",
    RPG2003: "rpg2k3",
    RPGXP: "rgss1",
    RPGVX: "rgss2",
    RPGVXACE: "rgss3",
    RPGMV: "mv-v1",
    RPGMZ: "mz-v1",
  } as const;
  return profiles[generation];
}
