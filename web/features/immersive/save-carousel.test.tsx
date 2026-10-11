import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { ImmersiveSaveCarousel } from "./save-carousel";
import { useSaveSelection } from "./use-save-selection";
import { saveFixture } from "./test-fixtures";

const mocks = vi.hoisted(() => ({ GET: vi.fn(), POST: vi.fn(), replace: vi.fn(), notify: vi.fn(), clear: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => ({ replace: mocks.replace }) }));
vi.mock("@/components/toast-provider", () => ({ useToast: () => ({ notify: mocks.notify, clear: mocks.clear }) }));
vi.mock("@/lib/api/client", async (original) => ({ ...await original<object>(), api: { GET: mocks.GET, POST: mocks.POST } }));
const scrollDescriptor = Object.getOwnPropertyDescriptor(Element.prototype, "scrollIntoView");
afterEach(() => { cleanup(); vi.useRealTimers(); vi.resetAllMocks(); vi.restoreAllMocks(); if (scrollDescriptor) { Object.defineProperty(Element.prototype, "scrollIntoView", scrollDescriptor); } else { Reflect.deleteProperty(Element.prototype, "scrollIntoView"); } });

function Harness({ gameId = "game" }: { gameId?: string }) {
  const latest = saveFixture("latest", gameId);
  const selection = useSaveSelection({ game: latest.game, save: latest }, true, { entryId: "", saveId: "" });
  return <ImmersiveSaveCarousel gameTitle={latest.game.title} selection={selection} returnTo={`/immersive?view=saves&entry=latest&save=${selection.save?.id ?? ""}`} />;
}

it("does not flash a loading notice for a fast save read and retains the count slot", async () => {
  vi.useFakeTimers();
  const save = saveFixture("latest");
  let finish!: (value: unknown) => void;
  mocks.GET.mockImplementation(() => new Promise((resolve) => { finish = resolve; }));
  const view = render(<Harness />);
  expect(screen.queryByText("正在读取存档…")).not.toBeInTheDocument();
  const count = view.container.querySelector("header strong");
  expect(count).toHaveTextContent("— / —");
  expect(screen.getByRole("button", { name: "从存档继续" })).toBeDisabled();
  await act(async () => {
    await vi.advanceTimersByTimeAsync(100);
    finish({ data: { items: [save], offset: 0, total: 1 }, response: new Response() });
  });
  expect(view.container.querySelector("header strong")).toBe(count);
  expect(count).toHaveTextContent("1 / 1");
  expect(screen.getByRole("button", { name: "从存档继续" })).toBeEnabled();
  await act(async () => vi.advanceTimersByTimeAsync(1000));
  expect(screen.queryByText("正在读取存档…")).not.toBeInTheDocument();
});

it("shows feedback for slow reads and restarts the delay when the selected game changes", async () => {
  vi.useFakeTimers();
  mocks.GET.mockImplementation(() => new Promise(() => {}));
  const view = render(<Harness gameId="first" />);
  await act(async () => vi.advanceTimersByTimeAsync(400));
  expect(screen.queryByText("正在读取存档…")).not.toBeInTheDocument();
  view.rerender(<Harness gameId="second" />);
  await act(async () => vi.advanceTimersByTimeAsync(499));
  expect(screen.queryByText("正在读取存档…")).not.toBeInTheDocument();
  await act(async () => vi.advanceTimersByTimeAsync(1));
  expect(screen.getByText("正在读取存档…")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "从存档继续" })).toBeDisabled();
  view.rerender(<Harness gameId="third" />);
  expect(screen.queryByText("正在读取存档…")).not.toBeInTheDocument();
  view.unmount();
  await act(async () => vi.advanceTimersByTimeAsync(1000));
  expect(screen.queryByText("正在读取存档…")).not.toBeInTheDocument();
});

it("loads the selected older save with its frozen core and keeps blocked saves inspectable", async () => {
  const latest = saveFixture("latest");
  const older = { ...saveFixture("older"), extinfo: { ...latest.extinfo, coreId: "nestopia" } };
  const blocked = { ...saveFixture("blocked"), restorable: false, restoreReason: "core_changed" as const };
  mocks.GET.mockResolvedValue({ data: { items: [latest, older, blocked], offset: 0, total: 3 }, response: new Response() });
  mocks.POST.mockResolvedValue({ data: { id: "run" }, response: new Response() });
  Object.defineProperty(Element.prototype, "scrollIntoView", { configurable: true, value: vi.fn() });
  const user = userEvent.setup();
  render(<Harness />);
  await screen.findByRole("button", { name: "第 1 份存档，进度 latest，4.7MB" });
  await user.click(screen.getByRole("button", { name: "下一份存档" }));
  expect(screen.getByRole("button", { name: "第 2 份存档，进度 older，4.7MB" })).toHaveAttribute("aria-pressed", "true");
  expect(screen.getAllByLabelText("存档大小 4.7MB")).toHaveLength(3);
  await user.click(screen.getByRole("button", { name: "第 3 份存档，进度 blocked，4.7MB" }));
  expect(screen.getByRole("img", { name: "存档不可加载：运行核心已变化，无法恢复" })).toBeInTheDocument();
  expect(screen.queryByText("运行核心已变化，无法恢复")).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "从存档继续" })).toBeDisabled();
  expect(mocks.POST).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "上一份存档" }));
  await user.click(screen.getByRole("button", { name: "从存档继续" }));
  await waitFor(() => expect(mocks.POST).toHaveBeenCalledWith("/api/v1/runs", { body: { gameId: "game", coreId: "nestopia", saveId: "older", purpose: "play" } }));
  expect(mocks.replace).toHaveBeenCalledWith("/play/run?returnTo=%2Fimmersive%3Fview%3Dsaves%26entry%3Dlatest%26save%3Dolder");
});
