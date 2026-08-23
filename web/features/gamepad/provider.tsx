"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { AppIcon } from "@/components/app-icon";
import { userNavigation } from "@/components/app-navigation";
import { useAuth } from "@/features/auth/auth-provider";
import { createBrowserControllerSource } from "./browser-source";
import {
  activateControllerFocus,
  changeEditableController,
  controllerBackInScope,
  controllerCandidates,
  controllerGroupAction,
  focusControllerDefault,
  focusControllerElement,
  moveControllerFocus,
} from "./focus-navigation";
import {
  initialNavigationControllerState,
  suspendNavigationController,
  updateNavigationController,
  type NavigationControllerState,
} from "./model";
import type { ControllerAction, ControllerSource } from "./types";

type InputMode = "pointer" | "keyboard" | "gamepad";

type GamepadContextValue = Readonly<{
  source: ControllerSource;
  activeIndex: number | null;
  centerButtonObservable: boolean;
  inputMode: InputMode;
  claim: (index: number, centerButtonObservable?: boolean) => void;
  releaseClaim: () => void;
}>;

const emptySource: ControllerSource = Object.freeze({ read: () => Object.freeze([]) });
const GamepadContext = createContext<GamepadContextValue>({
  source: emptySource,
  activeIndex: null,
  centerButtonObservable: false,
  inputMode: "pointer",
  claim: () => undefined,
  releaseClaim: () => undefined,
});

const excludedRoutes = new Set(["/setup", "/login", "/register", "/reset-password", "/account"]);

function controllerNavigationEnabled(pathname: string, authenticated: boolean) {
  return authenticated && !excludedRoutes.has(pathname) && !pathname.startsWith("/admin") &&
    !pathname.startsWith("/play/");
}

function userRoute(pathname: string) {
  return pathname === "/" || ["/library", "/games/", "/saves", "/favorites", "/recent", "/netplay"]
    .some((route) => pathname === route || pathname.startsWith(route));
}

function preferredLabels(pathname: string) {
  if (pathname === "/") {return ["继续游戏", "开始游戏", "浏览游戏库"];}
  if (pathname === "/library") {return ["查看", "游戏详情", "浏览全部游戏", "清除筛选"];}
  if (pathname.startsWith("/games/")) {return ["继续游戏", "开始游戏"];}
  if (pathname === "/saves") {return ["继续", "浏览游戏库"];}
  if (pathname === "/favorites") {return ["查看游戏详情", "浏览游戏库"];}
  if (pathname === "/recent") {return ["继续游戏", "查看游戏详情", "浏览游戏库"];}
  if (pathname === "/netplay") {return ["恢复房间", "创建房间"];}
  if (pathname.startsWith("/netplay/rooms/")) {return ["准备", "取消准备", "开始游戏", "离开房间"];}
  return [];
}

function focusRouteDefault(pathname: string) {
  const main = document.querySelector<HTMLElement>("main") ?? document.body;
  const candidates = controllerCandidates(main);
  const labels = preferredLabels(pathname);
  const preferred = labels.flatMap((label) => candidates.filter((candidate) =>
    (candidate.getAttribute("aria-label") ?? candidate.textContent ?? "").trim().includes(label))).at(0);
  if (preferred) {focusControllerElement(preferred); return preferred;}
  return focusControllerDefault(main);
}

export function useGamepad() {
  return useContext(GamepadContext);
}

export function GamepadProvider({ children, source }: { children: ReactNode; source?: ControllerSource }) {
  const pathname = usePathname();
  const router = useRouter();
  const { context } = useAuth();
  const browserSource = useRef<ControllerSource>(source ?? emptySource);
  const sourceProxy = useMemo<ControllerSource>(() => ({ read: () => browserSource.current.read() }), []);
  const controller = useRef<NavigationControllerState>(initialNavigationControllerState());
  const routeHistory = useRef<string[]>([]);
  const navigationTrigger = useRef<HTMLElement | null>(null);
  const [activeIndex, setActiveIndex] = useState<number | null>(null);
  const [centerButtonObservable, setCenterButtonObservable] = useState(false);
  const [inputMode, setInputMode] = useState<InputMode>("pointer");
  const [navigationOpen, setNavigationOpen] = useState(false);
  const [helpOpen, setHelpOpen] = useState(false);
  const [connectionNotice, setConnectionNotice] = useState(false);
  const [fullscreenPrompt, setFullscreenPrompt] = useState(false);
  const [fullscreenMessage, setFullscreenMessage] = useState("");
  const enabled = controllerNavigationEnabled(
    pathname,
    context.authenticationState === "AUTHENTICATED",
  );

  useEffect(() => {
    if (!source) {browserSource.current = createBrowserControllerSource();}
  }, [source]);

  useEffect(() => {
    if (!userRoute(pathname)) {return;}
    const history = routeHistory.current;
    if (history.at(-1) !== pathname) {history.push(pathname);}
    if (history.length > 32) {history.splice(0, history.length - 32);}
  }, [pathname]);

  useEffect(() => {
    document.documentElement.dataset.inputMode = inputMode;
    return () => {delete document.documentElement.dataset.inputMode;};
  }, [inputMode]);

  const claim = useCallback((index: number, center = false) => {
    controller.current = {
      ...initialNavigationControllerState(index),
      centerButtonObservable: center,
    };
    setActiveIndex(index);
    setCenterButtonObservable(center);
  }, []);

  const releaseClaim = useCallback(() => {
    controller.current = initialNavigationControllerState();
    setActiveIndex(null);
    setCenterButtonObservable(false);
  }, []);

  const safeBack = useCallback(() => {
    if (controllerBackInScope()) {return;}
    const history = routeHistory.current;
    if (history.at(-1) === pathname) {history.pop();}
    const target = [...history].reverse().find(userRoute) ?? "/";
    router.push(target);
  }, [pathname, router]);

  const openNavigation = useCallback(() => {
    navigationTrigger.current = document.activeElement instanceof HTMLElement
      ? document.activeElement
      : null;
    setNavigationOpen(true);
  }, []);

  const closeNavigation = useCallback(() => {
    setNavigationOpen(false);
    window.requestAnimationFrame(() => {
      if (navigationTrigger.current?.isConnected) {
        focusControllerElement(navigationTrigger.current);
      }
    });
  }, []);

  const dispatchAction = useCallback((action: ControllerAction) => {
    setInputMode("gamepad");
    if (action.type === "claimed") {
      setActiveIndex(action.index);
      setCenterButtonObservable(action.centerButtonObservable);
      setConnectionNotice(true);
      setFullscreenPrompt(document.fullscreenElement === null);
      window.setTimeout(() => setConnectionNotice(false), 3_000);
      return;
    }
    if (action.type === "disconnected") {
      setActiveIndex(null);
      setConnectionNotice(false);
      return;
    }
    if (action.type === "ready") {focusRouteDefault(pathname); return;}
    if (action.type === "navigation") {openNavigation(); return;}
    if (action.type === "confirm") {activateControllerFocus(); return;}
    if (action.type === "back") {safeBack(); return;}
    if (action.type === "previous-group") {controllerGroupAction(false); return;}
    if (action.type === "next-group") {controllerGroupAction(true); return;}
    if (!changeEditableController(action.direction)) {moveControllerFocus(action.direction);}
  }, [openNavigation, pathname, safeBack]);

  useEffect(() => {
    if (!enabled) {
      controller.current = suspendNavigationController(controller.current);
      return;
    }
    let frame = 0;
    const update = (now: number) => {
      if (document.visibilityState === "visible" && document.hasFocus()) {
        const result = updateNavigationController(controller.current, browserSource.current.read(), now);
        controller.current = result.state;
        result.actions.forEach(dispatchAction);
      }
      frame = window.requestAnimationFrame(update);
    };
    frame = window.requestAnimationFrame(update);
    const suspend = () => {controller.current = suspendNavigationController(controller.current);};
    window.addEventListener("blur", suspend);
    document.addEventListener("visibilitychange", suspend);
    return () => {
      window.cancelAnimationFrame(frame);
      window.removeEventListener("blur", suspend);
      document.removeEventListener("visibilitychange", suspend);
    };
  }, [dispatchAction, enabled]);

  useEffect(() => {
    const pointer = () => setInputMode("pointer");
    const keyboard = () => setInputMode("keyboard");
    document.addEventListener("pointermove", pointer, { passive: true });
    document.addEventListener("keydown", keyboard);
    return () => {
      document.removeEventListener("pointermove", pointer);
      document.removeEventListener("keydown", keyboard);
    };
  }, []);

  const value = useMemo<GamepadContextValue>(() => ({
    source: sourceProxy,
    activeIndex,
    centerButtonObservable,
    inputMode,
    claim,
    releaseClaim,
  }), [activeIndex, centerButtonObservable, claim, inputMode, releaseClaim, sourceProxy]);

  async function requestLoungeFullscreen() {
    try {
      await document.documentElement.requestFullscreen({ navigationUI: "hide" });
      setFullscreenPrompt(false);
    } catch {
      setFullscreenMessage("浏览器需要一次鼠标、触摸或键盘确认才能进入全屏；手柄导航仍可继续。");
    }
  }

  return <GamepadContext.Provider value={value}>
    {children}
    {enabled ? <>
      <ControllerNavigationPanel
        open={navigationOpen}
        pathname={pathname}
        netplayEnabled={context.netplayEnabled}
        onClose={closeNavigation}
        onHelp={() => {setNavigationOpen(false); setHelpOpen(true);}}
        onReplace={() => {releaseClaim(); closeNavigation();}}
      />
      <ControllerHelp open={helpOpen} activeIndex={activeIndex} centerButtonObservable={centerButtonObservable} onClose={() => setHelpOpen(false)} onReplace={() => {releaseClaim(); setHelpOpen(false);}} />
      <ControllerStatus inputMode={inputMode} connectionNotice={connectionNotice} fullscreenPrompt={fullscreenPrompt} fullscreenMessage={fullscreenMessage} onDismissFullscreen={() => setFullscreenPrompt(false)} onFullscreen={() => void requestLoungeFullscreen()} />
    </> : null}
  </GamepadContext.Provider>;
}

function ControllerNavigationPanel({ open, pathname, netplayEnabled, onClose, onHelp, onReplace }: {
  open: boolean;
  pathname: string;
  netplayEnabled: boolean;
  onClose: () => void;
  onHelp: () => void;
  onReplace: () => void;
}) {
  useEffect(() => {
    if (!open) {return;}
    const current = document.querySelector<HTMLElement>(".controller-navigation-panel [aria-current='page']");
    (current ?? focusControllerDefault(document.querySelector(".controller-navigation-panel") ?? document))?.focus();
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") {event.preventDefault(); onClose();}
    };
    document.addEventListener("keydown", closeOnEscape);
    return () => document.removeEventListener("keydown", closeOnEscape);
  }, [onClose, open]);
  if (!open) {return null;}
  return <div className="controller-overlay" data-gamepad-scope data-gamepad-open="true">
    <button className="controller-overlay-backdrop" type="button" tabIndex={-1} aria-label="关闭用户导航" onClick={onClose} />
    <aside className="controller-navigation-panel" role="dialog" aria-modal="true" aria-label="用户导航">
      <header><AppIcon name="gamepad" /><div><small>客厅模式</small><h2>用户导航</h2></div></header>
      <nav aria-label="手柄用户导航">
        {userNavigation.filter((item) => item.href !== "/netplay" || netplayEnabled).map((item) => {
          const current = item.exact ? pathname === item.href : pathname === item.href || pathname.startsWith(`${item.href}/`);
          return <Link key={item.href} href={item.href} aria-current={current ? "page" : undefined} data-gamepad-default={current ? "true" : undefined} onClick={onClose}><AppIcon name={item.icon} />{item.label}</Link>;
        })}
      </nav>
      <div className="controller-navigation-actions">
        <button type="button" onClick={onHelp}><AppIcon name="gamepad" />手柄帮助</button>
        <button type="button" onClick={onReplace}>更换导航手柄</button>
        <p>账户设置需要键盘或鼠标。</p>
      </div>
      <button className="controller-close" type="button" data-gamepad-back="true" onClick={onClose}>关闭</button>
    </aside>
  </div>;
}

function ControllerHelp({ open, activeIndex, centerButtonObservable, onClose, onReplace }: {
  open: boolean;
  activeIndex: number | null;
  centerButtonObservable: boolean;
  onClose: () => void;
  onReplace: () => void;
}) {
  useEffect(() => {
    if (!open) {return;}
    focusControllerDefault(document.querySelector(".controller-help-panel") ?? document);
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") {event.preventDefault(); onClose();}
    };
    document.addEventListener("keydown", closeOnEscape);
    return () => document.removeEventListener("keydown", closeOnEscape);
  }, [onClose, open]);
  if (!open) {return null;}
  return <div className="controller-overlay" data-gamepad-scope data-gamepad-open="true">
    <button className="controller-overlay-backdrop" type="button" tabIndex={-1} aria-label="关闭手柄帮助" onClick={onClose} />
    <section className="controller-help-panel" role="dialog" aria-modal="true" aria-labelledby="controller-help-title">
      <AppIcon name="gamepad" />
      <h2 id="controller-help-title">手柄状态与快捷键</h2>
      <p>{activeIndex === null ? "正在等待标准布局手柄" : `标准布局手柄 ${activeIndex + 1} 已连接`}</p>
      <dl><div><dt>方向键 / 左摇杆</dt><dd>移动</dd></div><div><dt>确认键</dt><dd>选择</dd></div><div><dt>返回键</dt><dd>返回或关闭</dd></div><div><dt>Start</dt><dd>用户导航</dd></div><div><dt>中心键</dt><dd>{centerButtonObservable ? "打开 Retrom 菜单" : "可能被系统占用"}</dd></div><div><dt>Select + Start</dt><dd>后备菜单</dd></div></dl>
      <footer><button type="button" data-gamepad-default="true" data-gamepad-back="true" onClick={onClose}>返回</button><button type="button" onClick={onReplace}>更换导航手柄</button></footer>
    </section>
  </div>;
}

function ControllerStatus({ inputMode, connectionNotice, fullscreenPrompt, fullscreenMessage, onDismissFullscreen, onFullscreen }: {
  inputMode: InputMode;
  connectionNotice: boolean;
  fullscreenPrompt: boolean;
  fullscreenMessage: string;
  onDismissFullscreen: () => void;
  onFullscreen: () => void;
}) {
  return <>
    <div className={`controller-connected-toast${connectionNotice ? " is-visible" : ""}`} role="status" aria-live="polite"><AppIcon name="gamepad" /><span><strong>手柄已连接</strong><small>方向移动 · 确认选择 · Start 导航</small></span></div>
    {fullscreenPrompt ? <section className="controller-fullscreen-prompt" aria-label="客厅全屏提示"><AppIcon name="gamepad" /><span role="status"><strong>手柄导航已就绪</strong><small>{fullscreenMessage || "浏览器需要一次鼠标、触摸或键盘确认才能进入全屏。"}</small></span><button type="button" data-gamepad-default="true" onClick={onDismissFullscreen}>暂不进入</button><button type="button" onClick={onFullscreen}>进入客厅全屏</button></section> : null}
    <div className={`controller-hint-bar${inputMode === "gamepad" ? " is-visible" : ""}`} aria-hidden={inputMode !== "gamepad"}><span>方向 · 移动</span><span>确认 · 选择</span><span>返回 · 返回</span><span>Start · 用户导航</span></div>
  </>;
}
