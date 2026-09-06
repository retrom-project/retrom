import {cleanup, render, screen, waitFor} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {afterEach, beforeEach, expect, it, vi} from "vitest";
import type {GameSaveDraft} from "./game-save-draft-store";
import {LocalGameSaveNotice} from "./local-game-save-notice";

const state = vi.hoisted(() => ({userId: "owner", list: vi.fn(), remove: vi.fn(), upload: vi.fn(), active: vi.fn()}));
vi.mock("@/features/auth/auth-provider", () => ({useAuth: () => ({context: {user: {userId: state.userId}}})}));
vi.mock("./game-save-draft-store", () => ({listGameSaveDrafts: state.list, deleteGameSaveDraft: state.remove}));
vi.mock("./game-save-draft-lease", () => ({draftIsActive: state.active}));
vi.mock("./local-game-save-upload", () => ({uploadLocalGameSave: state.upload}));

const draft: GameSaveDraft = {userId: "owner", launchId: "old-launch", title: "测试游戏", restored: true, updatedAtMs: 1000,
  payload: {checkpoint: {bytes: Uint8Array.of(1), format: "native-v1", metadata: null}, screenshot: new Blob(["image"], {type: "image/png"}),
    source: "GAME_SAVE", requestId: "saved-request", name: "固定名称"}};

beforeEach(() => {
  vi.resetAllMocks(); state.userId = "owner"; state.list.mockResolvedValue([draft]);
  state.active.mockReturnValue(false); state.remove.mockResolvedValue(undefined); state.upload.mockResolvedValue(undefined);
});
afterEach(cleanup);

it("only offers recovery and waits for explicit confirmation before upload", async () => {
  const user = userEvent.setup(); render(<LocalGameSaveNotice pathname="/games/example" />);
  await user.click(await screen.findByRole("button", {name: "处理本地草稿"}));
  expect(screen.getByRole("alertdialog")).toHaveTextContent("不是关闭页面时的即时进度");
  expect(state.upload).not.toHaveBeenCalled();
  state.list.mockResolvedValue([]);
  await user.click(screen.getByRole("button", {name: "保存到服务端"}));
  expect(state.upload).toHaveBeenCalledWith(draft);
  expect(state.remove).toHaveBeenCalledWith("owner", "old-launch", "saved-request");
  await waitFor(() => expect(screen.queryByRole("complementary")).not.toBeInTheDocument());
});

it("keeps the draft and confirmation open when the server rejects a stale save", async () => {
  state.upload.mockRejectedValue(new Error("原存档已更新，草稿仍保留"));
  const user = userEvent.setup(); render(<LocalGameSaveNotice pathname="/" />);
  await user.click(await screen.findByText("处理本地草稿"));
  await user.click(screen.getByText("保存到服务端"));
  expect(await screen.findByRole("alert")).toHaveTextContent("草稿仍保留");
  expect(state.remove).not.toHaveBeenCalled();
});

it("discards only the explicitly selected snapshot without uploading", async () => {
  const user = userEvent.setup(); render(<LocalGameSaveNotice pathname="/" />);
  await user.click(await screen.findByText("处理本地草稿")); state.list.mockResolvedValue([]);
  await user.click(screen.getByText("丢弃草稿"));
  expect(state.remove).toHaveBeenCalledWith("owner", "old-launch", "saved-request");
  expect(state.upload).not.toHaveBeenCalled();
});

it("hides an old account's draft immediately after switching accounts", async () => {
  const view = render(<LocalGameSaveNotice pathname="/" />);
  await screen.findByText("处理本地草稿"); state.userId = "other";
  view.rerender(<LocalGameSaveNotice pathname="/" />);
  expect(screen.queryByRole("complementary")).not.toBeInTheDocument();
  await waitFor(() => expect(state.list).toHaveBeenLastCalledWith("other"));
});

it("does not offer local drafts in the player or from a live game session", async () => {
  const view = render(<LocalGameSaveNotice pathname="/play/new-launch" />);
  expect(state.list).not.toHaveBeenCalled();
  state.active.mockReturnValue(true); view.rerender(<LocalGameSaveNotice pathname="/" />);
  await waitFor(() => expect(state.active).toHaveBeenCalledWith("owner", "old-launch"));
  expect(screen.queryByRole("complementary")).not.toBeInTheDocument();
});
