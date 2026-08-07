"use client";

import Link, { useLinkStatus } from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useState, type ReactNode } from "react";
import { AppIcon, type AppIconName } from "@/components/app-icon";

type NavItem = { href: string; label: string; icon: AppIconName; exact?: boolean; child?: boolean };

const userNavigation: NavItem[] = [
  { href: "/", label: "首页", icon: "home", exact: true },
  { href: "/library", label: "游戏库", icon: "library" },
  { href: "/saves", label: "我的存档", icon: "save" },
  { href: "/recent", label: "最近游玩", icon: "history" }
];

const adminNavigation: NavItem[] = [
  { href: "/admin/imports", label: "游戏入库", icon: "download", exact: true },
  { href: "/admin/imports/new", label: "导入游戏", icon: "plus", child: true },
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

function NavigationPending() {
  const { pending } = useLinkStatus();
  return pending ? <span className="button-spinner nav-pending" role="status" aria-label="正在加载" /> : null;
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
          <NavigationPending />
        </Link>;
      })}
    </nav>
  );
}

function ServiceHealth() {
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
  const label = state === "checking" ? "正在检查服务" : state === "ready" ? "服务正常" : "服务存在异常";
  return <span className={`connection ${state}`} aria-label={`${label}：${detail}`} aria-live="polite" tabIndex={0}><i aria-hidden="true" /><span className="connection-tooltip" role="tooltip"><strong>{label}</strong><small>{detail}</small></span></span>;
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
          <div className="sidebar-foot-row"><Link className="context-switch" href={administrator ? "/" : "/admin/imports"}>
              <AppIcon className="nav-icon" name={administrator ? "arrow-left" : "settings"} />
              {administrator ? "返回用户侧" : "管理后台"}
            </Link><ServiceHealth /></div>
        </div>
      </aside>
      <div className="app-body">
        <main className="content">{children}</main>
      </div>
    </div>
  );
}
