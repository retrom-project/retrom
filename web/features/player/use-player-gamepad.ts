"use client";

import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type MutableRefObject,
} from "react";
import type { ControllerSource } from "@/features/gamepad/types";
import type {
  EmulatorInstance,
  ManualScreenshot,
  PlayerConfig,
} from "./adapters/ejs-4.2.3-v2";
import { releaseAllPlayerInputs } from "./gamepad-host-controls";
import type { PlayerGamepadHostControls } from "./gamepad-host-controls";
import { usePlayerGamepadRuntime } from "./player-gamepad-runtime";

type Mutable<T> = MutableRefObject<T>;
type PauseLease = "none" | "single" | "netplay";

type PlayerGamepadParams = Readonly<{
  activeIndex: number | null;
  source: ControllerSource;
  claim: (index: number, centerButtonObservable?: boolean) => void;
  releaseClaim: () => void;
  host: Mutable<PlayerGamepadHostControls>;
  emulator: Mutable<EmulatorInstance | undefined>;
  running: Mutable<boolean>;
  pausedRef: Mutable<boolean>;
  pausePending: Mutable<boolean>;
  pauseCapture: Mutable<Promise<ManualScreenshot | null>>;
  playerMode: Mutable<PlayerConfig["mode"]>;
  netplayConfig: Mutable<NonNullable<PlayerConfig["netplay"]> | null>;
  netplayPausedRef: Mutable<boolean>;
  requestNetplayPause: (action: "pause" | "resume") => Promise<boolean>;
  pauseForToolbarInteraction: () => void;
  resumeSinglePlayer: () => void;
  holdControls: () => void;
  releaseControls: () => void;
  showControls: () => void;
  showToast: (message: string, timeout?: number) => void;
}>;

export function usePlayerGamepad(params: PlayerGamepadParams) {
  const {
    activeIndex, source, claim, releaseClaim, host, emulator, running, pausedRef,
    pausePending: toolbarPausePending, pauseCapture, playerMode, netplayConfig,
    netplayPausedRef, requestNetplayPause, pauseForToolbarInteraction,
    resumeSinglePlayer, holdControls, releaseControls, showControls, showToast,
  } = params;
  const [menuRequest, setMenuRequest] = useState(0);
  const [exitRequest, setExitRequest] = useState(0);
  const [reconnectOpen, setReconnectOpen] = useState(false);
  const [reconnectReady, setReconnectReady] = useState(false);
  const overlayAcquired = useRef(false);
  const pauseLease = useRef<PauseLease>("none");
  const pausePending = useRef<Promise<void>>(Promise.resolve());

  useEffect(() => {
    if (activeIndex !== null && host.current.currentIndex() !== activeIndex) {
      host.current.setActiveIndex(activeIndex);
    }
  }, [activeIndex, host]);

  const acquireOverlay = useCallback(() => {
    releaseAllPlayerInputs(emulator.current);
    host.current.openOverlay();
    holdControls();
    if (overlayAcquired.current) {return;}
    overlayAcquired.current = true;
    if (!running.current) {return;}
    if (playerMode.current === "single") {
      if (pausedRef.current || toolbarPausePending.current || !emulator.current) {return;}
      pauseLease.current = "single";
      pauseForToolbarInteraction();
      pausePending.current = pauseCapture.current.then(() => undefined);
      return;
    }
    if (netplayConfig.current?.playerNo !== 1) {
      showToast("你是 P2；游戏仍由 P1 继续，本地输入已释放。", 4_000);
      return;
    }
    if (netplayPausedRef.current) {return;}
    pausePending.current = requestNetplayPause("pause").then((acquired) => {
      if (acquired) {pauseLease.current = "netplay";}
      else {showToast("无法暂停双方，菜单保持打开且本地输入仍已释放。", 4_000);}
    });
  }, [emulator, holdControls, netplayConfig, netplayPausedRef, pauseCapture, pauseForToolbarInteraction,
    pausedRef, playerMode, requestNetplayPause, running, showToast, toolbarPausePending, host]);

  const resumeAfterOverlay = useCallback(async () => {
    await pausePending.current;
    if (pauseLease.current === "single" && pausedRef.current) {
      resumeSinglePlayer();
    } else if (pauseLease.current === "netplay") {
      await requestNetplayPause("resume");
    }
    pauseLease.current = "none";
    overlayAcquired.current = false;
    releaseControls();
    showControls();
  }, [pausedRef, releaseControls, requestNetplayPause, resumeSinglePlayer, showControls]);

  const onSystemOverlayChange = useCallback((open: boolean) => {
    if (host.current.currentIndex() === null) {return;}
    if (open) {acquireOverlay();}
    else if (!reconnectOpen) {host.current.requestGameplay();}
  }, [acquireOverlay, host, reconnectOpen]);

  const callbacks = useMemo(() => ({
    claim,
    onOpenMenu: () => {
      acquireOverlay();
      setMenuRequest((value) => value + 1);
    },
    onOpenExit: () => {
      acquireOverlay();
      setExitRequest((value) => value + 1);
    },
    onDisconnect: () => {
      releaseClaim();
      releaseAllPlayerInputs(emulator.current);
      acquireOverlay();
      setReconnectReady(false);
      setReconnectOpen(true);
    },
    onSuspend: () => releaseAllPlayerInputs(emulator.current),
    onReconnectReady: () => setReconnectReady(true),
    onGameplayReady: () => {void resumeAfterOverlay();},
  }), [acquireOverlay, claim, emulator, releaseClaim, resumeAfterOverlay]);

  usePlayerGamepadRuntime(host, source, callbacks);

  return {
    menuRequest,
    exitRequest,
    reconnectOpen,
    reconnectReady,
    onSystemOverlayChange,
    onReconnect: () => {
      setReconnectOpen(false);
      host.current.requestGameplay();
    },
  };
}
