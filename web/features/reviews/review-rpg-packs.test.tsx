import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { RPGMakerReview } from "./review-actions-model";
import { RPGPackControls } from "./review-rpg-packs";

const review: RPGMakerReview = {
  selectedCoreId: "rpgmaker_xp", generation: "RPGXP", evidenceGeneration: "RPGXP",
  evidenceConfidence: "MATCHED", selfContained: false, selfContainedOverride: false,
  runtimeBindingRevision: 1,
  runtimePackRequirements: [{ slot: 1, declaredName: "Standard", normalizedDeclaredName: "standard" }],
  runtimePackSelections: [], runtimeValidation: null, runtimeValidationCurrent: false,
};

afterEach(() => {cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals();});

describe("RPGPackControls", () => {
  it("selects one exact READY installation for the detected slot", async () => {
    const installationId = "01980000-0000-7000-8000-000000000020";
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({
      definitions: [{ definitionId: "rgss1_standard", kind: "RGSS1_RTP_STANDARD", generation: "RPGXP", declaredName: "Standard", normalizedDeclaredName: "standard", displayName: "RPG Maker XP RTP", requiredLayoutVersion: "mkxpz-v1", origin: "BUILTIN", enabled: true }],
      installations: [{ installationId, definitionId: "rgss1_standard", filesDigest: "a".repeat(64), fileCount: 100, totalBytes: 1024, bundleSha256: "b".repeat(64), status: "READY", diagnostics: [], sourceNote: null, references: { variantRevisionCount: 0, checkpointCount: 0 }, version: 1, createdAtMs: 1, validatedAtMs: 2, deletedAtMs: null }],
    }), { status: 200, headers: { "Content-Type": "application/json" } })));
    const onChange = vi.fn();
    const user = userEvent.setup();
    render(<RPGPackControls value={review} disabled={false} onChange={onChange} />);
    const select = await screen.findByRole("combobox", { name: /Slot 1 · Standard/ });
    await user.selectOptions(select, installationId);
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({
      runtimeValidationCurrent: false,
      runtimePackSelections: [{ slot: 1, declaredName: "Standard", installationId }],
    }));
  });

  it("allows only the 2000/2003 self-contained override and clears selections", async () => {
    const onChange = vi.fn();
    const user = userEvent.setup();
    render(<RPGPackControls value={{ ...review, generation: "RPG2000", runtimePackRequirements: [{ slot: 0, declaredName: "RPG2000_RTP", normalizedDeclaredName: "rpg2000_rtp" }], runtimePackSelections: [{ slot: 0, declaredName: "RPG2000_RTP", installationId: "01980000-0000-7000-8000-000000000021" }] }} disabled={false} onChange={onChange} />);
    await user.click(screen.getByRole("checkbox", { name: /确认项目自包含 RTP/ }));
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ selfContainedOverride: true, runtimePackSelections: [] }));
  });
});
