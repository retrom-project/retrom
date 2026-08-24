"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useEffectEvent, useRef, useState, type ReactNode } from "react";
import { getActiveImmersiveGamepadIndex, setActiveImmersiveGamepadIndex } from "./active-gamepad";
import { browserGamepadSource, type GamepadFrame, type GamepadFrameSource } from "./gamepad-source";
import { GamepadClaimModel, NavigationInputModel, isStandardGamepad, type NavigationAction } from "./input-model";
import styles from "./immersive.module.css";

export type HelpAction = Readonly<{ button: string; label: string }>;

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

export function ImmersiveShell({ children, help, inputEpoch, onAction, source = browserGamepadSource }: {
  children: ReactNode;
  help: readonly HelpAction[];
  inputEpoch?: string | number;
  onAction: (action: NavigationAction) => void;
  source?: GamepadFrameSource;
}) {
  const router = useRouter();
  const supportedViewport = useSupportedViewport();
  const [controllerReady, setControllerReady] = useState(false);
  const [clock, setClock] = useState(() => new Date());
  const modelRef = useRef(new NavigationInputModel());
  const claimRef = useRef(new GamepadClaimModel());
  const handleAction = useEffectEvent(onAction);

  useEffect(() => {
    const timer = window.setInterval(() => setClock(new Date()), 30_000);
    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    function onFrame(frame: GamepadFrame) {
      if (frame.suspended) {
        modelRef.current.reset(120);
        setControllerReady(false);
        return;
      }
      let activeIndex = getActiveImmersiveGamepadIndex();
      if (activeIndex === null) {
        const claim = claimRef.current.update(frame.gamepads);
        if (claim.claimedIndex === null) {return;}
        activeIndex = claim.claimedIndex;
        setActiveImmersiveGamepadIndex(activeIndex);
        modelRef.current.reset(120);
      }
      const candidate = frame.gamepads.find((item) => item.index === activeIndex) ?? null;
      const gamepad = candidate && isStandardGamepad(candidate) ? candidate : null;
      if (!gamepad) {
        setActiveImmersiveGamepadIndex(null);
        setControllerReady(false);
        modelRef.current.reset(120);
        claimRef.current.reset(frame.gamepads);
        return;
      }
      setControllerReady(true);
      const update = modelRef.current.update(gamepad, frame.nowMs);
      if (update.neutralReady) {
        for (const action of update.actions) {handleAction(action);}
      }
    }
    return source.subscribe(onFrame);
  }, [source]);

  useEffect(() => {
    modelRef.current.reset(120);
  }, [inputEpoch]);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      const action = keyboardAction(event.key);
      if (!action) {return;}
      event.preventDefault();
      if (!supportedViewport && (action === "cancel" || action === "confirm")) {
        setActiveImmersiveGamepadIndex(null);
        router.replace("/");
        return;
      }
      setControllerReady(true);
      handleAction(action);
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [router, supportedViewport]);

  return <main className={styles.shell} data-immersive-shell="true">
    <header className={styles.shellHeader}>
      <div><strong>RETROM</strong><span>/</span><span>沉浸模式</span></div>
      <time dateTime={clock.toISOString()}>{formatClock(clock)}</time>
    </header>
    <div className={styles.shellContent}>{children}</div>
    <footer className={styles.helpBar} aria-label="手柄操作提示">
      {help.map((item) => <span key={`${item.button}:${item.label}`}><kbd>{item.button}</kbd>{item.label}</span>)}
    </footer>
    {!controllerReady ? <section className={styles.controllerOverlay} role="status" aria-live="polite">
      <div><span className={styles.controllerGlyph} aria-hidden="true">⌁</span><h2>等待手柄</h2><p>按下标准布局手柄上的任意按键以继续。</p><Link href="/" onClick={() => setActiveImmersiveGamepadIndex(null)}>返回首页</Link></div>
    </section> : null}
    {!supportedViewport ? <section className={styles.viewportOverlay} role="alert">
      <div><h2>沉浸模式需要横屏大屏</h2><p>请使用至少 960 × 540 的横屏视口。</p><button type="button" onClick={() => {setActiveImmersiveGamepadIndex(null); router.replace("/");}}>返回普通首页</button></div>
    </section> : null}
  </main>;
}
