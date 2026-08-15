import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it } from "vitest";
import { useState } from "react";
import { TagChips, TagPicker, type TagReference } from "./tag-picker";

afterEach(cleanup);

const options: TagReference[] = [
  { tagId: "tag-action", name: "动作" },
  { tagId: "tag-coop", name: "双人合作" },
  { tagId: "tag-finished", name: "已通关" },
];

function PickerHarness({ initial = [] }: { initial?: TagReference[] }) {
  const [selected, setSelected] = useState(initial);
  return <TagPicker label="游戏标签" options={options} selected={selected} onChange={setSelected} />;
}

describe("TagPicker", () => {
  it("searches, selects and removes an existing tag with the keyboard", async () => {
    const user = userEvent.setup();
    render(<PickerHarness />);
    const input = screen.getByRole("combobox", { name: "游戏标签" });

    await user.type(input, "合作");
    expect(screen.getByRole("option", { name: "双人合作" })).toHaveAttribute("aria-selected", "true");
    await user.keyboard("{Enter}");
    expect(screen.getByText("双人合作")).toBeVisible();
    expect(screen.getByText("已选择 1/20 个标签")).toBeVisible();
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
    expect(input).toHaveFocus();

    await user.click(screen.getByRole("button", { name: "移除标签“双人合作”" }));
    expect(screen.queryByRole("button", { name: "移除标签“双人合作”" })).not.toBeInTheDocument();
    await waitFor(() => expect(input).toHaveFocus());
  });

  it("explains the empty taxonomy and enforces the twenty-tag limit", async () => {
    const user = userEvent.setup();
    const twenty = Array.from({ length: 20 }, (_, index) => ({ tagId: `tag-${index}`, name: `标签 ${index + 1}` }));
    const { rerender } = render(<TagPicker options={[]} selected={[]} onChange={() => undefined} />);
    await user.click(screen.getByRole("combobox", { name: "标签" }));
    expect(screen.getByRole("link", { name: "前往标签管理" })).toHaveAttribute("href", "/admin/tags");

    rerender(<TagPicker options={twenty} selected={twenty} onChange={() => undefined} />);
    expect(screen.getByRole("combobox", { name: "标签" })).toBeDisabled();
    expect(screen.getByText(/已选择 20\/20 个标签，已达到上限/)).toBeVisible();
  });
});

describe("TagChips", () => {
  it("limits dense projections and links exact tag filters", () => {
    render(<TagChips tags={options} limit={2} linked />);
    expect(screen.getByRole("link", { name: "查看标签“动作”下的游戏" })).toHaveAttribute("href", "/library?tagId=tag-action");
    expect(screen.getByLabelText("另有 1 个标签")).toHaveTextContent("+1");
    expect(screen.queryByText("已通关")).not.toBeInTheDocument();
  });
});
