import { act } from "react";
import { hydrateRoot, type Root } from "react-dom/client";
import { renderToString } from "react-dom/server";
import { afterEach, describe, expect, it } from "vitest";
import { formatTime } from "./backend";
import { useBrowserTimeZone } from "./use-browser-time-zone";

function TimeZoneProbe({ value }: { value: number }) {
  const timeZone = useBrowserTimeZone();
  return <time dateTime={new Date(value).toISOString()}>{formatTime(value, timeZone)}</time>;
}

describe("useBrowserTimeZone", () => {
  const originalTimeZone = process.env.TZ;

  afterEach(() => {
    process.env.TZ = originalTimeZone;
    document.body.replaceChildren();
  });

  it.each(["Asia/Shanghai", "America/New_York"])(
    "hydrates the UTC server snapshot before switching to %s",
    async (browserTimeZone) => {
      const value = Date.parse("2026-09-02T12:43:00.000Z");
      process.env.TZ = "UTC";
      const container = document.createElement("div");
      container.innerHTML = renderToString(<TimeZoneProbe value={value} />);
      document.body.append(container);
      expect(container).toHaveTextContent("2026年9月2日 12:43");

      process.env.TZ = browserTimeZone;
      const recoverableErrors: unknown[] = [];
      let root: Root | undefined;
      await act(async () => {
        root = hydrateRoot(container, <TimeZoneProbe value={value} />, {
          onRecoverableError: (error) => recoverableErrors.push(error),
        });
      });

      const expected = browserTimeZone === "Asia/Shanghai" ? "2026年9月2日 20:43" : "2026年9月2日 08:43";
      expect(container).toHaveTextContent(expected);
      expect(recoverableErrors).toEqual([]);
      await act(async () => root?.unmount());
    },
  );
});
