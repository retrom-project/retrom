import { ListFilters } from "@/components/list-filters";
import { AppIcon } from "@/components/app-icon";
import { EmptyState, PageHeader } from "@/components/ui";
import type { GameSummary } from "@/features/library/game-grid";
import { backendJSON, scalarSearchParams, withQuery, type ListResponse } from "@/lib/backend";
import Link from "next/link";
import Image from "next/image";

type AdminGame = GameSummary & { version: number; updatedAtMs: number };
export const metadata = { title: "游戏管理" };

export default async function AdminGamesPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const values = scalarSearchParams(await searchParams, ["q", "status"]);
  const games = await backendJSON<ListResponse<AdminGame>>(withQuery("/api/v1/admin/games", values));
  return <><PageHeader title="游戏管理" description="编辑游戏信息和媒体，替换游戏内容，或调整所在目录。" /><ListFilters action="/admin/games" placeholder="输入游戏名称" values={values} resultCount={games.items.length} filters={[{ name: "status", label: "游戏状态", options: [{ value: "PUBLISHED", label: "用户可见" }, { value: "DELETED", label: "已移出游戏库" }, { value: "ALL", label: "全部" }] }]} />{games.items.length === 0 ? <EmptyState title="没有可管理的游戏" description="审核通过并发布游戏后，可以在这里继续维护。" /> : <div className="admin-game-grid">{games.items.map((game) => <Link className="admin-game-card" href={`/admin/games/${game.gameId}`} key={game.gameId}><div className="admin-game-cover">{game.coverUrl ? <Image src={game.coverUrl} alt={`${game.title} 封面`} fill sizes="(min-width: 2600px) 300px, 260px" /> : <span role="img" aria-label={`${game.title} 暂无封面`}>RETROM</span>}{game.status !== "PUBLISHED" ? <span className="admin-game-hidden" title="用户当前不可见" aria-label="用户当前不可见"><AppIcon name="eye-off" /></span> : null}</div><div className="admin-game-body"><h2>{game.title}</h2><p>{game.platform.name}</p><small>{game.platformInstance.name}</small></div></Link>)}</div>}</>;
}
