import { describe, expect, it, vi } from "vitest";
import {
  RpgValidationGateClient,
  originalValidationEventCount,
  rpgEngineProfile,
  sameRpgPosition,
  validateRpgPosition,
} from "./rpg-validation-protocol";

describe("RpgValidationGateClient", () => {
  it("replays the exact event after a transient network failure", async () => {
    const requests: string[] = [];
    const fetchGate = vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
      requests.push(String(init?.body));
      if (requests.length === 1) {throw new TypeError("network unavailable");}
      const request = JSON.parse(String(init?.body)) as { sequence: number; eventId: string };
      return Response.json({
        sequence: request.sequence,
        eventId: request.eventId,
        validationState: "STARTING",
        idempotentReplay: true,
      });
    });
    const client = new RpgValidationGateClient(crypto.randomUUID(), 0, new AbortController().signal, fetchGate);

    await client.begin("RUNTIME_READY");

    expect(requests).toHaveLength(2);
    expect(requests[1]).toBe(requests[0]);
    expect(JSON.parse(requests[0])).toMatchObject({ sequence: 1, gate: "RUNTIME_READY", phase: "BEGIN", evidence: {} });
  });

  it("starts a restore Launch after all eighteen original events", async () => {
    const fetchGate = vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
      const request = JSON.parse(String(init?.body)) as { sequence: number; eventId: string };
      return Response.json({
        sequence: request.sequence,
        eventId: request.eventId,
        validationState: "CHECKPOINTED",
        idempotentReplay: false,
      });
    });
    const client = new RpgValidationGateClient(
      crypto.randomUUID(), originalValidationEventCount, new AbortController().signal, fetchGate,
    );

    await client.begin("RESTORE_STARTED");

    const request = JSON.parse(String(fetchGate.mock.calls[0]?.[1]?.body)) as { sequence: number };
    expect(request.sequence).toBe(originalValidationEventCount + 1);
  });
});

describe("RPG validation evidence", () => {
  it("validates exact position fields and compares all four of them", () => {
    const saved = { mapId: 1, playerX: 2, playerY: 3, fixtureState: 4 };
    expect(validateRpgPosition(saved)).toBe(true);
    expect(validateRpgPosition({ ...saved, mapId: 0 })).toBe(false);
    expect(sameRpgPosition(saved, { ...saved })).toBe(true);
    expect(sameRpgPosition(saved, { ...saved, fixtureState: 5 })).toBe(false);
  });

  it("locks every generation to its expected engine profile", () => {
    expect([
      rpgEngineProfile("RPG2000"), rpgEngineProfile("RPG2003"), rpgEngineProfile("RPGXP"),
      rpgEngineProfile("RPGVX"), rpgEngineProfile("RPGVXACE"), rpgEngineProfile("RPGMV"),
      rpgEngineProfile("RPGMZ"),
    ]).toEqual(["rpg2k", "rpg2k3", "rgss1", "rgss2", "rgss3", "mv-v1", "mz-v1"]);
  });
});
