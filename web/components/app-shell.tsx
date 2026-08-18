"use client";

import Link, { useLinkStatus } from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useRef, useState, type ReactNode, type RefObject } from "react";
import { AppIcon, type AppIconName } from "@/components/app-icon";
import { ResponsiveSheet } from "@/components/responsive-sheet";
import { useAuth } from "@/features/auth/auth-provider";

type NavItem = { href: string; label: string; icon: AppIconName; exact?: boolean; child?: boolean };

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
  { href: "/admin/bios", label: "运行依赖", icon: "chip", exact: true }
];

function navState(item: NavItem, pathname: string): "active" | "context" | "" {
  if (item.href === "/admin/imports" && pathname !== item.href &&
    (pathname.startsWith("/admin/imports") || pathname.startsWith("/admin/reviews"))) return "context";
  if (item.exact) return pathname === item.href ? "active" : "";
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
        if (error instanceof DOMException && error.name === "AbortError") return;
        setState("unavailable"); setDetail(error instanceof Error ? `服务连接失败：${error.message}` : "服务连接失败");
      });
    return () => controller.abort();
  }, []);
  return { state, detail };
}

function ServiceHealth({ health, onClick, buttonRef }: { health: ServiceHealthState; onClick?: () => void; buttonRef?: RefObject<HTMLButtonElement | null> }) {
  const { state, detail } = health;
  const label = state === "checking" ? "正在检查服务" : state === "ready" ? "服务正常" : "服务存在异常";
  if (onClick) return <button ref={buttonRef} className={`connection compact-health ${state}`} type="button" aria-label={label} onClick={onClick}><i aria-hidden="true" /></button>;
  return <span className={`connection ${state}`} aria-live="polite" tabIndex={0}><i aria-hidden="true" /><span className="connection-tooltip" role="tooltip"><strong>{label}</strong><small>{detail}</small></span></span>;
}

function pageTitle(pathname: string) {
  if (pathname === "/") return "首页";
  if (pathname.startsWith("/games/")) return "游戏详情";
  if (pathname === "/library") return "游戏库";
  if (pathname === "/saves") return "我的存档";
  if (pathname === "/favorites") return "我的收藏";
  if (pathname === "/recent") return "最近游玩";
  if (pathname.startsWith("/netplay/rooms/")) return "联机房间";
  if (pathname === "/netplay") return "联机游玩";
  if (pathname === "/account") return "账户设置";
  if (pathname.startsWith("/admin/imports/server/pegasus/")) return "Pegasus 导入详情";
  if (pathname.startsWith("/admin/imports/server/")) return "服务器导入详情";
  if (pathname === "/admin/imports/server") return "本地扫描";
  if (pathname === "/admin/imports/new") return "导入游戏";
  if (pathname === "/admin/imports/tasks") return "任务进度";
  if (pathname === "/admin/imports") return "游戏入库";
  if (pathname === "/admin/reviews/history") return "审核历史";
  if (pathname.startsWith("/admin/reviews/")) return "审核详情";
  if (pathname === "/admin/reviews") return "待审核";
  if (pathname.startsWith("/admin/games/")) return "游戏管理详情";
  if (pathname === "/admin/games") return "游戏管理";
  if (pathname === "/admin/tags") return "标签管理";
  if (pathname === "/admin/platform-instances") return "游戏目录";
  if (pathname === "/admin/users") return "用户管理";
  if (pathname === "/admin/bios") return "运行依赖";
  return "Retrom";
}

function mobileSection(pathname: string): "home" | "library" | "saves" | "favorites" | "more" {
  if (pathname === "/") return "home";
  if (pathname === "/library" || pathname.startsWith("/games/")) return "library";
  if (pathname === "/saves") return "saves";
  if (pathname === "/favorites") return "favorites";
  return "more";
}

export function AppShell({ children }: { children: ReactNode }) {
  const pathname = usePathname();
  const { context, logout } = useAuth();
  const health = useServiceHealth();
  const accountMenuRef = useRef<HTMLDetailsElement>(null);
  const navigationButtonRef = useRef<HTMLButtonElement>(null);
  const moreButtonRef = useRef<HTMLButtonElement>(null);
  const healthButtonRef = useRef<HTMLButtonElement>(null);
  const accountButtonRef = useRef<HTMLButtonElement>(null);
  const [compactPanelState, setCompactPanelState] = useState<{ pathname: string; panel: "navigation" | "more" | "health" | "account" | null }>(() => ({ pathname, panel: null }));
  const compactPanel = compactPanelState.pathname === pathname ? compactPanelState.panel : null;
  const setCompactPanel = (panel: "navigation" | "more" | "health" | "account" | null) => setCompactPanelState({ pathname, panel });
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
  if (pathname.startsWith("/play/")) return <>{children}</>;
  const publicRoute = ["/setup", "/login", "/register", "/reset-password"].includes(pathname);
  if (context.instanceState === "INITIALIZATION_REQUIRED") {
    return pathname === "/setup" ? <>{children}</> : <FullScreenLoading />;
  }
  if (context.authenticationState !== "AUTHENTICATED") {
    return publicRoute ? <>{children}</> : <FullScreenLoading />;
  }
  if (publicRoute) return <FullScreenLoading />;
  if (pathname.startsWith("/admin/review-previews/") && context.user?.role === "ADMIN") return <>{children}</>;
  if (pathname.startsWith("/admin") && context.user?.role !== "ADMIN") {
    return <Forbidden />;
  }
  const administrator = pathname.startsWith("/admin");
  const user = context.user;
  const visibleUserNavigation = userNavigation.filter((item) => item.href !== "/netplay" || context.netplayEnabled);
  const section = mobileSection(pathname);
  const navigationItems = administrator ? adminNavigation : visibleUserNavigation;
  return (
    <div className="app-frame">
      <aside className="sidebar">
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
          {administrator || user?.role === "ADMIN" ? <Link className="context-switch" href={administrator ? "/" : "/admin/imports"}>
            <AppIcon className="nav-icon" name={administrator ? "arrow-left" : "settings"} />
            {administrator ? "返回用户侧" : "管理后台"}
          </Link> : null}
        </div>
      </aside>
      <header className={`compact-app-bar${administrator ? " is-admin" : " is-user"}`}>
        <button ref={navigationButtonRef} className="compact-nav-trigger" type="button" aria-label="打开主要导航" aria-expanded={compactPanel === "navigation"} aria-controls="compact-navigation-sheet" onClick={() => setCompactPanel("navigation")}><AppIcon name="menu" /></button>
        <Link className="compact-user-brand" href="/" aria-label="Retrom 首页"><span className="brand-mark" aria-hidden="true">R</span></Link>
        <strong className="compact-page-title">{pageTitle(pathname)}</strong>
        <div className="compact-app-actions">
          <ServiceHealth health={health} buttonRef={healthButtonRef} onClick={() => setCompactPanel("health")} />
          <button ref={accountButtonRef} className="compact-account-trigger" type="button" aria-label="打开账户菜单" aria-expanded={compactPanel === "account"} onClick={() => setCompactPanel("account")}><span aria-hidden="true">{user?.displayName.slice(0, 1).toUpperCase()}</span></button>
        </div>
      </header>
      <div className="app-body">
        <main className="content">{children}</main>
      </div>
      {!administrator ? <nav className="mobile-bottom-nav" aria-label="手机主导航">
        <Link className={section === "home" ? "is-active" : ""} aria-current={section === "home" ? "page" : undefined} href="/"><AppIcon name="home" /><span>首页</span></Link>
        <Link className={section === "library" ? "is-active" : ""} aria-current={section === "library" ? "page" : undefined} href="/library"><AppIcon name="library" /><span>游戏库</span></Link>
        <Link className={section === "saves" ? "is-active" : ""} aria-current={section === "saves" ? "page" : undefined} href="/saves"><AppIcon name="save" /><span>存档</span></Link>
        <Link className={section === "favorites" ? "is-active" : ""} aria-current={section === "favorites" ? "page" : undefined} href="/favorites"><AppIcon name="heart" /><span>收藏</span></Link>
        <button ref={moreButtonRef} className={section === "more" ? "is-active" : ""} type="button" aria-label="更多导航" aria-pressed={section === "more"} aria-expanded={compactPanel === "more"} aria-controls="compact-more-sheet" onClick={() => setCompactPanel("more")}><AppIcon name="more" /><span>更多</span></button>
      </nav> : null}

      <ResponsiveSheet open={compactPanel === "navigation"} title={administrator ? "管理后台" : "Retrom 导航"} description={administrator ? "选择管理能力，或返回用户侧。" : "浏览资料库和账户能力。"} placement="left" onClose={() => setCompactPanel(null)} returnFocusRef={navigationButtonRef} className="compact-navigation-sheet">
        <div id="compact-navigation-sheet" className="compact-navigation-content">
          <Navigation items={navigationItems} pathname={pathname} label="紧凑主要导航" onNavigate={() => setCompactPanel(null)} />
          <div className="compact-navigation-foot">
            <Link href="/account" onClick={() => setCompactPanel(null)}><AppIcon name="settings" />账户设置</Link>
            {administrator || user?.role === "ADMIN" ? <Link href={administrator ? "/" : "/admin/imports"} onClick={() => setCompactPanel(null)}><AppIcon name={administrator ? "arrow-left" : "settings"} />{administrator ? "返回用户侧" : "管理后台"}</Link> : null}
          </div>
        </div>
      </ResponsiveSheet>

      <ResponsiveSheet open={compactPanel === "more"} title="更多" description="最近游玩、联机和账户能力。" placement="bottom" onClose={() => setCompactPanel(null)} returnFocusRef={moreButtonRef} className="compact-more-sheet">
        <div id="compact-more-sheet" className="compact-action-list">
          <Link href="/recent" onClick={() => setCompactPanel(null)}><AppIcon name="history" /><span><strong>最近游玩</strong><small>查看游玩历史与累计时长</small></span></Link>
          {context.netplayEnabled ? <Link href="/netplay" onClick={() => setCompactPanel(null)}><AppIcon name="gamepad" /><span><strong>联机游玩</strong><small>创建或加入同源房间</small></span></Link> : null}
          <Link href="/account" onClick={() => setCompactPanel(null)}><AppIcon name="settings" /><span><strong>账户设置</strong><small>{user?.displayName} · @{user?.username}</small></span></Link>
          {user?.role === "ADMIN" ? <Link href="/admin/imports" onClick={() => setCompactPanel(null)}><AppIcon name="settings" /><span><strong>管理后台</strong><small>入库、审核和运行依赖</small></span></Link> : null}
          <button type="button" onClick={() => setCompactPanel("health")}><AppIcon name="chip" /><span><strong>服务状态</strong><small>{health.state === "ready" ? "服务正常" : health.state === "checking" ? "正在检查" : "服务存在异常"}</small></span></button>
          <button className="is-danger" type="button" onClick={() => void logout()}><AppIcon name="log-out" /><span><strong>退出登录</strong><small>结束当前浏览器会话</small></span></button>
        </div>
      </ResponsiveSheet>

      <ResponsiveSheet open={compactPanel === "health"} title="服务状态" description="当前 Retrom 后端就绪检查。" placement="bottom" onClose={() => setCompactPanel(null)} returnFocusRef={healthButtonRef} className="compact-info-sheet">
        <div className={`compact-health-detail is-${health.state}`} role="status"><i aria-hidden="true" /><div><strong>{health.state === "ready" ? "服务正常" : health.state === "checking" ? "正在检查服务" : "服务存在异常"}</strong><p>{health.detail}</p></div></div>
      </ResponsiveSheet>

      <ResponsiveSheet open={compactPanel === "account"} title="账户" description={`${user?.displayName} · @${user?.username}`} placement="bottom" onClose={() => setCompactPanel(null)} returnFocusRef={accountButtonRef} className="compact-account-sheet">
        <div className="compact-action-list">
          <Link href="/account" onClick={() => setCompactPanel(null)}><AppIcon name="settings" /><span><strong>账户设置</strong><small>修改密码和查看身份</small></span></Link>
          <button className="is-danger" type="button" onClick={() => void logout()}><AppIcon name="log-out" /><span><strong>退出登录</strong><small>结束当前浏览器会话</small></span></button>
        </div>
      </ResponsiveSheet>
    </div>
  );
}

function FullScreenLoading() {
  return <div className="auth-route-loading" role="status"><span className="button-spinner" />正在确认账号状态…</div>;
}

function Forbidden() {
  return <main className="auth-route-message"><span aria-hidden="true">403</span><h1 tabIndex={-1}>没有管理权限</h1><p>用户管理仅包含账号与安全状态；游玩记录和存档保持私有。</p><Link className="button" href="/">返回首页</Link></main>;
}
