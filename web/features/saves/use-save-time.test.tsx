import { act } from "react";
import { hydrateRoot, type Root } from "react-dom/client";
import { renderToString } from "react-dom/server";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useSaveTimeFormatter } from "./use-save-time";

function SaveTimeProbe({ nowMs, value }: { nowMs: number; value: number }) {
  const formatTime = useSaveTimeFormatter();
  return <time dateTime={new Date(value).toISOString()}>{formatTime(value, nowMs)}</time>;
}

function mockBrowserUtcOffset(hours: number) {
  const shifted = (date: Date) => new Date(date.getTime() + hours * 60 * 60 * 1_000);
  vi.spyOn(Date.prototype, "getFullYear").mockImplementation(function (this: Date) { return shifted(this).getUTCFullYear(); });
  vi.spyOn(Date.prototype, "getMonth").mockImplementation(function (this: Date) { return shifted(this).getUTCMonth(); });
  vi.spyOn(Date.prototype, "getDate").mockImplementation(function (this: Date) { return shifted(this).getUTCDate(); });
  vi.spyOn(Date.prototype, "getHours").mockImplementation(function (this: Date) { return shifted(this).getUTCHours(); });
  vi.spyOn(Date.prototype, "getMinutes").mockImplementation(function (this: Date) { return shifted(this).getUTCMinutes(); });
  vi.spyOn(Date.prototype, "getSeconds").mockImplementation(function (this: Date) { return shifted(this).getUTCSeconds(); });
}

describe("useSaveTimeFormatter", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    document.body.replaceChildren();
  });

  it("hydrates an UTC server snapshot before switching to the browser timezone", async () => {
    const value = Date.parse("2026-09-02T12:41:27.000Z");
    const nowMs = Date.parse("2026-09-02T13:00:00.000Z");
    const container = document.createElement("div");
    container.innerHTML = renderToString(<SaveTimeProbe value={value} nowMs={nowMs} />);
    document.body.append(container);
    expect(container).toHaveTextContent("今天 12:41:27");

    mockBrowserUtcOffset(8);
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => undefined);
    let root: Root | undefined;
    await act(async () => {
      root = hydrateRoot(container, <SaveTimeProbe value={value} nowMs={nowMs} />);
    });

    expect(container).toHaveTextContent("今天 20:41:27");
    expect(consoleError.mock.calls.flat().join("\n")).not.toMatch(/hydration|hydrated/i);
    await act(async () => root?.unmount());
  });
});
