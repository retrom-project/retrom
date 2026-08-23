import type { AppIconName } from "@/components/app-icon";

export type NavItem = Readonly<{
  href: string;
  label: string;
  icon: AppIconName;
  exact?: boolean;
  child?: boolean;
}>;

export const userNavigation: readonly NavItem[] = [
  { href: "/", label: "首页", icon: "home", exact: true },
  { href: "/library", label: "游戏库", icon: "library" },
  { href: "/saves", label: "我的存档", icon: "save" },
  { href: "/favorites", label: "我的收藏", icon: "heart" },
  { href: "/recent", label: "最近游玩", icon: "history" },
  { href: "/netplay", label: "联机游玩", icon: "gamepad" },
] as const;

export const adminNavigation: readonly NavItem[] = [
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
  { href: "/admin/storage", label: "容量分析", icon: "storage", exact: true },
] as const;
