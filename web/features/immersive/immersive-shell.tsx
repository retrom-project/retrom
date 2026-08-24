"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useEffectEvent, useRef, useState, type ReactNode } from "react";
import {
  consumeImmersivePlayerReturn,
  getActiveImmersiveGamepadIndex,
  isImmersivePlayerReturnPending,
  setActiveImmersiveGamepadIndex,
} from "./active-gamepad";
import { browserGamepadSource, type GamepadFrame, type GamepadFrameSource } from "./gamepad-source";
import { GamepadClaimModel, NavigationInputModel, isStandardGamepad, type NavigationAction } from "./input-model";
import { useImmersiveFullscreen } from "./use-immersive-fullscreen";
import styles from "./immersive.module.css";

export type HelpAction = Readonly<{ button: string; label: string }>;

function HelpButton({ button }: { button: string }) {
  if (button !== "horizontal" && button !== "vertical") {
    return <kbd data-button={button}>{button}</kbd>;
  }
  const horizontal = button === "horizontal";
  return <kbd data-button={button} role="img" aria-label={horizontal ? "左右方向键" : "上下方向键"}>
    <svg viewBox="0 0 24 24" aria-hidden="true">
      {horizontal
        ? <path d="M8 12h8M10 8l-4 4 4 4M14 8l4 4-4 4" />
        : <path d="M12 8v8M8 10l4-4 4 4M8 14l4 4 4-4" />}
    </svg>
  </kbd>;
}

function keyboardAction(key: string): NavigationAction | null {
  return ({ ArrowLeft: "left", ArrowRight: "right", ArrowUp: "up", ArrowDown: "down", Enter: "confirm", Escape: "cancel" } as const)[key as "ArrowLeft"] ?? null;
}

function useSupportedViewport() {
  const [supported, setSupported] = useState(true);
  useEffect(() => {
    const query = window.matchMedia("(orientation: landscape) and (min-width: 960px) and (min-height: 540px)");
    const update = () => setSupported(query.matches);
    update();
    query.addEventListener("change", update);
    return () => query.removeEventListener("change", update);
  }, []);
  return supported;
}

function formatClock(value: Date) {
  return new Intl.DateTimeFormat("zh-CN", { hour: "2-digit", minute: "2-digit", hour12: false }).format(value);
}

type ControllerState = "checking" | "ready" | "waiting";
const MISSING_GAMEPAD_GRACE_MS = 250;

export function ImmersiveShell({ children, help, inputEpoch, onAction, source = browserGamepadSource }: {
  children: ReactNode;
  help: readonly HelpAction[];
  inputEpoch?: string | number;
  onAction: (action: NavigationAction) => void;
  source?: GamepadFrameSource;
}) {
  const router = useRouter();
  const supportedViewport = useSupportedViewport();
  const [controllerState, setControllerState] = useState<ControllerState>(() => (
    getActiveImmersiveGamepadIndex() === null ? "waiting" : "checking"
  ));
  const [clock, setClock] = useState<Date | null>(null);
  const [inputModel] = useState(() => new NavigationInputModel(isImmersivePlayerReturnPending()));
  const claimRef = useRef(new GamepadClaimModel());
  const missingSinceMsRef = useRef<number | null>(null);
  const handleAction = useEffectEvent(onAction);
  const {
    active: fullscreenActive,
    enterFullscreen,
    restoreVisible: fullscreenRestoreVisible,
    supported: fullscreenSupported,
  } = useImmersiveFullscreen();

  useEffect(() => {
    consumeImmersivePlayerReturn();
  }, []);

  useEffect(() => {
    const initialTimer = window.setTimeout(() => setClock(new Date()), 0);
    const timer = window.setInterval(() => setClock(new Date()), 30_000);
    return () => {
      window.clearTimeout(initialTimer);
      window.clearInterval(timer);
    };
  }, []);

  useEffect(() => {
    function onFrame(frame: GamepadFrame) {
      if (frame.suspended) {
        inputModel.reset(120);
        return;
      }
      let activeIndex = getActiveImmersiveGamepadIndex();
      if (activeIndex === null) {
        const claim = claimRef.current.update(frame.gamepads);
        if (claim.claimedIndex === null) {return;}
        activeIndex = claim.claimedIndex;
        setActiveImmersiveGamepadIndex(activeIndex);
        inputModel.reset(120);
      }
      const candidate = frame.gamepads.find((item) => item.index === activeIndex) ?? null;
      const gamepad = candidate && isStandardGamepad(candidate) ? candidate : null;
      if (!gamepad) {
        if (missingSinceMsRef.current === null) {
          missingSinceMsRef.current = frame.nowMs;
          inputModel.reset(120);
          return;
        }
        if (frame.nowMs - missingSinceMsRef.current < MISSING_GAMEPAD_GRACE_MS) {return;}
        setActiveImmersiveGamepadIndex(null);
        setControllerState("waiting");
        inputModel.reset(120);
        claimRef.current.reset(frame.gamepads);
        return;
      }
      missingSinceMsRef.current = null;
      setControllerState("ready");
      const update = inputModel.update(gamepad, frame.nowMs);
      for (const action of update.actions) {handleAction(action);}
    }
    return source.subscribe(onFrame);
  }, [inputModel, source]);

  useEffect(() => {
    inputModel.reset(120);
  }, [inputEpoch, inputModel]);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key.toLowerCase() === "f" && fullscreenSupported && !fullscreenActive) {
        event.preventDefault();
        enterFullscreen();
        return;
      }
      const action = keyboardAction(event.key);
      if (!action) {return;}
      event.preventDefault();
      if (!supportedViewport && (action === "cancel" || action === "confirm")) {
        setActiveImmersiveGamepadIndex(null);
        router.replace("/");
        return;
      }
      setControllerState("ready");
      handleAction(action);
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [enterFullscreen, fullscreenActive, fullscreenSupported, router, supportedViewport]);

  return <main
    className={styles.shell}
    data-controller-state={controllerState}
    data-immersive-shell="true"
  >
    <header className={styles.shellHeader}>
      <div><strong>RETROM</strong><span>/</span><span>沉浸模式</span></div>
      {clock ? <time dateTime={clock.toISOString()}>{formatClock(clock)}</time> : <time aria-label="正在读取当前时间">--:--</time>}
    </header>
    <div className={styles.shellContent}>{children}</div>
    <footer className={styles.helpBar} aria-label="手柄操作提示">
      {help.map((item) => <span key={`${item.button}:${item.label}`}><HelpButton button={item.button} />{item.label}</span>)}
    </footer>
    {fullscreenSupported && !fullscreenActive ? <button
      type="button"
      className={`${styles.fullscreenRestore} ${fullscreenRestoreVisible ? styles.fullscreenRestoreVisible : ""}`.trim()}
      aria-hidden={!fullscreenRestoreVisible}
      tabIndex={fullscreenRestoreVisible ? 0 : -1}
      onClick={enterFullscreen}
    ><span aria-hidden="true">⛶</span>进入全屏</button> : null}
    {controllerState === "waiting" ? <section className={styles.controllerOverlay} role="status" aria-live="polite">
      <div><span className={styles.controllerGlyph} aria-hidden="true">⌁</span><h2>等待手柄</h2><p>按下标准布局手柄上的任意按键以继续。</p><Link href="/" onClick={() => setActiveImmersiveGamepadIndex(null)}>返回首页</Link></div>
    </section> : null}
    {!supportedViewport ? <section className={styles.viewportOverlay} role="alert">
      <div><h2>沉浸模式需要横屏大屏</h2><p>请使用至少 960 × 540 的横屏视口。</p><button type="button" onClick={() => {setActiveImmersiveGamepadIndex(null); router.replace("/");}}>返回普通首页</button></div>
    </section> : null}
  </main>;
}
