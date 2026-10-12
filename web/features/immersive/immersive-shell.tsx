"use client";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { RestoreConfirmationContext, restoreConfirmationTitle, restoreConfirmationDescription, useRestoreConfirmation } from "@/features/player/restore-confirmation";
import { AppIcon } from "@/components/app-icon";
import { useToast } from "@/components/toast-provider";
import { Suspense, useEffect, useRef, useState } from "react";
import { ResourceState } from "@/components/resource-state";
import { toggleFavorite } from "@/features/library/api";
import { useImmersiveNavigation } from "./use-navigation";
import { setActiveImmersiveGamepadIndex } from "./active-gamepad";
import type { NavigationAction } from "./input-model";
import { ImmersiveGames } from "./immersive-content";
import { PlatformCarousel } from "./platform-carousel";
import type { View } from "./immersive-data";
import { useImmersiveLibrary } from "./use-immersive-library";
import { ImmersiveAudioProvider, useImmersiveAudio } from "./immersive-audio-provider";
import { ImmersiveSystemMenu } from "./immersive-system-menu";
import { useImmersiveSystemMenu } from "./use-immersive-system-menu";
import { useImmersiveFullscreen } from "./use-immersive-fullscreen";
import { ImmersiveChoiceDialog } from "./choice-dialog";
import { ImmersiveChrome } from "./immersive-chrome";
import styles from "./immersive.module.css";

export function ImmersiveShell() {
  return <Suspense><ImmersiveAudioProvider><ImmersiveContent /></ImmersiveAudioProvider></Suspense>;
}
function ImmersiveContent() {
  const shell = useRef<HTMLDivElement>(null);
  const { destinations, view, setView, selected, setSelected, destination, setDestination, current, entries, folder, setFolder, folders, returnTo, saves } = useImmersiveLibrary();
  const { notify } = useToast();
  const restoreConfirmation = useRestoreConfirmation();
  const [restoreChoice, setRestoreChoice] = useState("cancel");
  function confirmRestore() { setRestoreChoice("cancel"); return restoreConfirmation.request(); }
  const [exitOpen, setExitOpen] = useState(false);
  const [exitChoice, setExitChoice] = useState("continue");
  const router = useRouter();
  const audio = useImmersiveAudio();
  const fullscreen = useImmersiveFullscreen();
  const { enterFullscreen } = fullscreen;
  function leave() { setActiveImmersiveGamepadIndex(null); router.replace("/"); }
  function changeView(next: View) { setView(next); setSelected(0); }
  async function favorite() {
    const game = entries.data?.items[selected]?.game;
    if (!game) { return; }
    try { await toggleFavorite(game.id, !game.favorite); entries.reload(); destinations.reload(); notify({ tone: "good", message: game.favorite ? "已取消收藏。" : "已加入收藏。" }); }
    catch (failure) { notify({ tone: "bad", message: failure instanceof Error ? failure.message : "收藏失败。" }); }
  }
  function launch() { document.querySelector<HTMLButtonElement>(`.${styles.gameDetails} .home-launch-control button`)?.click(); }
  function navigate(action: NavigationAction) {
    if (action === "cancel") { if (view === "platforms") { setExitChoice("continue"); setExitOpen(true); } else { changeView("platforms"); } return; }
    if (view === "platforms") { navigatePlatform(action); return; }
    navigateGames(action);
  }
  function navigatePlatform(action: NavigationAction) {
    const count = destinations.data?.items.length ?? 0;
    if (!count) { return; }
    if (action === "left" || action === "right") { setDestination((value) => (value + (action === "left" ? -1 : 1) + count) % count); }
    if (action === "confirm" && current) { changeView(current.view); }
  }
  function navigateGames(action: NavigationAction) {
    if (action === "up" || action === "down") {
      const last = Math.max(0, (entries.data?.items.length ?? 1) - 1);
      const next = Math.max(0, selected + (action === "up" ? -1 : 1));
      setSelected(Math.min(next, last + (entries.hasMore ? 1 : 0)));
      if (next >= last && entries.hasMore && !entries.moreError) { entries.loadMore(); }
    }
    if (view === "saves" && (action === "left" || action === "right")) { saves.move(action); }
    if (action === "confirm") { launch(); }
    if (action === "favorite") { void favorite(); }
  }
  const systemMenu = useImmersiveSystemMenu({ commitPreference: audio.commitPreference, enterFullscreen: fullscreen.enterFullscreen, fullscreenActive: fullscreen.active, fullscreenSupported: fullscreen.supported, onBrowseAction: navigate, onExit: leave, preferences: audio.preferences });
  function chooseExit(id: string) { if (id === "exit") { leave(); } else { setExitOpen(false); } }
  const controller = useImmersiveNavigation((action) => {
    if (restoreConfirmation.open) {
      if (action === "left") { setRestoreChoice("cancel"); }
      if (action === "right") { setRestoreChoice("load"); }
      if (action === "cancel") { restoreConfirmation.settle(false); }
      if (action === "confirm") { restoreConfirmation.settle(restoreChoice === "load"); }
      return;
    }
    if (!exitOpen) { systemMenu.handleActions([action]); return; }
    if (action === "left") { setExitChoice("continue"); }
    if (action === "right") { setExitChoice("exit"); }
    if (action === "cancel") { setExitOpen(false); }
    if (action === "confirm") { chooseExit(exitChoice); }
  }, shell, restoreConfirmation.open || exitOpen || systemMenu.open);
  useEffect(() => {
    function key(event: KeyboardEvent) {
      if (event.key.toLowerCase() === "f" && !(event.target instanceof HTMLInputElement)) { event.preventDefault(); void enterFullscreen(); }
    }
    window.addEventListener("keydown", key);
    return () => window.removeEventListener("keydown", key);
  }, [enterFullscreen]);
  return (
    <RestoreConfirmationContext value={confirmRestore}><div ref={shell} tabIndex={-1} className={styles.shell} data-immersive-shell="true">
      <ImmersiveChrome view={view} fullscreen={fullscreen} onMenu={() => systemMenu.handleActions(["menu"])} />
      <main className={styles.shellContent}>
        {controller.message ? <p className={styles.notice} role="status">{controller.message}</p> : null}
        {view === "platforms" ? (
          <ResourceState resource={destinations}>{(data) => <PlatformCarousel destinations={data.items} selected={destination} onSelect={setDestination} onOpen={() => current && changeView(current.view)} />}</ResourceState>
        ) : (
          <ResourceState resource={entries}>{(data) => (
            <ImmersiveGames returnTo={returnTo} saves={saves} saveView={view === "saves"} resource={entries} entries={data.items} total={data.total} selected={selected} onSelect={setSelected} title={current?.name ?? "游戏库"}
              filter={view === "favorites" ? <label className="field">收藏夹<select aria-label="收藏夹" value={folder} onChange={(event) => { setFolder(event.target.value); setSelected(0); }}>
                <option value="">全部收藏</option><option value="unclassified">未分类</option>
                {folders.data?.items.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}
              </select></label> : null}
              onLaunch={launch} />
          )}</ResourceState>
        )}
      </main>
      {systemMenu.open ? <ImmersiveSystemMenu announcement={systemMenu.announcement} fullscreenActive={fullscreen.active} fullscreenSupported={fullscreen.supported} preferences={audio.preferences} selectedIndex={systemMenu.selectedIndex} onActivate={systemMenu.activate} onAdjust={systemMenu.commitPreference} onClose={systemMenu.close} onSelect={systemMenu.select} /> : null}
      {restoreConfirmation.open ? <ImmersiveChoiceDialog title={restoreConfirmationTitle} description={restoreConfirmationDescription}
        selectedId={restoreChoice} choices={[{ id: "cancel", label: "取消" }, { id: "load", label: "尝试加载" }]}
        onChoose={(id) => restoreConfirmation.settle(id === "load")} /> : null}
      {exitOpen ? <ImmersiveChoiceDialog title="退出沉浸模式？" description="返回后将继续使用普通 PC 或移动界面。" selectedId={exitChoice} choices={[{ id: "continue", label: "继续沉浸模式" }, { id: "exit", label: "返回普通首页", tone: "danger" }]} onChoose={chooseExit} /> : null}
      {!controller.ready ? <section className={styles.controllerOverlay} role="status"><div><span className={styles.controllerGlyph}><AppIcon name="gamepad" /></span><h2>等待手柄</h2><p>按下标准布局手柄上的任意按键以继续，也可使用方向键浏览。</p><Link href="/">返回首页</Link></div></section> : null}
      <div className={styles.viewportOverlay}><div><h2>沉浸模式需要横屏大屏</h2><p>请使用至少 960 × 540 的横屏视口。</p><Link className="button" href="/">返回普通首页</Link></div></div>
    </div></RestoreConfirmationContext>
  );
}
