import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { RPGDependenciesCard } from "./review-rpg-dependencies";
import type { RPGMakerReview } from "./review-actions-model";

const review: RPGMakerReview = {
  selectedCoreId:"rpgmaker", generation:"RPG2000", evidenceGeneration:"RPG2000", evidenceConfidence:"MATCHED",
  selfContained:false, selfContainedOverride:false, externalRTPRequirements:[{slot:0,declaredName:"RPG2000_RTP"}],
};
afterEach(cleanup);
it("keeps explicit self-contained confirmation without fetching or selecting packs", async () => {
  const onChange=vi.fn();
  render(<RPGDependenciesCard value={review} disabled={false} onChange={onChange} />);
  expect(screen.getByText(/检测到外部 RTP/)).toBeInTheDocument();
  expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
  await userEvent.setup().click(screen.getByRole("checkbox",{name:"确认项目自包含 RTP"}));
  expect(onChange).toHaveBeenCalledWith({...review,selfContainedOverride:true});
});
it("allows a confirmation to be cleared and respects pending writes", async () => {
  const onChange=vi.fn();
  const {rerender}=render(<RPGDependenciesCard value={{...review,selfContainedOverride:true}} disabled={false} onChange={onChange} />);
  await userEvent.setup().click(screen.getByRole("checkbox"));
  expect(onChange).toHaveBeenCalledWith(review);
  rerender(<RPGDependenciesCard value={review} disabled onChange={onChange} />);
  expect(screen.getByRole("checkbox")).toBeDisabled();
});
it("does not offer RTP confirmation for a native web project", () => {
  render(<RPGDependenciesCard value={{...review,generation:"RPGMV",externalRTPRequirements:[]}} disabled={false} onChange={vi.fn()} />);
  expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
  expect(screen.getByText("未声明外部 RTP")).toBeInTheDocument();
});
