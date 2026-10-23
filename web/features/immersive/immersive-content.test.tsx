import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { ImmersiveGames } from "./immersive-content";
import { useSaveSelection } from "./use-save-selection";
import { saveFixture } from "./test-fixtures";
import type { Save } from "@/lib/api/types";

const mocks = vi.hoisted(() => ({ GET: vi.fn(), POST: vi.fn(), replace: vi.fn(), notify: vi.fn(), clear: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => ({ replace: mocks.replace }) }));
vi.mock("@/components/toast-provider", () => ({ useToast: () => ({ notify: mocks.notify, clear: mocks.clear }) }));
vi.mock("@/lib/api/client", async (original) => ({ ...await original<object>(), api: { GET: mocks.GET, POST: mocks.POST } }));
const scrollDescriptor = Object.getOwnPropertyDescriptor(Element.prototype, "scrollIntoView");
afterEach(() => { cleanup(); vi.resetAllMocks(); if (scrollDescriptor) { Object.defineProperty(Element.prototype, "scrollIntoView", scrollDescriptor); } else { Reflect.deleteProperty(Element.prototype, "scrollIntoView"); } });
const idle = { data: null, loading: false, error: "", reload: () => {}, loadingMore: false, moreError: "", hasMore: false, loadMore: () => {}, base: 0, hasBefore: false, loadingBefore: false, loadBefore: async () => 0, moveTo: async () => 0 };
function Harness({ save }: { save: Save }) {
  const entry = { game: save.game, save };
  const selection = useSaveSelection(entry, true, { entryId: "", saveId: "" });
  return <ImmersiveGames entries={[entry]} resource={idle} total={1} title="我的存档" saveView saves={selection} selected={0} onSelect={() => {}} onLaunch={() => {}} returnTo="/immersive" />;
}
function response(save: Save) { return { data: { items: [save], offset: 0, total: 1 }, response: new Response() }; }

it("keeps screenshot and thumbnail elements mounted across game switches and blocks launch until the current request succeeds", async () => {
  Object.defineProperty(Element.prototype, "scrollIntoView", { configurable: true, value: vi.fn() });
  const first = { ...saveFixture("first", "first-game"), screenshotUrl: "/first.png" };
  const next = { ...saveFixture("next", "next-game"), screenshotUrl: "/next.png" };
  next.game.media = [{ id: "cover", kind: "cover", url: "/cover.png", mediaType: "image/png", ordinal: 0 }, { id: "video", kind: "video", url: "/video.mp4", mediaType: "video/mp4", ordinal: 0 }];
  let finish!: (value: ReturnType<typeof response>) => void;
  mocks.GET.mockResolvedValueOnce(response(first)).mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
  const view = render(<Harness save={first} />);
  await waitFor(() => expect(screen.getByRole("button", { name: "从存档继续" })).toBeEnabled());
  const preview = screen.getByRole("img", { name: first.game.title });
  view.rerender(<Harness save={next} />);
  expect(screen.getByRole("img", { name: next.game.title })).toBe(preview);
  expect(preview).toHaveAttribute("src", "/next.png");
  expect(view.container.querySelector("video")).toBeNull();
  const thumbnail = screen.getByRole("button", { name: "第 1 份存档，进度 next，4.7MB" });
  expect(thumbnail).toBeDisabled();
  expect(screen.getByRole("button", { name: "从存档继续" })).toBeDisabled();
  await act(async () => finish(response(next)));
  expect(screen.getByRole("img", { name: next.game.title })).toBe(preview);
  expect(screen.getByRole("button", { name: "第 1 份存档，进度 next，4.7MB" })).toBe(thumbnail);
  expect(screen.getByRole("button", { name: "从存档继续" })).toBeEnabled();
  expect(screen.queryByRole("button", { name: "下一页" })).not.toBeInTheDocument();
  expect(mocks.POST).not.toHaveBeenCalled();
  expect(Element.prototype.scrollIntoView).not.toHaveBeenCalled();
});
