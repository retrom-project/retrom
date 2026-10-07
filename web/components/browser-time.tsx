"use client";

import { useSyncExternalStore } from "react";
import { formatTime } from "@/lib/format-time";
import { useBrowserTimeZone } from "@/lib/use-browser-time-zone";

const subscribe = () => () => undefined;

export function BrowserTime({ value }: { value: number | null | undefined }) {
  const timeZone = useBrowserTimeZone();
  const hydrated = useSyncExternalStore(
    subscribe,
    () => true,
    () => false,
  );
  return (
    <time
      className="browser-time"
      dateTime={
        value === null || value === undefined
          ? undefined
          : new Date(value).toISOString()
      }
    >
      {hydrated ? formatTime(value, timeZone) : "—"}
    </time>
  );
}
