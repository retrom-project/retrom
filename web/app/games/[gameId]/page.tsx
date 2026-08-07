import Image from "next/image";
import { notFound } from "next/navigation";
import Link from "next/link";
import { StatusBadge } from "@/components/ui";
import { LaunchControls, type CoreOption, type DOSEntry } from "@/features/player/launch-controls";
import { LaunchButton } from "@/features/player/launch-button";
import { backendJSON, formatTime } from "@/lib/backend";

type GameDetail = {
  gameId: string; title: string; description: string; developer: string; publisher: string; genre: string;
  players: number | null; releaseYear: number | null; activeDurationMs: number;
  coverUrl: string | null;
  platform: { id: string; name: string }; platformInstance: { id: string; name: string };
  coreOptions: CoreOption[];
  dosEntries: DOSEntry[];
  defaultDosEntry: string | null;
  saveStateCount: number;
  saveStates: Array<{ saveStateId: string; name: string; createdAtMs: number; screenshotUrl: string; core: { id: string; name: string } }>;
};

export default async function GamePage({ params }: { params: Promise<{ gameId: string }> }) {
  const { gameId } = await params;
  let game: GameDetail;
  try { game = await backendJSON<GameDetail>(`/api/v1/games/${gameId}`); } catch { notFound(); }
  return (
    <div className="page-layout page-layout-detail">
      <nav className="detail-toolbar" aria-label="返回导航"><Link className="row-action" href="/library">← 返回游戏库</Link></nav>
      <section className="panel hero">
        {game.coverUrl ? <div className="hero-cover has-image"><Image className="hero-cover-image" src={game.coverUrl} alt={`${game.title} 封面`} fill sizes="280px" priority /></div> : <div className="hero-cover" role="img" aria-label={`${game.title} 暂无封面`} />}
        <div className="hero-info"><p className="eyebrow">{game.platform.name}</p><h1>{game.title}</h1><div className="game-meta"><span>{game.platformInstance.name}</span>{game.releaseYear ? <span>{game.releaseYear}</span> : null}</div><p>{game.description || "尚未填写游戏简介。"}</p><dl className="detail-list"><div><dt>开发 / 发行</dt><dd>{game.developer || "—"} / {game.publisher || "—"}</dd></div><div><dt>类型 / 玩家</dt><dd>{game.genre || "—"} / {game.players ?? "—"}</dd></div><div><dt>有效游玩</dt><dd>{Math.round(game.activeDurationMs / 60000)} 分钟</dd></div></dl></div>
        <LaunchControls gameId={game.gameId} coreOptions={game.coreOptions} dosEntries={game.dosEntries} defaultDosEntry={game.defaultDosEntry} />
      </section>
      <section className="panel save-strip"><div className="panel-head"><div><h2>手动存档</h2><p>从保存时的画面和运行环境继续</p></div>{game.saveStateCount > 8 ? <Link className="row-action" href={`/saves?gameId=${game.gameId}`}>查看更多</Link> : <StatusBadge tone={game.saveStateCount ? "good" : "neutral"}>{game.saveStateCount} 份</StatusBadge>}</div><div className="panel-body">{game.saveStates.length ? <div className="save-card-grid">{game.saveStates.map((save) => <article className="save-card compact" key={save.saveStateId}><div className="save-shot"><Image src={save.screenshotUrl} alt={`${game.title} 存档画面`} width={640} height={360} unoptimized /><div className="save-shot-action"><LaunchButton gameId={game.gameId} saveStateId={save.saveStateId} returnTo={`/games/${game.gameId}`} label="从此存档继续" /></div></div><div className="save-card-body"><strong className="save-core-name">{save.core.name}</strong><time dateTime={new Date(save.createdAtMs).toISOString()}>{formatTime(save.createdAtMs)}</time></div></article>)}</div> : <p className="game-meta">游玩时使用工具栏保存进度后，可从这里一键恢复。</p>}</div></section>
    </div>
  );
}
