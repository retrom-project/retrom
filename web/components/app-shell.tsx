"use client";
import Link from "next/link";
import { PhoneShell } from "./phone-shell";
import { usePhoneLayout } from "@/lib/use-phone-layout";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import type { ReactNode } from "react";
import { AppIcon } from "./app-icon";
import type { AppIconName } from "./app-icon";
import { ResponsiveSheet } from "./responsive-sheet";
import { ServiceHealth } from "./service-health";
import { SidebarAccount } from "./sidebar-account";
import { useAuth } from "@/features/auth/auth-provider";
import { authenticationReturnPath } from "@/features/auth/return-path";
const userNav: Array<[string, string, AppIconName]> = [
  ["/", "首页", "home"],
  ["/library", "游戏库", "library"],
  ["/saves", "我的存档", "save"],
  ["/favorites", "我的收藏", "heart"],
  ["/recent", "最近游玩", "history"],
];
const adminNav: Array<[string, string, AppIconName]> = [
  ["/admin/imports", "游戏入库", "download"],
  ["/admin/imports/server", "来源扫描", "database"],
  ["/admin/reviews", "待审核", "check"],
  ["/admin/games", "游戏管理", "library"],
  ["/admin/tags", "标签管理", "list"],
  ["/admin/platform-instances", "游戏目录", "folder"],
  ["/admin/bios", "运行依赖", "chip"],
  ["/admin/users", "用户管理", "user"],
];
const publicRoutes = ["/login", "/setup", "/register", "/reset-password"];
export function AppShell({ children }: { children: ReactNode }) {
  const pathname = usePathname();
  const phone = usePhoneLayout();
  const router = useRouter();
  const { context, error, refresh } = useAuth();
  const publicRoute = publicRoutes.includes(pathname);
  useEffect(() => {
    if (!context) {
      return;
    }
    if (!context.initialized && pathname !== "/setup") {
      router.replace("/setup");
    } else if (context.initialized && !context.user && !publicRoute) {
      router.replace(`/login?returnTo=${encodeURIComponent(pathname)}`);
    } else if (context.user && publicRoute) {
      router.replace(authenticationReturnPath(window.location.search));
    }
  }, [context, pathname, publicRoute, router]);
  if (error) {
    return (
      <main className="auth-route-message" role="alert">
        <h1>服务暂不可用</h1>
        <p>{error}</p>
        <button className="button" onClick={() => void refresh()}>
          重新连接
        </button>
      </main>
    );
  }
  if (!context) {
    return (
      <main className="auth-route-loading" role="status">
        正在确认账号状态…
      </main>
    );
  }
  if (publicRoute) {
    return <>{children}</>;
  }
  if (!context.user || !context.initialized) {
    return (
      <main className="auth-route-loading" role="status">
        正在打开账号入口…
      </main>
    );
  }
  if (pathname.startsWith("/admin") && context.user.role !== "admin") {
    return (
      <main className="auth-route-message">
        <h1>没有管理权限</h1>
        <Link className="button" href="/">
          返回首页
        </Link>
      </main>
    );
  }
  if (pathname.startsWith("/play/") || pathname.startsWith("/immersive")) {
    return <>{children}</>;
  }
  return phone && !pathname.startsWith("/admin") ? (
    <PhoneShell pathname={pathname}>{children}</PhoneShell>
  ) : (
    <StandardShell pathname={pathname}>{children}</StandardShell>
  );
}
function Navigation({
  pathname,
  close,
}: {
  pathname: string;
  close?: () => void;
}) {
  const items = pathname.startsWith("/admin") ? adminNav : userNav;
  return (
    <nav className="side-nav" aria-label="主要导航">
      {items.map(([href, title, icon]) => {
        const child = href === "/admin/imports/server" || href === "/admin/reviews";
        const active = pathname === href || (href !== "/" && href !== "/admin/imports" && pathname.startsWith(`${href}/`));
        const context = href === "/admin/imports" && !active && (pathname.startsWith("/admin/imports/") || pathname.startsWith("/admin/reviews"));
        return (
          <Link
            key={href}
            href={href}
            onClick={close}
            className={`nav-link${child ? " nav-child" : ""}${active ? " is-active" : ""}${context ? " is-context" : ""}`}
            aria-current={active ? "page" : undefined}
          >
            <AppIcon className="nav-icon" name={icon} />
            <span>{title}</span>
          </Link>
        );
      })}
    </nav>
  );
}
function StandardShell({
  pathname,
  children,
}: {
  pathname: string;
  children: ReactNode;
}) {
  const { context } = useAuth();
  const [menu, setMenu] = useState(false);
  const admin = pathname.startsWith("/admin");
  const title =
    [...userNav, ...adminNav].find(([href]) => href === pathname)?.[1] ??
    "Retrom";
  return (
    <div className={`app-frame${admin ? " admin-app-frame" : ""}`}>
      <aside className="sidebar">
        <Link className="brand" href="/">
          <span className="brand-mark">R</span>
          <span>
            <strong>Retrom</strong>
            <small>复古游戏管理平台</small>
          </span>
        </Link>
        <Navigation pathname={pathname} />
        <div className="sidebar-foot">
          <SidebarAccount />
          {context?.user?.role === "admin" ? (
            <Link
              className="context-switch"
              href={admin ? "/" : "/admin/imports"}
            >
              <AppIcon className="nav-icon" name={admin ? "arrow-left" : "settings"} />
              {admin ? "返回用户侧" : "管理后台"}
            </Link>
          ) : null}
        </div>
      </aside>
      <CompactHeader admin={admin} title={title} onMenu={() => setMenu(true)} />
      <div className="app-body">
        <main className="content">{children}</main>
      </div>
      <nav className="phone-navigation" aria-label="手机主导航">
        {[
          ["/", "首页", "home"],
          ["/library", "游戏库", "library"],
          ["/me", "我的", "user"],
        ].map(([href, label, icon]) => (
          <Link
            key={href}
            href={href}
            aria-current={pathname === href ? "page" : undefined}
          >
            <AppIcon name={icon as AppIconName} />
            <span>{label}</span>
          </Link>
        ))}
      </nav>
      <ResponsiveSheet
        open={menu}
        title="Retrom 导航"
        placement="left"
        onClose={() => setMenu(false)}
      >
        <Navigation pathname={pathname} close={() => setMenu(false)} />
        <Link
          className="button secondary"
          href={admin ? "/" : "/admin/imports"}
          onClick={() => setMenu(false)}
        >
          {admin ? "返回用户侧" : "管理后台"}
        </Link>
      </ResponsiveSheet>
    </div>
  );
}

function CompactHeader({
  admin,
  title,
  onMenu,
}: {
  admin: boolean;
  title: string;
  onMenu: () => void;
}) {
  const { context } = useAuth();
  return (
    <header className={`compact-app-bar is-${admin ? "admin" : "user"}`}>
      <button
        className="compact-nav-trigger"
        aria-label="打开主要导航"
        onClick={onMenu}
      >
        <AppIcon name="menu" />
      </button>
      <strong className="compact-page-title">{title}</strong>
      <div className="compact-app-actions">
        <ServiceHealth compact />
        <Link
          href="/account"
          className="compact-account-trigger"
          aria-label="账户设置"
        >
          <span>{context?.user?.displayName.slice(0, 1)}</span>
        </Link>
      </div>
    </header>
  );
}
