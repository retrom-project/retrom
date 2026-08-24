"use client";

import { useEffect, useEffectEvent, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { getActiveImmersiveGamepadIndex, setActiveImmersiveGamepadIndex } from "./active-gamepad";
import { browserGamepadSource, type GamepadFrame, type GamepadFrameSource } from "./gamepad-source";
import { GamepadClaimModel, NavigationInputModel, isStandardGamepad, type NavigationAction } from "./input-model";
import styles from "./immersive.module.css";

type EntryMode = "idle" | "locked" | "ready" | "cooldown" | "navigating";
type EntrySelection = "cancel" | "enter";

function focusSelection(selection: EntrySelection) {
  const dialog = document.querySelector<HTMLElement>(".immersive-entry-dialog");
  const buttons = dialog?.querySelectorAll<HTMLButtonElement>(".dialog-actions button:not(:disabled)");
  buttons?.[selection === "cancel" ? 0 : 1]?.focus();
}

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

  function choose(next: EntrySelection) {
    selectionRef.current = next;
    setSelection(next);
    if (modeRef.current === "ready") {
      focusSelection(next);
    }
  }

  function cancel() {
    if (modeRef.current === "idle" || modeRef.current === "cooldown") {return;}
    cooldownPadRef.current = getActiveImmersiveGamepadIndex();
    setActiveImmersiveGamepadIndex(null);
    navigationRef.current.reset(500);
    modeRef.current = "cooldown";
    setOpen(false);
    setReady(false);
    choose("cancel");
  }

  function enter() {
    if (modeRef.current !== "ready") {return;}
    modeRef.current = "navigating";
    setOpen(false);
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
  }, [source]);

  useEffect(() => {
    if (open && ready) {focusSelection(selection);}
  }, [open, ready, selection]);

  useEffect(() => {
    if (!open || !ready) {return;}
    const onKeyDown = (event: KeyboardEvent) => {
      if (!["ArrowLeft", "ArrowRight", "Enter"].includes(event.key)) {return;}
      event.preventDefault();
      if (event.key === "ArrowLeft") {choose("cancel");}
      if (event.key === "ArrowRight") {choose("enter");}
      if (event.key === "Enter") {
        if (selectionRef.current === "enter") {enter();} else {cancel();}
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  });

  return <>
    {notice ? <p className={styles.homeGamepadNotice} role="status">{notice}</p> : null}
    <ConfirmDialog
      open={open}
      title="进入沉浸模式？"
      description="使用手柄按平台浏览并启动游戏。沉浸模式采用独立的大屏界面，不包含存档、联机和管理功能。"
      cancelLabel="取消"
      confirmLabel="进入沉浸模式"
      dialogClassName="immersive-entry-dialog"
      interactionDisabled={!ready}
      onCancel={cancel}
      onConfirm={enter}
    >
      <p className={styles.entryHint}>{ready ? "A 确认 · B 取消 · 左右选择" : "请松开手柄按键…"}</p>
    </ConfirmDialog>
  </>;
}
