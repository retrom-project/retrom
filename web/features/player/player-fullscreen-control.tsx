"use client";
import { useSyncExternalStore } from "react";
import { AppIcon } from "@/components/app-icon";

function subscribe(listener: () => void) {
  document.addEventListener("fullscreenchange", listener);
  return () => document.removeEventListener("fullscreenchange", listener);
}

export function PlayerFullscreenControl({
  onError,
}: {
  onError: (message: string) => void;
}) {
  const active = useSyncExternalStore(
    subscribe,
    () => document.fullscreenElement !== null,
    () => false,
  );
  const enabled = useSyncExternalStore(
    subscribe,
    () => document.fullscreenEnabled,
    () => false,
  );
  async function toggle() {
    try {
      if (document.fullscreenElement) {
        await document.exitFullscreen();
      } else {
        await document.documentElement.requestFullscreen();
      }
    } catch {
      onError("浏览器暂时无法切换全屏。");
    }
  }
  return (
    <button
      className="player-control is-icon"
      aria-label={active ? "退出全屏" : "全屏"}
      disabled={!enabled}
      onClick={() => void toggle()}
    >
      <AppIcon name={active ? "minimize" : "maximize"} />
    </button>
  );
}
