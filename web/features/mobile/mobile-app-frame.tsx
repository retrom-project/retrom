"use client";

import Link from "next/link";
import type { ReactNode } from "react";
import { AppIcon, type AppIconName } from "@/components/app-icon";
import { useAuth } from "@/features/auth/auth-provider";

export function phoneSection(pathname: string) {
  if (pathname === "/") {return "home";}
  if (pathname === "/library" || pathname.startsWith("/games/")) {return "library";}
  return "me";
}

export function MobileAppFrame({ children, pathname }: { children: ReactNode; pathname: string }) {
  const { context } = useAuth();
  const section = phoneSection(pathname);
  const detail = pathname.startsWith("/games/");
  const administrator = pathname.startsWith("/admin/") || pathname === "/admin";
  const secondary = section === "me" && pathname !== "/me";
  const backTo = detail || administrator ? "/library" : "/me";
  const links: Array<{ key: string; href: string; label: string; icon: AppIconName }> = [
    { key: "home", href: "/", label: "首页", icon: "home" },
    { key: "library", href: "/library", label: "游戏库", icon: "library" },
    { key: "me", href: "/me", label: "我的", icon: "user" },
  ];
  return <div className="phone-app-frame">
    <header className="phone-app-header">
      {detail || secondary || administrator
        ? <Link href={backTo} className="phone-back"><AppIcon name="arrow-left" />{backTo === "/library" ? "游戏库" : "我的"}</Link>
        : <Link href="/" className="phone-brand" aria-label="Retrom 首页"><span className="brand-mark" aria-hidden="true">R</span><strong>Retrom</strong></Link>}
      <Link href="/me" className="phone-avatar" aria-label="打开我的"><span aria-hidden="true">{context.user?.displayName.slice(0, 1).toUpperCase()}</span></Link>
    </header>
    <main className="phone-content">
      {administrator ? <section className="phone-admin-notice">
        <AppIcon name="library" />
        <h1>请在电脑上管理游戏库</h1>
        <p>导入、审核和维护适合在大屏幕上完成。现在可以先挑一款游戏来玩。</p>
        <Link className="button" href="/library">返回游戏库</Link>
      </section> : children}
    </main>
    <nav className="phone-bottom-nav" aria-label="手机主导航">
      {links.map((link) => <Link href={link.href} key={link.key} aria-current={section === link.key ? "page" : undefined}>
        <AppIcon name={link.icon} /><span>{link.label}</span>
      </Link>)}
    </nav>
  </div>;
}
