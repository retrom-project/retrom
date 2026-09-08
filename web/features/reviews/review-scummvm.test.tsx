import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { ScummVMSelection, type ScummVMReview } from "./review-scummvm";

afterEach(cleanup);
const value: ScummVMReview = {
  kind: "SCUMMVM", selectedCandidateId: "", detection: { candidates: [
    { id: "one", root: "First", engineId: "sky", gameId: "sky", description: "First game", language: "en", platform: "pc", extra: "Floppy", blocker: "" },
    { id: "two", root: "Second", engineId: "sky", gameId: "sky", description: "Second game", language: "fr", platform: "pc", extra: "CD", blocker: "" },
    { id: "unknown", root: "Unknown", engineId: "sky", gameId: "sky", description: "Unknown game", language: "en", platform: "pc", extra: "", blocker: "UNKNOWN_VARIANT" },
  ] },
};
it("keeps ambiguous roots unselected and exposes each original language and variant", async () => {
  const onChange = vi.fn(); const user = userEvent.setup();
  render(<ScummVMSelection value={value} onChange={onChange} disabled={false} />);
  expect(screen.getByRole("combobox", {name: "运行版本"})).toHaveValue("");
  expect(onChange).not.toHaveBeenCalled();
  expect(screen.getByRole("option", {name: /First game.*First.*en.*Floppy/})).toBeEnabled();
  expect(screen.getByRole("option", {name: /Unknown game/})).toBeDisabled();
  await user.selectOptions(screen.getByRole("combobox", {name: "运行版本"}), "two");
  expect(onChange).toHaveBeenCalledWith("two");
});
it("shows an explicit no-match message without inventing a candidate", () => {
  render(<ScummVMSelection value={{...value, detection: {candidates: []}}} onChange={vi.fn()} disabled={false} />);
  expect(screen.getByText(/没有识别到可运行的游戏/)).toBeVisible();
  expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
});
