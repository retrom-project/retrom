import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ImmersiveHomeEntry } from "./immersive-home-entry";

const mocks = vi.hoisted(() => ({
  context: { user: { userId: "user-1" } as { userId: string } | null },
  push: vi.fn(),
  requestFullscreen: vi.fn(),
}));

vi.mock("next/navigation", () => ({ useRouter: () => ({ push: mocks.push }) }));
vi.mock("@/features/auth/auth-provider", () => ({ useAuth: () => ({ context: mocks.context }) }));
vi.mock("@/features/immersive/immersive-fullscreen", () => ({ requestImmersiveFullscreen: mocks.requestFullscreen }));

afterEach(() => {
  cleanup();
  mocks.context.user = { userId: "user-1" };
  mocks.push.mockReset();
  mocks.requestFullscreen.mockReset();
});

describe("home immersive entry", () => {
  it("requests fullscreen inside the click and immediately enters immersive mode", async () => {
    mocks.requestFullscreen.mockResolvedValue(false);
    const user = userEvent.setup();
    render(<ImmersiveHomeEntry />);
    await user.click(screen.getByRole("button", { name: "进入沉浸模式" }));
    expect(mocks.requestFullscreen).toHaveBeenCalledOnce();
    expect(mocks.push).toHaveBeenCalledWith("/immersive");
    expect(mocks.requestFullscreen.mock.invocationCallOrder[0]).toBeLessThan(mocks.push.mock.invocationCallOrder[0]);
  });

  it("supports keyboard activation and prevents duplicate navigation", async () => {
    mocks.requestFullscreen.mockResolvedValue(true);
    const user = userEvent.setup();
    render(<ImmersiveHomeEntry />);
    await user.tab();
    await user.keyboard("{Enter}{Enter}");
    expect(mocks.requestFullscreen).toHaveBeenCalledOnce();
    expect(mocks.push).toHaveBeenCalledOnce();
  });

  it("does not render for an unauthenticated visitor", () => {
    mocks.context.user = null;
    render(<ImmersiveHomeEntry />);
    expect(screen.queryByRole("button", { name: "进入沉浸模式" })).not.toBeInTheDocument();
  });
});
