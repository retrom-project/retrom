"use client";

import { useCallback, useState } from "react";
import type { NavigationAction } from "./input-model";
import {
  getImmersiveAudioPreferences,
  saveImmersiveAudioPreferences,
  type ImmersiveAudioPreferences,
} from "./immersive-audio-preferences";
import {
  adjustSystemMenuPreference,
  immersiveSystemMenuItems,
  moveSystemMenuSelection,
  type ImmersiveSystemMenuItem,
  type MenuAdjustment,
} from "./system-menu-model";

export function useImmersiveSystemMenu({ enterFullscreen, fullscreenActive, fullscreenSupported, onBrowseAction, onExit }: {
  enterFullscreen: () => Promise<boolean>;
  fullscreenActive: boolean;
  fullscreenSupported: boolean;
  onBrowseAction: (action: NavigationAction) => void;
  onExit: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [preferences, setPreferences] = useState<ImmersiveAudioPreferences>(getImmersiveAudioPreferences);
  const [announcement, setAnnouncement] = useState("");

  const commitPreference = useCallback((item: ImmersiveSystemMenuItem, adjustment: MenuAdjustment) => {
    setPreferences((current) => {
      const next = adjustSystemMenuPreference(current, item, adjustment);
      if (next !== current) {saveImmersiveAudioPreferences(next);}
      return next;
    });
  }, []);

  const activate = useCallback((item: ImmersiveSystemMenuItem) => {
    if (item === "fullscreen") {
      if (fullscreenActive) {setAnnouncement("已经处于全屏模式"); return;}
      if (!fullscreenSupported) {setAnnouncement("当前浏览器不支持网页全屏"); return;}
      void enterFullscreen().then((entered) => setAnnouncement(entered
        ? "已进入全屏模式"
        : "浏览器未允许全屏，请再次按 A 确认或使用鼠标重试"));
      return;
    }
    if (item === "exit") {onExit(); return;}
    commitPreference(item, "confirm");
  }, [commitPreference, enterFullscreen, fullscreenActive, fullscreenSupported, onExit]);

  const handleOpenActions = useCallback((actions: readonly NavigationAction[]) => {
    let nextIndex = selectedIndex;
    for (const action of actions) {
      if (action === "cancel") {setOpen(false); setAnnouncement(""); return;}
      if (action === "up" || action === "down") {
        nextIndex = moveSystemMenuSelection(nextIndex, action);
      } else if (action === "left" || action === "right") {
        commitPreference(immersiveSystemMenuItems[nextIndex], action);
      } else if (action === "confirm") {
        activate(immersiveSystemMenuItems[nextIndex]);
      }
    }
    if (nextIndex !== selectedIndex) {setSelectedIndex(nextIndex);}
  }, [activate, commitPreference, selectedIndex]);

  const handleActions = useCallback((actions: readonly NavigationAction[]) => {
    if (open) {handleOpenActions(actions); return;}
    if (actions.includes("menu")) {
      setSelectedIndex(0);
      setAnnouncement("");
      setOpen(true);
      return;
    }
    for (const action of actions) {onBrowseAction(action);}
  }, [handleOpenActions, onBrowseAction, open]);

  return {
    activate,
    announcement,
    close: () => {setOpen(false); setAnnouncement("");},
    commitPreference,
    handleActions,
    open,
    preferences,
    selectedIndex,
    select: setSelectedIndex,
  };
}
