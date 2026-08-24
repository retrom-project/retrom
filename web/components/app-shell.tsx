"use client";

import Link, { useLinkStatus } from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useRef, useState, type ReactNode, type RefObject } from "react";
import { AppIcon, type AppIconName } from "@/components/app-icon";
import { ResponsiveSheet } from "@/components/responsive-sheet";
import { useAuth } from "@/features/auth/auth-provider";
import type { AuthContext, AuthUser } from "@/features/auth/types";

type NavItem = { href: string; label: string; icon: AppIconName; exact?: boolean; child?: boolean };
type CompactPanel = "navigation" | "more" | "health" | "account" | null;

const userNavigation: NavItem[] = [
  { href: "/", label: "首页", icon: "home", exact: true },
  { href: "/library", label: "游戏库", icon: "library" },
  { href: "/saves", label: "我的存档", icon: "save" },
  { href: "/favorites", label: "我的收藏", icon: "heart" },
  { href: "/recent", label: "最近游玩", icon: "history" },
  { href: "/netplay", label: "联机游玩", icon: "gamepad" }
];

const adminNavigation: NavItem[] = [
  { href: "/admin/imports", label: "游戏入库", icon: "download", exact: true },
  { href: "/admin/imports/new", label: "导入游戏", icon: "plus", child: true },
  { href: "/admin/imports/server", label: "本地扫描", icon: "database", child: true },
  { href: "/admin/imports/tasks", label: "任务进度", icon: "clock", child: true },
  { href: "/admin/reviews", label: "待审核", icon: "check", exact: true, child: true },
  { href: "/admin/reviews/history", label: "审核历史", icon: "history", child: true },
  { href: "/admin/games", label: "游戏管理", icon: "library" },
  { href: "/admin/tags", label: "标签管理", icon: "list" },
  { href: "/admin/platform-instances", label: "游戏目录", icon: "list" },
  { href: "/admin/users", label: "用户管理", icon: "settings" },
  { href: "/admin/bios", label: "运行依赖", icon: "chip", exact: true },
  { href: "/admin/storage", label: "容量分析", icon: "storage", exact: true }
];

function navState(item: NavItem, pathname: string): "active" | "context" | "" {
  if (item.href === "/admin/imports" && pathname !== item.href &&
    (pathname.startsWith("/admin/imports") || pathname.startsWith("/admin/reviews"))) {return "context";}
  if (item.exact) {return pathname === item.href ? "active" : "";}
  return pathname === item.href || pathname.startsWith(`${item.href}/`) ? "active" : "";
}

function NavigationPending() {
  const { pending } = useLinkStatus();
  return pending ? <span className="button-spinner nav-pending" role="status" aria-label="正在加载" /> : null;
}

function Navigation({ items, pathname, label = "主要导航", onNavigate }: { items: NavItem[]; pathname: string; label?: string; onNavigate?: () => void }) {
  return (
    <nav aria-label={label} className="side-nav">
      {items.map((item) => {
        const state = navState(item, pathname);
        return <Link
          aria-current={state === "active" ? "page" : undefined}
          className={`nav-link ${item.child ? "nav-child" : ""} ${state === "active" ? "is-active" : ""} ${state === "context" ? "is-context" : ""}`}
          href={item.href}
          key={`${item.href}:${item.label}`}
          onClick={onNavigate}
        >
          <AppIcon className="nav-icon" name={item.icon} />
          <span>{item.label}</span>
          <NavigationPending />
        </Link>;
      })}
    </nav>
  );
}

type ServiceHealthState = { state: "checking" | "ready" | "unavailable"; detail: string };

function useServiceHealth(): ServiceHealthState {
  const [state, setState] = useState<"checking" | "ready" | "unavailable">("checking");
  const [detail, setDetail] = useState("正在检查服务状态");
  useEffect(() => {
    const controller = new AbortController();
    void fetch("/health/ready", { cache: "no-store", signal: controller.signal })
      .then(async (response) => {
        setState(response.ok ? "ready" : "unavailable");
        if (response.ok) { setDetail("服务正常"); return; }
        const payload = await response.json().catch(() => null) as { error?: { message?: string }; checks?: Record<string, string> } | null;
        const checks = Object.entries(payload?.checks ?? {}).map(([name, value]) => `${name}：${value}`).join("；");
        setDetail(payload?.error?.message ?? (checks || `服务异常（HTTP ${response.status}）`));
      })
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") {return;}
        setState("unavailable"); setDetail(error instanceof Error ? `服务连接失败：${error.message}` : "服务连接失败");
      });
    return () => controller.abort();
  }, []);
  return { state, detail };
}

function ServiceHealth({ health, onClick, buttonRef }: { health: ServiceHealthState; onClick?: () => void; buttonRef?: RefObject<HTMLButtonElement | null> }) {
  const { state, detail } = health;
  const label = state === "checking" ? "正在检查服务" : state === "ready" ? "服务正常" : "服务存在异常";
  if (onClick) {return <button ref={buttonRef} className={`connection compact-health ${state}`} type="button" aria-label={label} onClick={onClick}><i aria-hidden="true" /></button>;}
  return <span className={`connection ${state}`} aria-live="polite" tabIndex={0}><i aria-hidden="true" /><span className="connection-tooltip" role="tooltip"><strong>{label}</strong><small>{detail}</small></span></span>;
}

const exactPageTitles = new Map<string, string>([
  ["/", "首页"],
  ["/library", "游戏库"],
  ["/saves", "我的存档"],
  ["/favorites", "我的收藏"],
  ["/recent", "最近游玩"],
  ["/netplay", "联机游玩"],
  ["/account", "账户设置"],
  ["/admin/imports/server", "本地扫描"],
  ["/admin/imports/new", "导入游戏"],
  ["/admin/imports/tasks", "任务进度"],
  ["/admin/imports", "游戏入库"],
  ["/admin/reviews/history", "审核历史"],
  ["/admin/reviews", "待审核"],
  ["/admin/games", "游戏管理"],
  ["/admin/tags", "标签管理"],
  ["/admin/platform-instances", "游戏目录"],
  ["/admin/users", "用户管理"],
  ["/admin/bios", "运行依赖"],
  ["/admin/storage", "容量分析"],
]);

const prefixPageTitles: Array<[string, string]> = [
  ["/admin/imports/server/pegasus/", "Pegasus 导入详情"],
  ["/admin/imports/server/", "服务器导入详情"],
  ["/admin/reviews/", "审核详情"],
  ["/admin/games/", "游戏管理详情"],
  ["/netplay/rooms/", "联机房间"],
  ["/games/", "游戏详情"],
];

function pageTitle(pathname: string) {
  const exact = exactPageTitles.get(pathname);
  if (exact) {return exact;}
  return prefixPageTitles.find(([prefix]) => pathname.startsWith(prefix))?.[1] ?? "Retrom";
}

function mobileSection(pathname: string): "home" | "library" | "saves" | "favorites" | "more" {
  if (pathname === "/") {return "home";}
  if (pathname === "/library" || pathname.startsWith("/games/")) {return "library";}
  if (pathname === "/saves") {return "saves";}
  if (pathname === "/favorites") {return "favorites";}
  return "more";
}

function usesStandaloneShell(pathname: string) {
  return pathname.startsWith("/play/") || pathname === "/immersive" || pathname.startsWith("/immersive/");
}

export function AppShell({ children }: { children: ReactNode }) {
  const pathname = usePathname();
  if (usesStandaloneShell(pathname)) {return <>{children}</>;}
  return <StandardAppShell pathname={pathname}>{children}</StandardAppShell>;
}

function StandardAppShell({ children, pathname }: { children: ReactNode; pathname: string }) {
  const { context, logout } = useAuth();
  const health = useServiceHealth();
  const accountMenuRef = useRef<HTMLDetailsElement>(null);
  const navigationButtonRef = useRef<HTMLButtonElement>(null);
  const moreButtonRef = useRef<HTMLButtonElement>(null);
  const healthButtonRef = useRef<HTMLButtonElement>(null);
  const accountButtonRef = useRef<HTMLButtonElement>(null);
  const [compactPanelState, setCompactPanelState] = useState<{ pathname: string; panel: CompactPanel }>(() => ({ pathname, panel: null }));
  const compactPanel = compactPanelState.pathname === pathname ? compactPanelState.panel : null;
  const setCompactPanel = (panel: CompactPanel) => setCompactPanelState({ pathname, panel });
  useEffect(() => {
    const closeAccountMenu = (event: PointerEvent) => {
      const menu = accountMenuRef.current;
      if (menu?.open && event.target instanceof Node && !menu.contains(event.target)) {
        menu.open = false;
      }
    };
    document.addEventListener("pointerdown", closeAccountMenu);
    return () => document.removeEventListener("pointerdown", closeAccountMenu);
  }, []);
  const publicRoute = ["/setup", "/login", "/register", "/reset-password"].includes(pathname);
  if (context.instanceState === "INITIALIZATION_REQUIRED") {
    return pathname === "/setup" ? <>{children}</> : <FullScreenLoading />;
  }
  if (context.authenticationState !== "AUTHENTICATED") {
    return publicRoute ? <>{children}</> : <FullScreenLoading />;
  }
  if (publicRoute) {return <FullScreenLoading />;}
  if (pathname.startsWith("/admin/review-previews/") && context.user?.role === "ADMIN") {return <>{children}</>;}
  if (pathname.startsWith("/admin") && context.user?.role !== "ADMIN") {
    return <Forbidden />;
  }
  const administrator = pathname.startsWith("/admin");
  const user = context.user;
  const visibleUserNavigation = userNavigation.filter((item) => item.href !== "/netplay" || context.netplayEnabled);
  const section = mobileSection(pathname);
  const navigationItems = administrator ? adminNavigation : visibleUserNavigation;
  return <AppFrame {...{
    accountButtonRef, accountMenuRef, administrator, children, compactPanel, context, health, healthButtonRef,
    logout, moreButtonRef, navigationButtonRef, navigationItems, pathname, section, setCompactPanel, user,
  }} />;
}

type AppFrameProps = {
  accountButtonRef: RefObject<HTMLButtonElement | null>;
  accountMenuRef: RefObject<HTMLDetailsElement | null>;
  administrator: boolean;
  children: ReactNode;
  compactPanel: CompactPanel;
  context: AuthContext;
  health: ServiceHealthState;
  healthButtonRef: RefObject<HTMLButtonElement | null>;
  logout: () => Promise<void>;
  moreButtonRef: RefObject<HTMLButtonElement | null>;
  navigationButtonRef: RefObject<HTMLButtonElement | null>;
  navigationItems: NavItem[];
  pathname: string;
  section: ReturnType<typeof mobileSection>;
  setCompactPanel: (panel: CompactPanel) => void;
  user: AuthUser | null;
};

function AppFrame({
  accountButtonRef, accountMenuRef, administrator, children, compactPanel, context, health, healthButtonRef,
  logout, moreButtonRef, navigationButtonRef, navigationItems, pathname, section, setCompactPanel, user,
}: AppFrameProps) {
  return (
    <div className="app-frame">
      <DesktopSidebar {...{ accountMenuRef, administrator, health, logout, navigationItems, pathname, user }} />
      <CompactHeader {...{
        accountButtonRef, administrator, compactPanel, health, healthButtonRef, navigationButtonRef, pathname,
        setCompactPanel, user,
      }} />
      <div className="app-body">
        <main className="content">{children}</main>
      </div>
      <MobileBottomNavigation {...{ administrator, compactPanel, moreButtonRef, section, setCompactPanel }} />
      <CompactSheets {...{
        accountButtonRef, administrator, compactPanel, context, health, healthButtonRef, logout, moreButtonRef,
        navigationButtonRef, navigationItems, pathname, setCompactPanel, user,
      }} />
    </div>
  );
}

function DesktopSidebar({ accountMenuRef, administrator, health, logout, navigationItems, pathname, user }: {
  accountMenuRef: RefObject<HTMLDetailsElement | null>;
  administrator: boolean;
  health: ServiceHealthState;
  logout: () => Promise<void>;
  navigationItems: NavItem[];
  pathname: string;
  user: AuthUser | null;
}) {
  const canSwitchContext = administrator || user?.role === "ADMIN";
  return <aside className="sidebar">
    <Link className="brand" href="/" aria-label="Retrom 首页">
      <span className="brand-mark" aria-hidden="true">R</span>
      <span><strong>Retrom</strong><small>复古游戏管理平台</small></span>
    </Link>
    <Navigation items={navigationItems} pathname={pathname} />
    <div className="sidebar-foot">
      <div className="sidebar-account-row">
        <details className="account-menu" ref={accountMenuRef}>
          <summary>
            <span className="account-initial" aria-hidden="true">{user?.displayName.slice(0, 1).toUpperCase()}</span>
            <span className="account-copy"><strong>{user?.displayName}</strong><small>@{user?.username}</small></span>
          </summary>
          <div className="account-menu-popover">
            <Link href="/account">账户设置</Link>
            <button type="button" onClick={() => void logout()}><AppIcon name="log-out" />退出登录</button>
          </div>
        </details>
        <ServiceHealth health={health} />
      </div>
      {canSwitchContext ? <Link className="context-switch" href={administrator ? "/" : "/admin/imports"}>
        <AppIcon className="nav-icon" name={administrator ? "arrow-left" : "settings"} />
        {administrator ? "返回用户侧" : "管理后台"}
      </Link> : null}
    </div>
  </aside>;
}

function CompactHeader({
  accountButtonRef, administrator, compactPanel, health, healthButtonRef, navigationButtonRef, pathname,
  setCompactPanel, user,
}: Pick<AppFrameProps, "accountButtonRef" | "administrator" | "compactPanel" | "health" | "healthButtonRef" |
  "navigationButtonRef" | "pathname" | "setCompactPanel" | "user">) {
  return <header className={`compact-app-bar${administrator ? " is-admin" : " is-user"}`}>
    <button ref={navigationButtonRef} className="compact-nav-trigger" type="button" aria-label="打开主要导航" aria-expanded={compactPanel === "navigation"} aria-controls="compact-navigation-sheet" onClick={() => setCompactPanel("navigation")}><AppIcon name="menu" /></button>
    <Link className="compact-user-brand" href="/" aria-label="Retrom 首页"><span className="brand-mark" aria-hidden="true">R</span></Link>
    <strong className="compact-page-title">{pageTitle(pathname)}</strong>
    <div className="compact-app-actions">
      <ServiceHealth health={health} buttonRef={healthButtonRef} onClick={() => setCompactPanel("health")} />
      <button ref={accountButtonRef} className="compact-account-trigger" type="button" aria-label="打开账户菜单" aria-expanded={compactPanel === "account"} onClick={() => setCompactPanel("account")}><span aria-hidden="true">{user?.displayName.slice(0, 1).toUpperCase()}</span></button>
    </div>
  </header>;
}

function MobileBottomNavigation({ administrator, compactPanel, moreButtonRef, section, setCompactPanel }: Pick<
  AppFrameProps, "administrator" | "compactPanel" | "moreButtonRef" | "section" | "setCompactPanel"
>) {
  if (administrator) {return null;}
  const links: Array<[Exclude<AppFrameProps["section"], "more">, string, AppIconName, string]> = [
    ["home", "/", "home", "首页"],
    ["library", "/library", "library", "游戏库"],
    ["saves", "/saves", "save", "存档"],
    ["favorites", "/favorites", "heart", "收藏"],
  ];
  return <nav className="mobile-bottom-nav" aria-label="手机主导航">
    {links.map(([key, href, icon, label]) => <Link
      className={section === key ? "is-active" : ""}
      aria-current={section === key ? "page" : undefined}
      href={href}
      key={key}
    ><AppIcon name={icon} /><span>{label}</span></Link>)}
    <button ref={moreButtonRef} className={section === "more" ? "is-active" : ""} type="button" aria-label="更多导航" aria-pressed={section === "more"} aria-expanded={compactPanel === "more"} aria-controls="compact-more-sheet" onClick={() => setCompactPanel("more")}><AppIcon name="more" /><span>更多</span></button>
  </nav>;
}

function CompactSheets(props: Omit<AppFrameProps,
  "accountMenuRef" | "children" | "section"
>) {
  return <>
    <CompactNavigationSheet {...props} />
    <CompactMoreSheet {...props} />
    <CompactHealthSheet {...props} />
    <CompactAccountSheet {...props} />
  </>;
}

function CompactNavigationSheet({
  administrator, compactPanel, navigationButtonRef, navigationItems, pathname, setCompactPanel, user,
}: Pick<AppFrameProps, "administrator" | "compactPanel" | "navigationButtonRef" | "navigationItems" |
  "pathname" | "setCompactPanel" | "user">) {
  const canSwitchContext = administrator || user?.role === "ADMIN";
  return <ResponsiveSheet open={compactPanel === "navigation"} title={administrator ? "管理后台" : "Retrom 导航"} description={administrator ? "选择管理能力，或返回用户侧。" : "浏览资料库和账户能力。"} placement="left" onClose={() => setCompactPanel(null)} returnFocusRef={navigationButtonRef} className="compact-navigation-sheet">
    <div id="compact-navigation-sheet" className="compact-navigation-content">
      <Navigation items={navigationItems} pathname={pathname} label="紧凑主要导航" onNavigate={() => setCompactPanel(null)} />
      <div className="compact-navigation-foot">
        <Link href="/account" onClick={() => setCompactPanel(null)}><AppIcon name="settings" />账户设置</Link>
        {canSwitchContext ? <Link href={administrator ? "/" : "/admin/imports"} onClick={() => setCompactPanel(null)}><AppIcon name={administrator ? "arrow-left" : "settings"} />{administrator ? "返回用户侧" : "管理后台"}</Link> : null}
      </div>
    </div>
  </ResponsiveSheet>;
}

function healthLabel(state: ServiceHealthState["state"], checkingLabel = "正在检查") {
  if (state === "ready") {return "服务正常";}
  if (state === "checking") {return checkingLabel;}
  return "服务存在异常";
}

function CompactMoreSheet({
  compactPanel, context, health, logout, moreButtonRef, setCompactPanel, user,
}: Pick<AppFrameProps, "compactPanel" | "context" | "health" | "logout" | "moreButtonRef" |
  "setCompactPanel" | "user">) {
  return <ResponsiveSheet open={compactPanel === "more"} title="更多" description="最近游玩、联机和账户能力。" placement="bottom" onClose={() => setCompactPanel(null)} returnFocusRef={moreButtonRef} className="compact-more-sheet">
    <div id="compact-more-sheet" className="compact-action-list">
      <Link href="/recent" onClick={() => setCompactPanel(null)}><AppIcon name="history" /><span><strong>最近游玩</strong><small>查看游玩历史与累计时长</small></span></Link>
      {context.netplayEnabled ? <Link href="/netplay" onClick={() => setCompactPanel(null)}><AppIcon name="gamepad" /><span><strong>联机游玩</strong><small>创建或加入同源房间</small></span></Link> : null}
      <Link href="/account" onClick={() => setCompactPanel(null)}><AppIcon name="settings" /><span><strong>账户设置</strong><small>{user?.displayName} · @{user?.username}</small></span></Link>
      {user?.role === "ADMIN" ? <Link href="/admin/imports" onClick={() => setCompactPanel(null)}><AppIcon name="settings" /><span><strong>管理后台</strong><small>入库、审核和运行依赖</small></span></Link> : null}
      <button type="button" onClick={() => setCompactPanel("health")}><AppIcon name="chip" /><span><strong>服务状态</strong><small>{healthLabel(health.state)}</small></span></button>
      <button className="is-danger" type="button" onClick={() => void logout()}><AppIcon name="log-out" /><span><strong>退出登录</strong><small>结束当前浏览器会话</small></span></button>
    </div>
  </ResponsiveSheet>;
}

function CompactHealthSheet({ compactPanel, health, healthButtonRef, setCompactPanel }: Pick<
  AppFrameProps, "compactPanel" | "health" | "healthButtonRef" | "setCompactPanel"
>) {
  return <ResponsiveSheet open={compactPanel === "health"} title="服务状态" description="当前 Retrom 后端就绪检查。" placement="bottom" onClose={() => setCompactPanel(null)} returnFocusRef={healthButtonRef} className="compact-info-sheet">
    <div className={`compact-health-detail is-${health.state}`} role="status"><i aria-hidden="true" /><div><strong>{healthLabel(health.state, "正在检查服务")}</strong><p>{health.detail}</p></div></div>
  </ResponsiveSheet>;
}

function CompactAccountSheet({ accountButtonRef, compactPanel, logout, setCompactPanel, user }: Pick<
  AppFrameProps, "accountButtonRef" | "compactPanel" | "logout" | "setCompactPanel" | "user"
>) {
  return <ResponsiveSheet open={compactPanel === "account"} title="账户" description={`${user?.displayName} · @${user?.username}`} placement="bottom" onClose={() => setCompactPanel(null)} returnFocusRef={accountButtonRef} className="compact-account-sheet">
    <div className="compact-action-list">
      <Link href="/account" onClick={() => setCompactPanel(null)}><AppIcon name="settings" /><span><strong>账户设置</strong><small>修改密码和查看身份</small></span></Link>
      <button className="is-danger" type="button" onClick={() => void logout()}><AppIcon name="log-out" /><span><strong>退出登录</strong><small>结束当前浏览器会话</small></span></button>
    </div>
  </ResponsiveSheet>;
}

function FullScreenLoading() {
  return <div className="auth-route-loading" role="status"><span className="button-spinner" />正在确认账号状态…</div>;
}

function Forbidden() {
  return <main className="auth-route-message"><span aria-hidden="true">403</span><h1 tabIndex={-1}>没有管理权限</h1><p>用户管理仅包含账号与安全状态；游玩记录和存档保持私有。</p><Link className="button" href="/">返回首页</Link></main>;
}
