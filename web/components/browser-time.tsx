"use client";

import { useSyncExternalStore } from "react";
import { formatTime, type TimeFormat } from "@/lib/format-time";
import { useBrowserTimeZone } from "@/lib/use-browser-time-zone";

const subscribe = () => () => undefined;

export function BrowserTime({ value, format = "full" }: { value: number | null | undefined; format?: TimeFormat }) {
  const timeZone = useBrowserTimeZone();
  const hydrated = useSyncExternalStore(
    subscribe,
    () => true,
    () => false,
  );
  return (
    <time
      className="browser-time"
      data-format={format}
      title={format === "compact" && hydrated && value !== null && value !== undefined ? formatTime(value, timeZone) : undefined}
      dateTime={
        value === null || value === undefined
          ? undefined
          : new Date(value).toISOString()
      }
    >
      {hydrated ? formatTime(value, timeZone, format) : "—"}
    </time>
  );
}
