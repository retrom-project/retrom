"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import type { ReactNode } from "react";

type NavItem = { href: string; label: string; icon: string; exact?: boolean; child?: boolean };

const userNavigation: NavItem[] = [
  { href: "/", label: "首页", icon: "⌂", exact: true },
  { href: "/library", label: "游戏库", icon: "▦" },
  { href: "/saves", label: "我的存档", icon: "◫" }
];

const adminNavigation: NavItem[] = [
  { href: "/admin/imports", label: "游戏入库", icon: "⇩", exact: true },
  { href: "/admin/imports/new", label: "导入文件 / 目录", icon: "＋", child: true },
  { href: "/admin/imports/tasks", label: "任务进度", icon: "◴", child: true },
  { href: "/admin/reviews", label: "待审核", icon: "✓", exact: true, child: true },
  { href: "/admin/reviews/history", label: "审核历史", icon: "↺", child: true },
  { href: "/admin/games", label: "游戏管理", icon: "▦" },
  { href: "/admin/platform-instances", label: "平台目录", icon: "▤" },
  { href: "/admin/bios", label: "BIOS 管理", icon: "◇", exact: true },
  { href: "/admin/bios/dats", label: "Arcade DAT", icon: "≋", child: true }
];

function active(item: NavItem, pathname: string) {
  if (item.href === "/admin/imports" &&
    (pathname.startsWith("/admin/imports") || pathname.startsWith("/admin/reviews"))) return true;
  if (item.exact) return pathname === item.href;
  return pathname === item.href || pathname.startsWith(`${item.href}/`);
}

function Navigation({ items, pathname }: { items: NavItem[]; pathname: string }) {
  return (
    <nav aria-label="主要导航" className="side-nav">
      {items.map((item) => (
        <Link
          className={`nav-link ${item.child ? "nav-child" : ""} ${active(item, pathname) ? "is-active" : ""}`}
          href={item.href}
          key={item.href}
        >
          <span aria-hidden="true" className="nav-icon">{item.icon}</span>
          <span>{item.label}</span>
        </Link>
      ))}
    </nav>
  );
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
          <div className="system-state"><i />系统运行正常</div>
        </div>
      </aside>
      <div className="app-body">
        <header className="topbar">
          <span className="breadcrumb">{administrator ? "管理后台" : "我的游戏空间"}</span>
          <div className="top-actions">
            <span className="connection"><i />本机服务</span>
            <span className="avatar" aria-label="本地玩家">本</span>
          </div>
        </header>
        <main className="content">{children}</main>
      </div>
    </div>
  );
}
