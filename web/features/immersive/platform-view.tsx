"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import { fetchImmersivePlatforms, ImmersiveAPIError, type ImmersivePlatform } from "./api";
import { setActiveImmersiveGamepadIndex } from "./active-gamepad";
import { ImmersiveChoiceDialog } from "./choice-dialog";
import { ImmersiveShell } from "./immersive-shell";
import type { NavigationAction } from "./input-model";
import { wrapPlatformIndex } from "./platform-selection";
import platformStyles from "./platform.module.css";
import styles from "./immersive.module.css";

type ViewState = "loading" | "ready" | "empty" | "error" | "unauthorized";
type ExitChoice = "continue" | "exit";
type PlatformTransition = Readonly<{ direction: "left" | "right" | null; sequence: number }>;

function platformCode(platform: ImmersivePlatform) {
  const normalized = platform.platformId.replaceAll(/[^a-z0-9]/gi, "").toUpperCase();
  return normalized.slice(0, 4) || "GAME";
}

const platformToneClasses = [platformStyles.platformTone0, platformStyles.platformTone1, platformStyles.platformTone2, platformStyles.platformTone3, platformStyles.platformTone4];

function platformTone(platformId: string) {
  let hash = 0;
  for (const character of platformId) {hash = (hash * 31 + (character.codePointAt(0) ?? 0)) >>> 0;}
  return platformToneClasses[hash % platformToneClasses.length];
}

function relativePlayTime(lastPlayedAtMs: number | null, generatedAtMs: number) {
  if (lastPlayedAtMs === null) {return "尚未游玩";}
  const delta = Math.max(0, generatedAtMs - lastPlayedAtMs);
  if (delta < 60_000) {return "刚刚";}
  if (delta < 3_600_000) {return `${Math.floor(delta / 60_000)} 分钟前`;}
  if (delta < 86_400_000) {return `${Math.floor(delta / 3_600_000)} 小时前`;}
  if (delta < 604_800_000) {return `${Math.floor(delta / 86_400_000)} 天前`;}
  return new Intl.DateTimeFormat("zh-CN", { month: "long", day: "numeric", hour: "2-digit", minute: "2-digit" }).format(lastPlayedAtMs);
}

function PlatformCard({ current, generatedAtMs, platform }: { current?: boolean; generatedAtMs: number; platform: ImmersivePlatform }) {
  return <article className={`${platformStyles.platformCard} ${platformTone(platform.platformId)} ${current ? platformStyles.currentPlatform : ""}`.trim()} aria-current={current ? "true" : undefined}>
    <span className={platformStyles.platformCode}>{platformCode(platform)}</span>
    <p>{current ? "当前平台" : "相邻平台"}</p>
    <h2>{platform.platformName}</h2>
    <strong>{platform.gameCount} 款游戏</strong>
    <small>上次游玩：{relativePlayTime(platform.lastPlayedAtMs, generatedAtMs)}</small>
  </article>;
}

export function PlatformView({ initialPlatformId }: { initialPlatformId?: string }) {
  const router = useRouter();
  const [state, setState] = useState<ViewState>("loading");
  const [platforms, setPlatforms] = useState<ImmersivePlatform[]>([]);
  const [generatedAtMs, setGeneratedAtMs] = useState(0);
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [transition, setTransition] = useState<PlatformTransition>({ direction: null, sequence: 0 });
  const [exitOpen, setExitOpen] = useState(false);
  const [exitChoice, setExitChoice] = useState<ExitChoice>("continue");
  const selected = platforms[selectedIndex];

  const requestPlatforms = useCallback(() => {
    const controller = new AbortController();
    void fetchImmersivePlatforms(controller.signal).then((result) => {
      setPlatforms(result.items);
      setGeneratedAtMs(result.generatedAtMs);
      const requested = result.items.findIndex((item) => item.platformId === initialPlatformId);
      setSelectedIndex(requested >= 0 ? requested : 0);
      setState(result.items.length ? "ready" : "empty");
    }).catch((error: unknown) => {
      if (error instanceof DOMException && error.name === "AbortError") {return;}
      setState(error instanceof ImmersiveAPIError && error.status === 401 ? "unauthorized" : "error");
    });
    return () => controller.abort();
  }, [initialPlatformId]);

  useEffect(() => requestPlatforms(), [requestPlatforms]);

  const retry = useCallback(() => {
    setState("loading");
    requestPlatforms();
  }, [requestPlatforms]);

  const leave = useCallback(() => {
    setActiveImmersiveGamepadIndex(null);
    router.replace("/");
  }, [router]);

  const confirmExitChoice = useCallback((choice: ExitChoice) => {
    if (choice === "exit") {leave(); return;}
    setExitOpen(false);
    setExitChoice("continue");
  }, [leave]);

  const handleExitAction = useCallback((action: NavigationAction) => {
      if (action === "cancel") {confirmExitChoice("continue"); return;}
      if (action === "left") {setExitChoice("continue");}
      if (action === "right") {setExitChoice("exit");}
      if (action === "confirm") {confirmExitChoice(exitChoice);}
  }, [confirmExitChoice, exitChoice]);

  const handleStateAction = useCallback((action: NavigationAction) => {
    if (state === "error" && action === "confirm") {retry(); return true;}
    if (state === "error" && action === "cancel") {leave(); return true;}
    if (state === "empty" && (action === "confirm" || action === "cancel")) {leave(); return true;}
    if (state === "unauthorized" && (action === "confirm" || action === "cancel")) {router.replace("/login"); return true;}
    return state !== "ready";
  }, [leave, retry, router, state]);

  const onAction = useCallback((action: NavigationAction) => {
    if (exitOpen) {
      handleExitAction(action);
      return;
    }
    if (handleStateAction(action) || !selected) {return;}
    if (action === "left" || action === "right") {
      setSelectedIndex((value) => wrapPlatformIndex(value, action, platforms.length));
      setTransition((current) => ({ direction: action, sequence: current.sequence + 1 }));
    }
    if (action === "confirm") {router.push(`/immersive/platforms/${encodeURIComponent(selected.platformId)}`);}
    if (action === "cancel") {setExitChoice("continue"); setExitOpen(true);}
  }, [exitOpen, handleExitAction, handleStateAction, platforms.length, router, selected]);

  const neighbors = useMemo(() => {
    if (!selected || platforms.length < 2) {return { previous: null, next: null };}
    return {
      previous: platforms[(selectedIndex - 1 + platforms.length) % platforms.length],
      next: platforms[(selectedIndex + 1) % platforms.length],
    };
  }, [platforms, selected, selectedIndex]);

  const help = state === "ready"
    ? [{ button: "horizontal", label: "选择平台" }, { button: "A", label: "进入" }, { button: "B", label: "退出沉浸模式" }]
    : [{ button: "A", label: state === "error" ? "重试" : "返回首页" }, { button: "B", label: "返回首页" }];
  return <ImmersiveShell help={help} inputEpoch={`${state}:${exitOpen}`} onAction={onAction}>
    <section className={platformStyles.platformView} aria-labelledby="platform-view-title">
      <div className={styles.viewHeading}><p>选择游戏平台</p><h1 id="platform-view-title">今天想玩哪个平台？</h1></div>
      {state === "loading" ? <div className={styles.centerState} role="status"><span className={styles.spinner} />正在读取游戏平台…</div> : null}
      {state === "empty" ? <div className={styles.centerState}><h2>还没有可游玩的游戏</h2><p>返回普通界面导入并发布游戏后再来看看。</p><button type="button" onClick={leave}>返回普通首页</button></div> : null}
      {state === "error" ? <div className={styles.centerState} role="alert"><h2>无法读取游戏平台</h2><p>请检查服务状态后重试。</p><button type="button" onClick={retry}>重试</button></div> : null}
      {state === "unauthorized" ? <div className={styles.centerState} role="alert"><h2>登录状态已失效</h2><p>请返回登录后重新进入沉浸模式。</p><button type="button" onClick={() => router.replace("/login")}>返回登录</button></div> : null}
      {state === "ready" && selected ? <div className={platformStyles.platformStage}>
        <div
          key={`${selected.platformId}:${transition.sequence}`}
          className={platformStyles.platformCarousel}
          data-direction={transition.direction ?? undefined}
          data-selected-index={selectedIndex}
          role="listbox"
          tabIndex={0}
          aria-label="游戏平台"
          aria-activedescendant={`platform-${selected.platformId}`}
        >
          {neighbors.previous ? <div role="option" aria-selected="false"><PlatformCard platform={neighbors.previous} generatedAtMs={generatedAtMs} /></div> : <div />}
          <div id={`platform-${selected.platformId}`} role="option" aria-selected="true"><PlatformCard current platform={selected} generatedAtMs={generatedAtMs} /></div>
          {neighbors.next ? <div role="option" aria-selected="false"><PlatformCard platform={neighbors.next} generatedAtMs={generatedAtMs} /></div> : <div />}
        </div>
        <div className={platformStyles.platformPosition} aria-hidden="true">
          <span>{String(selectedIndex + 1).padStart(2, "0")}</span>
          <div className={platformStyles.platformPositionTrack}>
            <i
              data-testid="platform-position-indicator"
              style={{
                width: `${100 / platforms.length}%`,
                transform: `translateX(${selectedIndex * 100}%)`,
              }}
            />
          </div>
          <span>{String(platforms.length).padStart(2, "0")}</span>
        </div>
      </div> : null}
    </section>
    {exitOpen ? <ImmersiveChoiceDialog
      title="退出沉浸模式？"
      description="返回后将继续使用普通 PC 或移动界面。"
      selectedId={exitChoice}
      choices={[{ id: "continue", label: "继续沉浸模式" }, { id: "exit", label: "返回普通首页", tone: "danger" }]}
      onChoose={(choice) => confirmExitChoice(choice === "exit" ? "exit" : "continue")}
    /> : null}
  </ImmersiveShell>;
}
