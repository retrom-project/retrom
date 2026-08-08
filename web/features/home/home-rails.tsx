"use client";

import Link from "next/link";
import { useEffect, useMemo, useRef, useSyncExternalStore, type ReactNode } from "react";

const pinnedPlatformsKey = "retrom:pinned-home-platforms";

export type HomePlatform = {
  id: string;
  name: string;
  gameCount: number;
  playCount: number;
};

export function HorizontalRail({ children, className = "", label }: { children: ReactNode; className?: string; label: string }) {
  const rail = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const element = rail.current;
    if (!element) return;
    const scrollHorizontally = (event: WheelEvent) => {
      if (element.scrollWidth <= element.clientWidth || Math.abs(event.deltaY) <= Math.abs(event.deltaX)) return;
      const previous = element.scrollLeft;
      element.scrollLeft += event.deltaY;
      if (element.scrollLeft !== previous) event.preventDefault();
    };
    element.addEventListener("wheel", scrollHorizontally, { passive: false });
    return () => element.removeEventListener("wheel", scrollHorizontally);
  }, []);

  return <div ref={rail} className={`home-horizontal-rail ${className}`.trim()} aria-label={label} tabIndex={0}>{children}</div>;
}

function readPinnedPlatforms(value: string) {
  try {
    const parsed: unknown = JSON.parse(value);
    return Array.isArray(parsed) ? parsed.filter((item): item is string => typeof item === "string") : [];
  } catch {
    return [];
  }
}

function subscribeToPinnedPlatforms(onChange: () => void) {
  const storageChanged = (event: StorageEvent) => {
    if (event.key === pinnedPlatformsKey) onChange();
  };
  window.addEventListener("storage", storageChanged);
  window.addEventListener("retrom:pinned-platforms-change", onChange);
  return () => {
    window.removeEventListener("storage", storageChanged);
    window.removeEventListener("retrom:pinned-platforms-change", onChange);
  };
}

function platformCode(id: string) {
  if (id === "arcade") return "ARC";
  return id.replaceAll(/[^a-z0-9]/gi, "").slice(0, 4).toUpperCase();
}

export function PlatformRail({ platforms }: { platforms: HomePlatform[] }) {
  const pinnedSnapshot = useSyncExternalStore(
    subscribeToPinnedPlatforms,
    () => localStorage.getItem(pinnedPlatformsKey) ?? "[]",
    () => "[]",
  );
  const pinned = useMemo(() => readPinnedPlatforms(pinnedSnapshot), [pinnedSnapshot]);

  const ordered = useMemo(() => {
    const positions = new Map(pinned.map((id, index) => [id, index]));
    return [...platforms].sort((left, right) => {
      const leftPosition = positions.get(left.id);
      const rightPosition = positions.get(right.id);
      if (leftPosition !== undefined || rightPosition !== undefined) {
        if (leftPosition === undefined) return 1;
        if (rightPosition === undefined) return -1;
        return leftPosition - rightPosition;
      }
      return left.name.localeCompare(right.name, "zh-CN") || left.id.localeCompare(right.id);
    });
  }, [pinned, platforms]);

  function togglePinned(id: string) {
    const next = pinned.includes(id) ? pinned.filter((item) => item !== id) : [id, ...pinned];
    localStorage.setItem(pinnedPlatformsKey, JSON.stringify(next));
    window.dispatchEvent(new Event("retrom:pinned-platforms-change"));
  }

  return <HorizontalRail className="home-platform-rail" label="支持的平台">
    {ordered.map((platform) => {
      const isPinned = pinned.includes(platform.id);
      return <article className={`home-platform-card${isPinned ? " is-pinned" : ""}`} key={platform.id}>
        <Link href={`/library?platformId=${encodeURIComponent(platform.id)}`}>
          <span><strong>{platform.name}</strong><small>{platform.gameCount} 款游戏</small></span>
          <code>{platformCode(platform.id)}</code>
        </Link>
        <button type="button" className="home-platform-pin" aria-label={`${isPinned ? "取消置顶" : "置顶"}“${platform.name}”`} aria-pressed={isPinned} title={isPinned ? "取消置顶" : "置顶平台"} onClick={() => togglePinned(platform.id)}><span aria-hidden="true">📌</span></button>
      </article>;
    })}
  </HorizontalRail>;
}
