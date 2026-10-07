"use client";
import { useEffect, useEffectEvent, type RefObject } from "react";
import type { PlayerRuntimeV1, RuntimeHostShortcutV1, RuntimeStateV1 } from "./runtime/contract";

type Options = {
  runtime: RefObject<PlayerRuntimeV1 | null>;
  state: RuntimeStateV1;
  immersive: boolean;
  suppressed: boolean;
  onMenu: () => void;
  onPause: () => void;
  onError: (message: string) => void;
};
export function useRuntimeShortcuts({ runtime, state, immersive, suppressed, onMenu, onPause, onError }: Options) {
  const receive = useEffectEvent((shortcut: RuntimeHostShortcutV1) => {
    if (suppressed || !runtime.current?.getInputCapabilities().hostShortcuts.includes(shortcut)) { return; }
    if (shortcut === "MENU") { onMenu(); }
    else if (!immersive) { onPause(); }
  });
  const report = useEffectEvent(onError);
  useEffect(() => {
    const instance = runtime.current;
    if (!instance || !ready(state)) { return; }
    return instance.subscribe((event) => {
      if (event.type === "HOST_SHORTCUT" && ready(instance.getState())) { receive(event.shortcut); }
    });
  }, [runtime, state]);
  useEffect(() => {
    const instance = runtime.current;
    if (!instance || !ready(state) || !ready(instance.getState())) { return; }
    const allowed = instance.getInputCapabilities().hostShortcuts;
    const policy = suppressed ? null : {
      menu: allowed.includes("MENU") ? immersive ? "KeyM" as const : "Escape" as const : null,
      pause: !immersive && allowed.includes("PAUSE"),
    };
    void instance.setHostShortcutPolicy(policy).catch((failure: unknown) => {
      if (runtime.current === instance && ready(instance.getState())) { report(failure instanceof Error ? failure.message : "无法应用游戏快捷键。"); }
    });
  }, [runtime, state, immersive, suppressed]);
}
function ready(state: RuntimeStateV1) { return state === "RUNNING" || state === "PAUSED"; }
