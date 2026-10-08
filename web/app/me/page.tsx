"use client";
import Link from "next/link";
import { useAuth } from "@/features/auth/auth-provider";
import { PageHeader } from "@/components/ui";
import { AppIcon } from "@/components/app-icon";
export default function Page() {
  const { context, logout } = useAuth();
  return (
    <>
      <PageHeader title="我的" description="存档、收藏与账户设置。" />
      <div className="stack">
        <Link className="workspace-row" href="/saves">
          <AppIcon className="nav-icon" name="save" />
          我的存档
        </Link>
        <Link className="workspace-row" href="/favorites">
          <AppIcon className="nav-icon" name="heart" />
          我的收藏
        </Link>
        <Link className="workspace-row" href="/recent">
          <AppIcon className="nav-icon" name="history" />
          最近游玩
        </Link>
        <Link className="workspace-row" href="/account">
          <AppIcon className="nav-icon" name="user" />
          账户设置
        </Link>
        {context?.user?.role === "admin" ? (
          <Link className="workspace-row" href="/admin/games">
            <AppIcon className="nav-icon" name="settings" />
            管理游戏库
          </Link>
        ) : null}
        <button className="button secondary" onClick={() => void logout()}>
          退出登录
        </button>
      </div>
    </>
  );
}
