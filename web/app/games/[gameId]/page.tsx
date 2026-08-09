import Image from "next/image";
import { notFound } from "next/navigation";
import Link from "next/link";
import { GameDetailSaves } from "@/features/games/game-detail-saves";
import { LaunchControls, type CoreOption, type DOSEntry } from "@/features/player/launch-controls";
import { collectSavePages, latestAvailableSave, type SavePage } from "@/features/saves/save-library";
import { backendJSON, withQuery } from "@/lib/backend";

type GameDetail = {
  gameId: string; title: string; description: string; developer: string; publisher: string; genre: string;
  players: number | null; releaseYear: number | null; activeDurationMs: number;
  coverUrl: string | null;
  platform: { id: string; name: string }; platformInstance: { id: string; name: string };
  coreOptions: CoreOption[];
  dosEntries: DOSEntry[];
  defaultDosEntry: string | null;
};

function formatPlayTime(value: number) {
  if (value < 60_000) return "少于 1 分钟";
  const minutes = Math.floor(value / 60_000);
  if (minutes < 60) return `${minutes} 分钟`;
  const hours = Math.floor(minutes / 60);
  const remainder = minutes % 60;
  return remainder ? `${hours} 小时 ${remainder} 分钟` : `${hours} 小时`;
}

async function loadGameSaves(gameId: string) {
  return collectSavePages((cursor) => backendJSON<SavePage>(withQuery("/api/v1/saves", {
    gameId,
    availability: "ALL",
    limit: "100",
    ...(cursor ? { cursor } : {}),
  })));
}

function fact(label: string, value: string | number | null) {
  const visible = value === null || value === "" ? "—" : value;
  return <div className="game-detail-fact"><span>{label}</span><strong className={visible === "—" ? "is-missing" : undefined} title={String(visible)}>{visible}</strong></div>;
}

export default async function GamePage({ params }: { params: Promise<{ gameId: string }> }) {
  const { gameId } = await params;
  let game: GameDetail;
  try { game = await backendJSON<GameDetail>(`/api/v1/games/${gameId}`); } catch { notFound(); }
  const saves = await loadGameSaves(gameId);
  const latestSave = latestAvailableSave(saves.items);
  return (
    <div className="page-layout page-layout-detail game-detail-page">
      <nav className="game-detail-breadcrumb" aria-label="返回导航"><Link href="/library">← 游戏库</Link></nav>
      <section className="game-detail-hero">
        <div className="game-detail-poster-shell">
          {game.coverUrl ? <div className="game-detail-poster"><Image src={game.coverUrl} alt={`${game.title} 封面`} fill sizes="240px" priority /></div> : <div className="game-detail-poster is-placeholder" role="img" aria-label={`${game.title} 暂无封面`}><span>{game.title}</span></div>}
          <div className="game-detail-poster-caption"><span>{game.platform.name}</span><span>{game.releaseYear ?? "年份未知"}</span></div>
        </div>
        <div className="game-detail-main">
          <p className="game-detail-eyebrow">{game.platform.name} · {game.platformInstance.name}</p>
          <h1>{game.title}</h1>
          <div className="game-detail-meta">{game.releaseYear ? <span>{game.releaseYear}</span> : null}{game.publisher ? <span>{game.publisher}</span> : null}{game.genre ? <span>{game.genre}</span> : null}</div>
          <p className="game-detail-description">{game.description || "尚未填写游戏简介。"}</p>
          <div className="game-detail-playtime"><strong>累计游玩</strong><span>{formatPlayTime(game.activeDurationMs)}</span></div>
        </div>
        <LaunchControls
          gameId={game.gameId}
          coreOptions={game.coreOptions}
          dosEntries={game.dosEntries}
          defaultDosEntry={game.defaultDosEntry}
          latestSave={latestSave ? { saveStateId: latestSave.saveStateId, screenshotUrl: latestSave.screenshotUrl, createdAtMs: latestSave.createdAtMs, coreId: latestSave.core.id, coreName: latestSave.core.name } : null}
          nowMs={saves.generatedAtMs}
        />
      </section>
      <section className="game-detail-info-strip" aria-label="游戏信息">
        {fact("游戏平台", game.platform.name)}
        {fact("游戏目录", game.platformInstance.name)}
        {fact("发行年份", game.releaseYear)}
        {fact("开发商", game.developer)}
        {fact("发行商", game.publisher)}
        {fact("类型", game.genre)}
        {fact("玩家数", game.players)}
      </section>
      <GameDetailSaves gameId={game.gameId} gameTitle={game.title} saves={saves.items} nowMs={saves.generatedAtMs} threadCoreIds={game.coreOptions.filter((core) => core.requiresThreads).map((core) => core.coreId)} />
    </div>
  );
}
