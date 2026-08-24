"use client";

import { useCallback, useEffect, useRef, useState, type Dispatch, type MutableRefObject, type SetStateAction } from "react";
import { getActiveImmersiveGamepadIndex, setActiveImmersiveGamepadIndex } from "@/features/immersive/active-gamepad";
import { setEmulatorPaused } from "./pause-control";
import {
  ImmersiveMenuInputReader,
  ImmersiveNeutralGate,
  gamepadButtonPressed,
  isNeutralGamepads,
  isStandardImmersiveGamepad,
} from "./immersive-controls";
import { ImmersiveGamepadFilter } from "./immersive-gamepad-filter";
import type { EmulatorInstance } from "./adapters/ejs-4.2.3-v2";
import {
  moveImmersiveMenuSelection,
  selectableImmersiveMenuItem,
  type ImmersiveMenuSelection,
} from "./immersive-player-menu-model";

export type ImmersivePlayerOverlay =
  | { kind: "closed" }
  | { kind: "closing" }
  | { kind: "menu"; error: string; notice: string; pending: boolean; selected: ImmersiveMenuSelection }
  | { kind: "reconnect"; ready: boolean };

type Params = {
  enabled: boolean;
  emulator: MutableRefObject<EmulatorInstance | undefined>;
  pausedRef: MutableRefObject<boolean>;
  running: boolean;
  setPaused: Dispatch<SetStateAction<boolean>>;
  exitStrict: () => Promise<void>;
  saveAvailable: boolean;
  saveGame: () => Promise<boolean>;
  onFatalError: (message: string) => void;
};

type PauseOwner = "menu" | "disconnect" | null;

export function useImmersivePlayer(params: Params) {
  const {
    enabled, emulator, pausedRef, running, setPaused, exitStrict, saveAvailable, saveGame, onFatalError,
  } = params;
  const [overlay, setOverlay] = useState<ImmersivePlayerOverlay>({ kind: "closed" });
  const overlayRef = useRef(overlay);
  const runningRef = useRef(running);
  const exitStrictRef = useRef(exitStrict);
  const pauseOwner = useRef<PauseOwner>(null);
  const reconnectTarget = useRef<"game" | "menu">("game");
  const activeIndex = useRef<number | null>(getActiveImmersiveGamepadIndex());
  const menuReader = useRef(new ImmersiveMenuInputReader());
  const closingGate = useRef(new ImmersiveNeutralGate());
  const reconnectGate = useRef(new ImmersiveNeutralGate());
  const previousPressed = useRef(new Map<number, boolean>());
  const previousReconnectA = useRef(false);
  const suspended = useRef(false);
  const [filter] = useState(() => new ImmersiveGamepadFilter({
    activeGamepadIndex: getActiveImmersiveGamepadIndex(),
    onMenuGesture: () => undefined,
  }));

  const updateOverlay = useCallback((next: ImmersivePlayerOverlay) => {
    overlayRef.current = next;
    setOverlay(next);
  }, []);

  const requestMenu = useCallback(() => {
    if (!enabled || !runningRef.current || overlayRef.current.kind !== "closed") {return;}
    filter.setBlocked(true);
    if (!pausedRef.current) {
      if (setEmulatorPaused(emulator.current, true)) {
        pauseOwner.current = "menu";
        pausedRef.current = true;
        setPaused(true);
      } else {
        onFatalError("PLAYER_IMMERSIVE_PAUSE_UNAVAILABLE");
        return;
      }
    }
    menuReader.current.reset();
    updateOverlay({ kind: "menu", error: "", notice: "", pending: false, selected: 0 });
  }, [emulator, enabled, filter, onFatalError, pausedRef, setPaused, updateOverlay]);

  useEffect(() => {
    filter.setOnMenuGesture(requestMenu);
    runningRef.current = running;
    exitStrictRef.current = exitStrict;
  }, [exitStrict, filter, requestMenu, running]);

  const beginClose = useCallback((owner: PauseOwner) => {
    if (overlayRef.current.kind === "closing") {return;}
    if (owner !== null && pauseOwner.current !== owner) {pauseOwner.current = null;}
    filter.setBlocked(true);
    closingGate.current.reset();
    updateOverlay({ kind: "closing" });
  }, [filter, updateOverlay]);

  const saveFromMenu = useCallback((current: Extract<ImmersivePlayerOverlay, { kind: "menu" }>) => {
    if (!saveAvailable) {
      updateOverlay({ ...current, error: "当前游戏无法创建可恢复存档。", notice: "" });
      return;
    }
    updateOverlay({ ...current, error: "", notice: "正在创建存档…", pending: true });
    void saveGame().then((saved) => {
      const latest = overlayRef.current;
      if (latest.kind !== "menu") {return;}
      updateOverlay(saved
        ? { ...latest, error: "", notice: "存档已创建。", pending: false }
        : { ...latest, error: "创建存档失败，请重试。", notice: "", pending: false });
    }).catch(() => {
      const latest = overlayRef.current;
      if (latest.kind === "menu") {
        updateOverlay({ ...latest, error: "创建存档失败，请重试。", notice: "", pending: false });
      }
    });
  }, [saveAvailable, saveGame, updateOverlay]);

  const runSelectedMenuAction = useCallback(() => {
    const current = overlayRef.current;
    if (current.kind !== "menu" || current.pending) {return;}
    if (current.selected === 0) {beginClose("menu"); return;}
    if (current.selected === 1) {saveFromMenu(current); return;}
    updateOverlay({ ...current, error: "", notice: "正在退出游戏…", pending: true });
    void exitStrictRef.current().catch(() => {
      const failed = overlayRef.current;
      if (failed.kind === "menu") {
        updateOverlay({ ...failed, error: "退出失败。按 A 重试，或按 B 继续游戏。", notice: "", pending: false });
      }
    });
  }, [beginClose, saveFromMenu, updateOverlay]);

  const menuCancel = useCallback(() => {
    const current = overlayRef.current;
    if (current.kind === "menu" && !current.pending) {beginClose("menu");}
  }, [beginClose]);
  const menuSelect = useCallback((selected: ImmersiveMenuSelection) => {
    const current = overlayRef.current;
    if (current.kind === "menu" && !current.pending && selectableImmersiveMenuItem(selected, saveAvailable)) {
      updateOverlay({ ...current, selected });
    }
  }, [saveAvailable, updateOverlay]);
  const menuMove = useCallback((direction: "left" | "right") => {
    const current = overlayRef.current;
    if (current.kind === "menu" && !current.pending) {
      updateOverlay({ ...current, selected: moveImmersiveMenuSelection(current.selected, direction, saveAvailable) });
    }
  }, [saveAvailable, updateOverlay]);

  useEffect(() => {
    if (!enabled) {return;}
    const keydown = (event: KeyboardEvent) => {
      if (overlayRef.current.kind === "closed" && event.key.toLowerCase() === "m") {
        event.preventDefault(); requestMenu(); return;
      }
      if (overlayRef.current.kind !== "menu") {return;}
      if (event.key === "Escape") {event.preventDefault(); menuCancel(); return;}
      if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
        event.preventDefault(); menuMove(event.key === "ArrowLeft" ? "left" : "right"); return;
      }
      if (event.key === "Enter") {event.preventDefault(); runSelectedMenuAction();}
    };
    window.addEventListener("keydown", keydown);
    return () => window.removeEventListener("keydown", keydown);
  }, [enabled, menuCancel, menuMove, requestMenu, runSelectedMenuAction]);

  useEffect(() => {
    if (!enabled) {return;}
    if (overlayRef.current.kind === "closed") {
      filter.reset();
      filter.setBlocked(!running);
    }
    if (!running) {return;}
    return startImmersivePoll({
      filter, activeIndex, overlayRef, pauseOwner, reconnectTarget, menuReader, closingGate,
      reconnectGate, previousPressed, previousReconnectA, suspended, emulator,
      pausedRef, setPaused, updateOverlay, beginClose,
      menuCancel, menuMove, runSelectedMenuAction, onFatalError,
    });
  }, [beginClose, emulator, enabled, filter, menuCancel, menuMove, onFatalError, pausedRef, runSelectedMenuAction, running, setPaused, updateOverlay]);

  useEffect(() => {
    if (!enabled) {return;}
    const suspend = () => {suspended.current = true; filter.setBlocked(true); filter.reset(); closingGate.current.reset();};
    const visibility = () => {if (document.hidden) {suspend();}};
    window.addEventListener("blur", suspend);
    document.addEventListener("visibilitychange", visibility);
    return () => {
      window.removeEventListener("blur", suspend);
      document.removeEventListener("visibilitychange", visibility);
      filter.setBlocked(true);
      filter.reset();
    };
  }, [enabled, filter]);

  return { filter, menuCancel, menuSelect, overlay, requestMenu, runSelectedMenuAction, saveAvailable };
}

type PollParams = {
  filter: ImmersiveGamepadFilter;
  activeIndex: MutableRefObject<number | null>;
  overlayRef: MutableRefObject<ImmersivePlayerOverlay>;
  pauseOwner: MutableRefObject<PauseOwner>;
  reconnectTarget: MutableRefObject<"game" | "menu">;
  menuReader: MutableRefObject<ImmersiveMenuInputReader>;
  closingGate: MutableRefObject<ImmersiveNeutralGate>;
  reconnectGate: MutableRefObject<ImmersiveNeutralGate>;
  previousPressed: MutableRefObject<Map<number, boolean>>;
  previousReconnectA: MutableRefObject<boolean>;
  suspended: MutableRefObject<boolean>;
  emulator: MutableRefObject<EmulatorInstance | undefined>;
  pausedRef: MutableRefObject<boolean>;
  setPaused: Dispatch<SetStateAction<boolean>>;
  updateOverlay: (next: ImmersivePlayerOverlay) => void;
  beginClose: (owner: PauseOwner) => void;
  menuCancel: () => void;
  menuMove: (direction: "left" | "right") => void;
  runSelectedMenuAction: () => void;
  onFatalError: (message: string) => void;
};

function startImmersivePoll(params: PollParams) {
  let frame = 0;
  const poll = (nowMs: number) => {
    const gamepads = typeof navigator.getGamepads === "function" ? Array.from(navigator.getGamepads()) : [];
    pollImmersiveState(params, gamepads, nowMs);
    frame = window.requestAnimationFrame(poll);
  };
  frame = window.requestAnimationFrame(poll);
  return () => window.cancelAnimationFrame(frame);
}

function pollImmersiveState(params: PollParams, gamepads: (Gamepad | null)[], nowMs: number) {
  claimFallbackGamepad(params, gamepads);
  if (params.suspended.current) {
    if (document.visibilityState === "visible" && document.hasFocus() && params.closingGate.current.update(isNeutralGamepads(gamepads), nowMs)) {
      params.suspended.current = false;
      if (params.overlayRef.current.kind === "closed") {params.filter.setBlocked(false);}
    }
    return;
  }
  const overlay = params.overlayRef.current;
  if (overlay.kind !== "reconnect" && params.activeIndex.current !== null &&
    !isStandardImmersiveGamepad(gamepads.find((gamepad) => gamepad?.index === params.activeIndex.current))) {
    enterReconnect(params, overlay.kind === "menu" ? "menu" : "game");
    return;
  }
  if (overlay.kind === "menu") {pollMenu(params, gamepads, nowMs);}
  if (overlay.kind === "closing") {pollClosing(params, gamepads, nowMs);}
  if (overlay.kind === "reconnect") {pollReconnect(params, gamepads, nowMs);}
}

function claimFallbackGamepad(params: PollParams, gamepads: (Gamepad | null)[]) {
  if (params.activeIndex.current !== null || params.overlayRef.current.kind === "reconnect") {return;}
  const candidate = gamepads.find(isStandardImmersiveGamepad);
  if (!candidate) {return;}
  params.activeIndex.current = candidate.index;
  params.filter.setActiveGamepadIndex(candidate.index);
  setActiveImmersiveGamepadIndex(candidate.index);
}

function pollMenu(params: PollParams, gamepads: (Gamepad | null)[], nowMs: number) {
  const action = params.menuReader.current.update(gamepads, params.activeIndex.current, nowMs);
  if (action === "cancel") {params.menuCancel(); return;}
  if (action === "confirm") {params.runSelectedMenuAction(); return;}
  if (action === "left" || action === "right") {params.menuMove(action);}
}

function pollClosing(params: PollParams, gamepads: (Gamepad | null)[], nowMs: number) {
  if (!params.closingGate.current.update(isNeutralGamepads(gamepads), nowMs)) {return;}
  if (params.pauseOwner.current) {
    if (!setEmulatorPaused(params.emulator.current, false)) {
      params.onFatalError("PLAYER_IMMERSIVE_RESUME_UNAVAILABLE");
      return;
    }
    params.pausedRef.current = false;
    params.setPaused(false);
  }
  params.pauseOwner.current = null;
  params.filter.reset();
  params.filter.setBlocked(false);
  params.updateOverlay({ kind: "closed" });
}

function enterReconnect(params: PollParams, target: "game" | "menu") {
  params.reconnectTarget.current = target;
  params.filter.setBlocked(true);
  if (!params.pausedRef.current) {
    if (!setEmulatorPaused(params.emulator.current, true)) {
      params.onFatalError("PLAYER_IMMERSIVE_PAUSE_UNAVAILABLE");
      return;
    }
    params.pauseOwner.current = "disconnect";
    params.pausedRef.current = true;
    params.setPaused(true);
  }
  params.activeIndex.current = null;
  setActiveImmersiveGamepadIndex(null);
  params.reconnectGate.current.reset();
  params.previousPressed.current.clear();
  params.previousReconnectA.current = false;
  params.updateOverlay({ kind: "reconnect", ready: false });
}

function pollReconnect(params: PollParams, gamepads: (Gamepad | null)[], nowMs: number) {
  if (params.activeIndex.current === null) {
    const candidate = gamepads.filter(isStandardImmersiveGamepad).sort((left, right) => left.index - right.index)
      .find((gamepad) => anyRisingButton(params.previousPressed.current, gamepad));
    rememberPressed(params.previousPressed.current, gamepads);
    if (!candidate) {return;}
    params.activeIndex.current = candidate.index;
    params.filter.setActiveGamepadIndex(candidate.index);
    setActiveImmersiveGamepadIndex(candidate.index);
    params.reconnectGate.current.reset();
    return;
  }
  const active = gamepads.find((gamepad) => gamepad?.index === params.activeIndex.current);
  if (!isStandardImmersiveGamepad(active)) {params.activeIndex.current = null; return;}
  const ready = params.reconnectGate.current.update(isNeutralGamepads(gamepads), nowMs);
  const current = gamepadButtonPressed(active, 0);
  const confirmed = ready && current && !params.previousReconnectA.current;
  params.previousReconnectA.current = current;
  const overlay = params.overlayRef.current;
  if (overlay.kind === "reconnect" && ready !== overlay.ready) {params.updateOverlay({ kind: "reconnect", ready });}
  if (!confirmed) {return;}
  if (params.reconnectTarget.current === "menu") {
    params.menuReader.current.reset();
    params.updateOverlay({ kind: "menu", error: "", notice: "", pending: false, selected: 0 });
    return;
  }
  params.beginClose("disconnect");
}

function anyRisingButton(previous: Map<number, boolean>, gamepad: Gamepad) {
  const current = gamepad.buttons.some((button) => button.pressed || button.value >= 0.5);
  return current && !previous.get(gamepad.index);
}

function rememberPressed(previous: Map<number, boolean>, gamepads: (Gamepad | null)[]) {
  previous.clear();
  for (const gamepad of gamepads) {
    if (gamepad) {previous.set(gamepad.index, gamepad.buttons.some((button) => button.pressed || button.value >= 0.5));}
  }
}
