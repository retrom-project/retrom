"use client";
import { useSyncExternalStore } from "react";
import { AppIcon } from "@/components/app-icon";

function subscribe(listener: () => void) {
  document.addEventListener("fullscreenchange", listener);
  return () => document.removeEventListener("fullscreenchange", listener);
}

export function PlayerFullscreenControl({
  onError,
  menu = false,
}: {
  onError: (message: string) => void;
  menu?: boolean;
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
      className={menu ? undefined : "player-control is-icon"}
      role={menu ? "menuitem" : undefined}
      aria-label={menu ? active ? "在更多操作中退出全屏" : "在更多操作中进入全屏" : active ? "退出全屏" : "全屏"}
      disabled={!enabled}
      onClick={() => void toggle()}
    >
      <AppIcon name={active ? "minimize" : "maximize"} />
      {menu ? <span>{active ? "退出全屏" : "进入全屏"}</span> : null}
    </button>
  );
}
