import { notFound } from "next/navigation";
import { ButtonLink, PageHeader, StatusBadge } from "@/components/ui";
import { LaunchControls, type CoreOption, type DOSEntry } from "@/features/player/launch-controls";
import { LaunchButton } from "@/features/player/launch-button";
import { backendJSON } from "@/lib/backend";

type GameDetail = {
  gameId: string; title: string; description: string; developer: string; publisher: string; genre: string;
  players: number | null; releaseYear: number | null; activeDurationMs: number;
  platform: { id: string; name: string }; platformInstance: { id: string; name: string };
  coreOptions: CoreOption[];
  dosEntries: DOSEntry[];
  defaultDosEntry: string | null;
  saveStates: Array<{ saveStateId: string; name: string; createdAtMs: number; core: { id: string; name: string } }>;
};

export default async function GamePage({ params }: { params: Promise<{ gameId: string }> }) {
  const { gameId } = await params;
  let game: GameDetail;
  try { game = await backendJSON<GameDetail>(`/api/v1/games/${gameId}`); } catch { notFound(); }
  return (
    <>
      <PageHeader title="游戏详情" description="本次启动不会修改目录默认核心，存档会锁定实际运行版本。" actions={<ButtonLink href="/library" secondary>返回游戏库</ButtonLink>} />
      <section className="panel hero">
        <div className="hero-cover" role="img" aria-label={`${game.title} 封面占位`} />
        <div className="hero-info"><StatusBadge tone="good">已发布</StatusBadge><h1>{game.title}</h1><div className="game-meta"><span>{game.platform.name}</span><span>{game.platformInstance.name}</span>{game.releaseYear ? <span>{game.releaseYear}</span> : null}</div><p>{game.description || "尚未填写游戏简介。"}</p><dl className="detail-list"><div><dt>开发 / 发行</dt><dd>{game.developer || "—"} / {game.publisher || "—"}</dd></div><div><dt>类型 / 玩家</dt><dd>{game.genre || "—"} / {game.players ?? "—"}</dd></div><div><dt>有效游玩</dt><dd>{Math.round(game.activeDurationMs / 60000)} 分钟</dd></div></dl></div>
        <LaunchControls gameId={game.gameId} coreOptions={game.coreOptions} dosEntries={game.dosEntries} defaultDosEntry={game.defaultDosEntry} />
      </section>
      <section className="panel save-strip"><div className="panel-head"><div><h2>手动存档</h2><p>继续时始终使用存档锁定的 CoreArtifact 与 VariantRevision</p></div><StatusBadge tone={game.saveStates.length ? "good" : "neutral"}>{game.saveStates.length} 份</StatusBadge></div><div className="panel-body">{game.saveStates.length ? <div className="admin-grid">{game.saveStates.map((save) => <article className="admin-card" key={save.saveStateId}><h2>{save.name}</h2><p>{save.core.name} · {new Date(save.createdAtMs).toLocaleString("zh-CN")}</p><LaunchButton gameId={game.gameId} saveStateId={save.saveStateId} returnTo={`/games/${game.gameId}`} label="从此存档继续" /></article>)}</div> : <p className="game-meta">游玩时使用 Player 工具栏保存状态后，可从这里一键恢复。</p>}</div></section>
    </>
  );
}
