import { ListFilters } from "@/components/list-filters";
import { EmptyState, PageHeader, StatusBadge } from "@/components/ui";
import type { GameSummary } from "@/features/library/game-grid";
import { backendJSON, scalarSearchParams, withQuery, type ListResponse } from "@/lib/backend";
import Link from "next/link";

type AdminGame = GameSummary & { version: number; updatedAtMs: number };
export const metadata = { title: "游戏管理" };

export default async function AdminGamesPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const values = scalarSearchParams(await searchParams, ["q", "status"]);
  const games = await backendJSON<ListResponse<AdminGame>>(withQuery("/api/v1/admin/games", values));
  return <><PageHeader title="游戏管理" description="编辑游戏信息和媒体，替换游戏内容，或调整所在目录。" /><ListFilters action="/admin/games" placeholder="搜索已发布或已删除游戏…" values={values} filters={[{ name: "status", label: "游戏状态", options: [{ value: "PUBLISHED", label: "已发布" }, { value: "DELETED", label: "已删除" }, { value: "ALL", label: "全部" }] }]} />{games.items.length === 0 ? <EmptyState title="没有可管理的游戏" description="审核通过并发布游戏后，可以在这里继续维护。" /> : <div className="admin-grid">{games.items.map((game) => <Link className="admin-card" href={`/admin/games/${game.gameId}`} key={game.gameId}><StatusBadge tone={game.status === "PUBLISHED" ? "good" : "bad"}>{game.status === "PUBLISHED" ? "已发布" : "已删除"}</StatusBadge><h2>{game.title}</h2><p>{game.platform.name} · {game.platformInstance.name}</p><div className="metric-line"><span>信息版本</span><strong>{game.version}</strong></div></Link>)}</div>}</>;
}
