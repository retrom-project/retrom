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
  return <><PageHeader title="游戏管理" description="元信息、媒体、内容与运行版本分别以不可变 revision 演进。" /><ListFilters action="/admin/games" placeholder="搜索已发布或已删除游戏…" values={values} filters={[{ name: "status", label: "游戏状态", options: [{ value: "PUBLISHED", label: "已发布" }, { value: "DELETED", label: "已删除" }, { value: "ALL", label: "全部" }] }]} />{games.items.length === 0 ? <EmptyState title="没有可管理的游戏" description="审核通过并发布游戏后，可以在这里编辑 revision、替换内容或移动目录。" /> : <div className="admin-grid">{games.items.map((game) => <Link className="admin-card" href={`/admin/games/${game.gameId}`} key={game.gameId}><StatusBadge tone={game.status === "PUBLISHED" ? "good" : "bad"}>{game.status}</StatusBadge><h2>{game.title}</h2><p>{game.platform.name} · {game.platformInstance.name}</p><div className="metric-line"><span>资源版本</span><strong>v{game.version}</strong></div></Link>)}</div>}</>;
}
