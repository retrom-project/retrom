"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useState, type ReactNode } from "react";
import { AppIcon, type AppIconName } from "@/components/app-icon";

type NavItem = { href: string; label: string; icon: AppIconName; exact?: boolean; child?: boolean };

const userNavigation: NavItem[] = [
  { href: "/", label: "首页", icon: "home", exact: true },
  { href: "/library", label: "游戏库", icon: "library" },
  { href: "/saves", label: "我的存档", icon: "save" }
];

const adminNavigation: NavItem[] = [
  { href: "/admin/imports", label: "游戏入库", icon: "download", exact: true },
  { href: "/admin/imports/new", label: "导入文件 / 目录", icon: "plus", child: true },
  { href: "/admin/imports/tasks", label: "任务进度", icon: "clock", child: true },
  { href: "/admin/reviews", label: "待审核", icon: "check", exact: true, child: true },
  { href: "/admin/reviews/history", label: "审核历史", icon: "history", child: true },
  { href: "/admin/games", label: "游戏管理", icon: "library" },
  { href: "/admin/platform-instances", label: "平台目录", icon: "list" },
  { href: "/admin/bios", label: "BIOS 管理", icon: "chip", exact: true },
  { href: "/admin/bios/dats", label: "街机数据目录", icon: "database", child: true }
];

function navState(item: NavItem, pathname: string): "active" | "context" | "" {
  if (item.href === "/admin/imports" && pathname !== item.href &&
    (pathname.startsWith("/admin/imports") || pathname.startsWith("/admin/reviews"))) return "context";
  if (item.exact) return pathname === item.href ? "active" : "";
  return pathname === item.href || pathname.startsWith(`${item.href}/`) ? "active" : "";
}

function Navigation({ items, pathname }: { items: NavItem[]; pathname: string }) {
  return (
    <nav aria-label="主要导航" className="side-nav">
      {items.map((item) => {
        const state = navState(item, pathname);
        return <Link
          aria-current={state === "active" ? "page" : undefined}
          className={`nav-link ${item.child ? "nav-child" : ""} ${state === "active" ? "is-active" : ""} ${state === "context" ? "is-context" : ""}`}
          href={item.href}
          key={item.href}
        >
          <AppIcon className="nav-icon" name={item.icon} />
          <span>{item.label}</span>
        </Link>;
      })}
    </nav>
  );
}

function breadcrumbs(pathname: string) {
  const root = pathname.startsWith("/admin") ? "管理后台" : "我的游戏";
  const routes: Array<[string, string[]]> = [
    ["/admin/reviews/history", ["游戏入库", "审核历史"]],
    ["/admin/reviews/", ["游戏入库", "待审核", "审核条目"]],
    ["/admin/reviews", ["游戏入库", "待审核"]],
    ["/admin/imports/new", ["游戏入库", "导入内容"]],
    ["/admin/imports/tasks", ["游戏入库", "任务进度"]],
    ["/admin/imports", ["游戏入库"]],
    ["/admin/games/", ["游戏管理", "游戏详情"]],
    ["/admin/games", ["游戏管理"]],
    ["/admin/platform-instances", ["平台目录"]],
    ["/admin/bios/dats", ["BIOS 管理", "街机数据目录"]],
    ["/admin/bios", ["BIOS 管理"]],
    ["/games/", ["游戏详情"]],
    ["/library", ["游戏库"]],
    ["/saves", ["我的存档"]],
    ["/", ["首页"]]
  ];
  return [root, ...(routes.find(([prefix]) => prefix === "/" ? pathname === "/" : pathname === prefix || pathname.startsWith(prefix))?.[1] ?? [])];
}

function ServiceHealth() {
  const [state, setState] = useState<"checking" | "ready" | "unavailable">("checking");
  useEffect(() => {
    const controller = new AbortController();
    void fetch("/health/ready", { cache: "no-store", signal: controller.signal })
      .then((response) => setState(response.ok ? "ready" : "unavailable"))
      .catch((error: unknown) => { if (!(error instanceof DOMException && error.name === "AbortError")) setState("unavailable"); });
    return () => controller.abort();
  }, []);
  const label = state === "checking" ? "正在检查服务" : state === "ready" ? "服务正常" : "服务不可用";
  return <span className={`connection ${state}`} aria-live="polite"><i />{label}</span>;
}

export function AppShell({ children }: { children: ReactNode }) {
  const pathname = usePathname();
  if (pathname.startsWith("/play/")) return <>{children}</>;
  const administrator = pathname.startsWith("/admin");
  return (
    <div className="app-frame">
      <aside className="sidebar">
        <Link className="brand" href="/" aria-label="Retrom 首页">
          <span className="brand-mark" aria-hidden="true">R</span>
          <span><strong>Retrom</strong><small>复古游戏管理平台</small></span>
        </Link>
        <Navigation items={administrator ? adminNavigation : userNavigation} pathname={pathname} />
        <div className="sidebar-foot">
          <Link className="context-switch" href={administrator ? "/" : "/admin/imports"}>
            <span aria-hidden="true">{administrator ? "←" : "⚙"}</span>
            {administrator ? "返回用户侧" : "管理后台"}
          </Link>
        </div>
      </aside>
      <div className="app-body">
        <header className="topbar">
          <nav aria-label="面包屑" className="breadcrumb"><ol>{breadcrumbs(pathname).map((item, index) => <li key={`${item}-${index}`}>{index ? <span aria-hidden="true">/</span> : null}{item}</li>)}</ol></nav>
          <ServiceHealth />
        </header>
        <main className="content">{children}</main>
      </div>
    </div>
  );
}
