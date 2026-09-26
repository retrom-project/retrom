import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { HomeFeaturedMedia } from "./home-featured-media";
import { platformArt } from "./platform-art";

const props = { screenshotUrl: "/save.png", coverUrl: "/cover.png", platformId: "snes", title: "冒险" };
let desktop = true;
const subscribers = new Set<() => void>();
beforeEach(() => {
  desktop = true;
  vi.stubGlobal("matchMedia", vi.fn(() => ({ matches: desktop, addEventListener: (_: string, listener: () => void) => subscribers.add(listener), removeEventListener: (_: string, listener: () => void) => subscribers.delete(listener) })));
});
afterEach(() => {cleanup(); subscribers.clear(); vi.unstubAllGlobals();});

async function load(image: HTMLImageElement, width: number, height: number) {
  Object.defineProperties(image, { naturalWidth: { configurable: true, value: width }, naturalHeight: { configurable: true, value: height } });
  await act(async () => {fireEvent.load(image);});
}

describe("home desktop media", () => {
  it("shows a landscape session screenshot with its correct label", async () => {
    render(<HomeFeaturedMedia {...props} />);
    const image = screen.getByAltText("冒险 上次存档截图") as HTMLImageElement;
    await load(image, 240, 160);
    expect(image).toHaveClass("is-ready");
    expect(screen.getByText("上次存档画面")).toBeInTheDocument();
  });

  it("skips portrait and square game images and uses only local platform artwork", async () => {
    const { container } = render(<HomeFeaturedMedia {...props} />);
    await load(container.querySelector("img")!, 160, 240);
    const cover = screen.getByAltText("冒险 游戏图片") as HTMLImageElement;
    expect(new URL(cover!.getAttribute("src")!, window.location.href).pathname).toBe("/cover.png");
    await load(cover, 300, 300);
    const platform = container.querySelector("img")!;
    expect(new URL(platform!.getAttribute("src")!, window.location.href).pathname).toBe("/images/platforms/snes.svg");
    expect(screen.queryByText("上次存档画面")).not.toBeInTheDocument();
    fireEvent.error(platform);
    expect(container.querySelector("img")).toBeNull();
    expect(container.querySelector(".home-featured-media")).toHaveAttribute("data-kind", "empty");
  });

  it("uses a landscape cover after screenshot failure without calling it a save preview", async () => {
    const { container } = render(<HomeFeaturedMedia {...props} />);
    fireEvent.error(container.querySelector("img")!);
    const image = container.querySelector("img")!;
    await load(image, 800, 400);
    expect(image).toHaveClass("is-ready");
    expect(screen.queryByText("上次存档画面")).not.toBeInTheDocument();
  });

  it("does not mount or request hero images on smaller screens and responds to resizing", () => {
    desktop = false;
    const { container } = render(<HomeFeaturedMedia {...props} />);
    expect(container.querySelector("img")).toBeNull();
    act(() => {desktop = true; subscribers.forEach((listener) => listener());});
    expect(new URL(container.querySelector("img")!.getAttribute("src")!, window.location.href).pathname).toBe("/save.png");
    act(() => {desktop = false; subscribers.forEach((listener) => listener());});
    expect(container.querySelector(".home-featured-media")).toBeNull();
  });

  it("leaves unsupported platforms blank and resets failures for a different game", () => {
    const { container, rerender } = render(<HomeFeaturedMedia {...props} screenshotUrl={null} coverUrl={null} platformId="unknown" />);
    expect(container.querySelector("img")).toBeNull();
    expect(platformArt("constructor")).toBeNull();
    rerender(<HomeFeaturedMedia {...props} />);
    fireEvent.error(container.querySelector("img")!);
    rerender(<HomeFeaturedMedia {...props} screenshotUrl="/another-save.png" />);
    expect(new URL(container.querySelector("img")!.getAttribute("src")!, window.location.href).pathname).toBe("/another-save.png");
  });
});
