import { act } from "react";
import { hydrateRoot, type Root } from "react-dom/client";
import { renderToString } from "react-dom/server";
import { afterEach, describe, expect, it } from "vitest";
import { BrowserTime } from "./browser-time";

describe("BrowserTime", () => {
  const originalTimeZone = process.env.TZ;
  afterEach(() => {
    process.env.TZ = originalTimeZone;
    document.body.replaceChildren();
  });

  it("preserves the compact layout variant through hydration and resets it for full dates", async () => {
    const value = Date.parse("2026-10-07T12:20:00Z");
    process.env.TZ = "UTC";
    const container = document.createElement("div");
    container.innerHTML = renderToString(<BrowserTime value={value} format="compact" />);
    document.body.append(container);
    expect(container.querySelector("time")).toHaveAttribute("data-format", "compact");
    expect(container).toHaveTextContent("—");
    const errors: unknown[] = [];
    let root: Root | undefined;
    await act(async () => {
      root = hydrateRoot(container, <BrowserTime value={value} format="compact" />, {
        onRecoverableError: (error) => errors.push(error),
      });
    });
    expect(container).toHaveTextContent("10/07 12:20");
    expect(container.querySelector("time")).toHaveAttribute("title", "2026年10月7日 12:20");
    expect(container.querySelector("time")).toHaveAttribute("data-format", "compact");
    await act(async () => root?.render(<BrowserTime value={value} />));
    expect(container).toHaveTextContent("2026年10月7日 12:20");
    expect(container.querySelector("time")).toHaveAttribute("data-format", "full");
    expect(container.querySelector("time")).not.toHaveAttribute("title");
    expect(errors).toEqual([]);
    await act(async () => root?.unmount());
  });

  it.each([
    ["Asia/Shanghai", "2026-10-01T19:35:00Z", "2026年10月2日 03:35"],
    ["UTC", "2026-10-01T19:35:00Z", "2026年10月1日 19:35"],
    ["America/Los_Angeles", "2026-10-01T19:35:00Z", "2026年10月1日 12:35"],
    ["America/Los_Angeles", "2026-03-08T09:59:00Z", "2026年3月8日 01:59"],
    ["America/Los_Angeles", "2026-03-08T10:00:00Z", "2026年3月8日 03:00"],
  ])(
    "hydrates in %s at %s without a server timezone flash",
    async (zone, instant, expected) => {
      const value = Date.parse(instant);
      process.env.TZ = "UTC";
      const container = document.createElement("div");
      container.innerHTML = renderToString(<BrowserTime value={value} />);
      document.body.append(container);
      expect(container).toHaveTextContent("—");
      process.env.TZ = zone;
      const errors: unknown[] = [];
      let root: Root | undefined;
      await act(async () => {
        root = hydrateRoot(container, <BrowserTime value={value} />, {
          onRecoverableError: (error) => errors.push(error),
        });
      });
      expect(container).toHaveTextContent(expected);
      expect(container.querySelector("time")).toHaveAttribute(
        "datetime",
        new Date(value).toISOString(),
      );
      expect(errors).toEqual([]);
      await act(async () => root?.unmount());
    },
  );
});
