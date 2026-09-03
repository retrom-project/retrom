import { act } from "react";
import { hydrateRoot, type Root } from "react-dom/client";
import { renderToString } from "react-dom/server";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ImmersiveSaveCarousel } from "./save-carousel";

describe("ImmersiveSaveCarousel", () => {
  const originalTimeZone = process.env.TZ;

  afterEach(() => {
    process.env.TZ = originalTimeZone;
    document.body.replaceChildren();
  });

  it("hydrates with UTC before showing the browser-local save time", async () => {
    Element.prototype.scrollIntoView = vi.fn();
    const save = {
      saveStateId: "01a00000-0000-7000-8000-000000000001",
      name: "测试存档",
      createdAtMs: Date.parse("2026-09-02T12:43:00.000Z"),
      sizeBytes: 1024,
      discIndex: null,
      screenshotUrl: null,
    };
    process.env.TZ = "UTC";
    const container = document.createElement("div");
    container.innerHTML = renderToString(<ImmersiveSaveCarousel gameTitle="测试游戏" saves={[save]} selectedIndex={0} onSelect={() => undefined} />);
    document.body.append(container);
    expect(container).toHaveTextContent("12:43");

    process.env.TZ = "Asia/Shanghai";
    const recoverableErrors: unknown[] = [];
    let root: Root | undefined;
    await act(async () => {
      root = hydrateRoot(container, <ImmersiveSaveCarousel gameTitle="测试游戏" saves={[save]} selectedIndex={0} onSelect={() => undefined} />, {
        onRecoverableError: (error) => recoverableErrors.push(error),
      });
    });

    expect(container).toHaveTextContent("20:43");
    expect(recoverableErrors).toEqual([]);
    await act(async () => root?.unmount());
  });
});
