"use client";

import { useCallback, useEffect, useEffectEvent, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { getActiveImmersiveGamepadIndex, setActiveImmersiveGamepadIndex } from "./active-gamepad";
import { browserGamepadSource, type GamepadFrame, type GamepadFrameSource } from "./gamepad-source";
import { requestImmersiveFullscreen } from "./immersive-fullscreen";
import { GamepadClaimModel, NavigationInputModel, isStandardGamepad, type NavigationAction } from "./input-model";
import styles from "./entry.module.css";

type EntryMode = "idle" | "locked" | "ready" | "cooldown" | "navigating";
type EntrySelection = "cancel" | "enter";

function selectedFromAction(action: NavigationAction, selection: EntrySelection) {
  return action === "left" ? "cancel" : action === "right" ? "enter" : selection;
}

export function ImmersiveEntryDialog({ source = browserGamepadSource }: { source?: GamepadFrameSource }) {
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [ready, setReady] = useState(false);
  const [selection, setSelection] = useState<EntrySelection>("cancel");
  const [notice, setNotice] = useState("");
  const modeRef = useRef<EntryMode>("idle");
  const selectionRef = useRef<EntrySelection>("cancel");
  const cooldownPadRef = useRef<number | null>(null);
  const claimModelRef = useRef(new GamepadClaimModel());
  const navigationRef = useRef(new NavigationInputModel());
  const cancelButtonRef = useRef<HTMLButtonElement>(null);
  const enterButtonRef = useRef<HTMLButtonElement>(null);
  const dialogRef = useRef<HTMLElement>(null);
  const returnFocusRef = useRef<HTMLElement | null>(null);

  const focusSelection = useCallback((next: EntrySelection) => {
    (next === "cancel" ? cancelButtonRef : enterButtonRef).current?.focus();
  }, []);

  const choose = useCallback((next: EntrySelection) => {
    selectionRef.current = next;
    setSelection(next);
    if (modeRef.current === "ready") {
      focusSelection(next);
    }
  }, [focusSelection]);

  function cancel() {
    if (modeRef.current === "idle" || modeRef.current === "cooldown") {return;}
    cooldownPadRef.current = getActiveImmersiveGamepadIndex();
    setActiveImmersiveGamepadIndex(null);
    navigationRef.current.reset(500);
    modeRef.current = "cooldown";
    setOpen(false);
    setReady(false);
    choose("cancel");
    returnFocusRef.current?.focus();
  }

  function enter() {
    if (modeRef.current !== "ready") {return;}
    modeRef.current = "navigating";
    setOpen(false);
    void requestImmersiveFullscreen();
    router.push("/immersive");
  }

  const handleActions = useEffectEvent((actions: readonly NavigationAction[]) => {
    for (const action of actions) {
      if (action === "cancel") {cancel(); return;}
      const next = selectedFromAction(action, selectionRef.current);
      if (next !== selectionRef.current) {choose(next);}
      if (action === "confirm") {
        if (selectionRef.current === "enter") {enter();} else {cancel();}
        return;
      }
    }
  });

  useEffect(() => {
    function discover(frame: GamepadFrame) {
      const claim = claimModelRef.current.update(frame.gamepads);
      if (claim.unsupportedEdge) {setNotice("当前手柄不是标准布局，沉浸模式无法保证操作");}
      if (claim.claimedIndex === null) {return;}
      setActiveImmersiveGamepadIndex(claim.claimedIndex);
      navigationRef.current.reset(120);
      choose("cancel");
      returnFocusRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
      modeRef.current = "locked";
      setOpen(true);
    }

    function coolDown(frame: GamepadFrame) {
      const candidate = frame.gamepads.find((item) => item.index === cooldownPadRef.current) ?? null;
      const gamepad = candidate && isStandardGamepad(candidate) ? candidate : null;
      const update = navigationRef.current.update(gamepad, frame.nowMs);
      if (!update.neutralReady && gamepad) {return;}
      modeRef.current = "idle";
      cooldownPadRef.current = null;
      claimModelRef.current.reset(frame.gamepads);
    }

    function updateDialog(frame: GamepadFrame) {
      const index = modeRef.current === "cooldown" ? cooldownPadRef.current : getActiveImmersiveGamepadIndex();
      const candidate = frame.gamepads.find((item) => item.index === index) ?? null;
      const gamepad = candidate && isStandardGamepad(candidate) ? candidate : null;
      if (!gamepad) {
        setActiveImmersiveGamepadIndex(null);
        modeRef.current = "idle";
        setOpen(false);
        setReady(false);
        setNotice("手柄已断开");
        claimModelRef.current.reset(frame.gamepads);
        return;
      }
      const update = navigationRef.current.update(gamepad, frame.nowMs);
      if (modeRef.current === "locked" && update.neutralReady) {
        modeRef.current = "ready";
        setReady(true);
      }
      if (modeRef.current === "ready") {handleActions(update.actions);}
    }

    function onFrame(frame: GamepadFrame) {
      if (modeRef.current === "navigating") {return;}
      if (frame.suspended) {
        navigationRef.current.reset(modeRef.current === "cooldown" ? 500 : 120);
        if (modeRef.current === "ready") {modeRef.current = "locked"; setReady(false);}
        return;
      }
      if (modeRef.current === "idle") {discover(frame); return;}
      if (modeRef.current === "cooldown") {coolDown(frame); return;}
      updateDialog(frame);
    }
    return source.subscribe(onFrame);
  }, [choose, source]);

  useEffect(() => {
    if (!open) {return;}
    if (ready) {focusSelection(selection);}
    else {dialogRef.current?.focus();}
  }, [focusSelection, open, ready, selection]);

  useEffect(() => {
    if (!open) {return;}
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        cancel();
        return;
      }
      if (!ready || !["ArrowLeft", "ArrowRight", "Enter", "Tab"].includes(event.key)) {return;}
      event.preventDefault();
      if (event.key === "ArrowLeft") {choose("cancel");}
      if (event.key === "ArrowRight") {choose("enter");}
      if (event.key === "Tab") {choose(event.shiftKey ? "cancel" : selectionRef.current === "cancel" ? "enter" : "cancel");}
      if (event.key === "Enter") {
        if (selectionRef.current === "enter") {enter();} else {cancel();}
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  });

  return <>
    {notice ? <p className={styles.homeGamepadNotice} role="status">{notice}</p> : null}
    {open ? <div className={styles.entryTakeover} data-immersive-entry="true" onPointerDown={(event) => {
      if (event.currentTarget === event.target) {cancel();}
    }}>
      <header className={styles.entryHeader}>
        <div><span aria-hidden="true">R</span><strong>RETROM</strong><small>沉浸模式</small></div>
        <div className={styles.entryControls} aria-label="手柄操作提示">
          <span><kbd data-button="A">A</kbd>确认</span><span><kbd data-button="B">B</kbd>取消</span>
        </div>
      </header>
      <section
        ref={dialogRef}
        className={styles.entryDialog}
        role="alertdialog"
        aria-modal="true"
        aria-labelledby="immersive-entry-title"
        aria-describedby="immersive-entry-description immersive-entry-status"
        tabIndex={-1}
      >
        <p className={styles.entryEyebrow}>检测到标准布局手柄</p>
        <h1 id="immersive-entry-title">进入沉浸模式？</h1>
        <p id="immersive-entry-description">使用手柄按平台浏览并启动游戏。沉浸模式采用独立的大屏界面，不包含存档、联机和管理功能。</p>
        <div className={styles.entryActions}>
          <button
            ref={cancelButtonRef}
            className={selection === "cancel" ? styles.entrySelected : undefined}
            type="button"
            disabled={!ready}
            onClick={cancel}
          >取消</button>
          <button
            ref={enterButtonRef}
            className={selection === "enter" ? styles.entrySelected : undefined}
            type="button"
            disabled={!ready}
            onClick={enter}
          >进入沉浸模式</button>
        </div>
        <p id="immersive-entry-status" className={styles.entryHint} role="status">
          {ready ? "A 确认 · B 取消 · 左右选择" : "请松开手柄按键，稍候即可选择…"}
        </p>
      </section>
    </div> : null}
  </>;
}
