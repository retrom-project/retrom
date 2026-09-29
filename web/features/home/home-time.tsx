"use client";

import { formatTime } from "@/lib/backend";
import { useBrowserTimeZone } from "@/lib/use-browser-time-zone";

export function HomeTime({ value }: { value: number }) {
  const timeZone = useBrowserTimeZone();
  return <time dateTime={new Date(value).toISOString()}>{formatTime(value, timeZone)}</time>;
}
