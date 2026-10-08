"use client";
import Link from "next/link";
import type { ReactNode } from "react";
import { AppIcon } from "./app-icon";
import type { AppIconName } from "./app-icon";
import { useAuth } from "@/features/auth/auth-provider";
export function PhoneShell({
  pathname,
  children,
}: {
  pathname: string;
  children: ReactNode;
}) {
  const { context } = useAuth();
  const detail = pathname.startsWith("/games/");
  return (
    <div className="phone-app-frame">
      <header className="phone-app-header">
        {detail ? (
          <Link className="phone-back" href="/library">
            <AppIcon name="arrow-left" />
            <strong>游戏库</strong>
          </Link>
        ) : (
          <Link className="phone-brand" href="/">
            <span className="brand-mark">R</span>
            <strong>Retrom</strong>
          </Link>
        )}
        <Link className="phone-avatar" href="/account" aria-label="账户设置">
          <span>
            <span className="phone-avatar-initial">
              {context?.user?.displayName.slice(0, 1)}
            </span>
          </span>
        </Link>
      </header>
      <main className="phone-content">{children}</main>
      <nav className="phone-bottom-nav" aria-label="手机主导航">
        {links.map(([href, label, icon]) => (
          <Link
            key={href}
            href={href}
            aria-current={active(pathname, href) ? "page" : undefined}
          >
            <AppIcon name={icon} />
            <span>{label}</span>
          </Link>
        ))}
      </nav>
    </div>
  );
}
const links: Array<[string, string, AppIconName]> = [
  ["/", "首页", "home"],
  ["/library", "游戏库", "library"],
  ["/me", "我的", "user"],
];
function active(path: string, href: string) {
  if (href === "/library") {
    return path === "/library" || path.startsWith("/games/");
  }
  if (href === "/me") {
    return ["/me", "/account", "/saves", "/favorites", "/recent"].includes(
      path,
    );
  }
  return path === href;
}
