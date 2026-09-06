"use client";

import Link from "next/link";
import { AppIcon, type AppIconName } from "@/components/app-icon";
import { useAuth } from "@/features/auth/auth-provider";

const destinations: Array<{ href: string; label: string; icon: AppIconName }> = [
  { href: "/favorites", label: "我的收藏", icon: "heart" },
  { href: "/saves", label: "我的存档", icon: "save" },
  { href: "/recent", label: "最近游玩", icon: "history" },
];

export function MobileProfile() {
  const { context, logout } = useAuth();
  return <div className="phone-profile page-layout">
    <h1>我的</h1>
    <div className="phone-profile-identity"><span aria-hidden="true">{context.user?.displayName.slice(0, 1).toUpperCase()}</span><div><strong>{context.user?.displayName}</strong><p>@{context.user?.username}</p></div></div>
    <nav className="phone-profile-links" aria-label="个人游戏资料">
      {destinations.map((item) => <Link key={item.href} href={item.href}><AppIcon name={item.icon} /><span>{item.label}</span></Link>)}
      {context.netplayEnabled ? <Link href="/netplay"><AppIcon name="gamepad" /><span>联机游玩</span></Link> : null}
    </nav>
    <div className="phone-profile-links">
      <Link href="/account"><AppIcon name="settings" /><span>账户设置</span></Link>
      <button type="button" onClick={() => void logout()}><AppIcon name="log-out" /><span>退出登录</span></button>
    </div>
  </div>;
}
