"use client";

import { PhoneDisclosure } from "@/features/mobile/phone-layout";

import Image from "next/image";
import Link from "next/link";
import { useState } from "react";
import { AppIcon } from "@/components/app-icon";
import { EmptyState } from "@/components/ui";
import { LaunchButton } from "@/features/player/launch-button";
import { TagChips } from "@/components/tag-picker";
import { useCursorPage } from "@/lib/use-cursor-page";
import { useBrowserTimeZone } from "@/lib/use-browser-time-zone";
import {
  recentGameURL,
  formatRecentDuration,
  formatRecentTime,
  zonedDateKey,
  type RecentGame,
  type RecentPage,
  type RecentGameFilters,
} from "./recent-games";

const periodOptions = [
  { value: "all", label: "全部" },
  { value: "7d", label: "7 天" },
  { value: "30d", label: "30 天" },
] as const;

function RecentGameRow({ game, timeZone }: { game: RecentGame; timeZone: string }) {
  const deleted = game.status === "DELETED";
  return <article className="recent-history-row">
    {deleted ? <div className="recent-history-cover" aria-label={`${game.title} 已删除`}><span aria-hidden="true">RETROM</span></div> : <Link className="recent-history-cover" href={`/games/${game.gameId}`} aria-label={`查看 ${game.title} 详情`}>
      {game.coverUrl ? <Image src={game.coverUrl} alt="" fill sizes="180px" unoptimized /> : <span aria-hidden="true">RETROM</span>}
    </Link>}
    <div className="recent-history-content">
      <div className="recent-history-main">
        {deleted ? <h2>{game.title}</h2> : <Link href={`/games/${game.gameId}`}><h2>{game.title}</h2></Link>}
        <TagChips tags={game.tags ?? []} limit={2} label={`${game.title} 的标签`} />
        <p><AppIcon name="library" />{game.platform.name} · {game.platformInstance.name}</p>
      </div>
      <div className="recent-history-facts">
        <div className="recent-history-fact"><span><AppIcon name="clock" />最近游玩</span><strong>{formatRecentTime(game.lastPlayedAtMs, timeZone)}</strong></div>
        <div className="recent-history-fact"><span><AppIcon name="history" />累计时长</span><strong>{formatRecentDuration(game.activeDurationMs)}</strong></div>
        <div className="recent-history-fact"><span><AppIcon name="play" />游玩次数</span><strong>{game.sessionCount} 次</strong></div>
      </div>
    </div>
    <div className="recent-history-actions">
      {deleted ? <span className="recent-history-detail">已删除游戏</span> : <><div className="recent-history-launch"><LaunchButton gameId={game.gameId} returnTo="/recent" label="再玩一次" /></div><Link className="recent-history-detail" href={`/games/${game.gameId}`}>查看详情</Link></>}
    </div>
  </article>;
}

export function RecentHistory({ initialPage }: { initialPage: RecentPage }) {
  const nowMs = initialPage.generatedAtMs;
  const timeZone = useBrowserTimeZone();
  const [query, setQuery] = useState("");
  const [platformId, setPlatformId] = useState("");
  const [sort, setSort] = useState<RecentGameFilters["sort"]>("recent");
  const [period, setPeriod] = useState<RecentGameFilters["period"]>("all");
  const pagination = useCursorPage(initialPage, recentGameURL({ query, platformId, sort, period, nowMs }));
  const { page, loading } = pagination;
  const platforms = page.platforms ?? [];
  const stats = page.stats ?? { gameCount: 0, activeDurationMs: 0, sessionCount: 0 };
  const filtered = page.items;
  const todayKey = zonedDateKey(nowMs, timeZone);
  const today = filtered.filter((game) => zonedDateKey(game.lastPlayedAtMs, timeZone) === todayKey);
  const earlier = filtered.filter((game) => zonedDateKey(game.lastPlayedAtMs, timeZone) !== todayKey);

  return <>
    <section className="recent-summary-grid" aria-label="游玩统计">
      <article className="recent-summary-card"><span className="recent-summary-icon"><AppIcon name="library" /></span><span><small>最近游玩的游戏</small><strong>{stats.gameCount}</strong></span></article>
      <article className="recent-summary-card"><span className="recent-summary-icon"><AppIcon name="clock" /></span><span><small>累计游玩时长</small><strong>{formatRecentDuration(stats.activeDurationMs)}</strong></span></article>
      <article className="recent-summary-card"><span className="recent-summary-icon"><AppIcon name="play" /></span><span><small>总游玩次数</small><strong>{stats.sessionCount} 次</strong></span></article>
    </section>

    <section className="recent-filter-panel" aria-label="筛选最近游玩">
      <label className="recent-filter-search"><span>搜索游戏</span><span className="recent-input-shell"><AppIcon name="search" /><input type="search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="输入游戏名称" /></span></label>
      <PhoneDisclosure title="筛选与排序"><label className="recent-filter-select"><span>游戏平台</span><select value={platformId} onChange={(event) => setPlatformId(event.target.value)}><option value="">所有平台</option>{platforms.map((platform) => <option value={platform.id} key={platform.id}>{platform.name}</option>)}</select></label>
      <label className="recent-filter-select"><span>排列顺序</span><select value={sort} onChange={(event) => setSort(event.target.value as RecentGameFilters["sort"])}><option value="recent">按最近游玩排序</option><option value="duration">按累计时长排序</option><option value="sessions">按游玩次数排序</option><option value="title">按标题排序</option></select></label>
      <fieldset className="recent-period-filter"><legend>时间范围</legend><div>{periodOptions.map((option) => <button className={period === option.value ? "is-active" : ""} type="button" aria-pressed={period === option.value} onClick={() => setPeriod(option.value)} key={option.value}>{option.label}</button>)}</div></fieldset>
      </PhoneDisclosure><p className="recent-result-count" aria-live="polite">共 <strong>{page.filteredCount ?? 0}</strong> 款</p>
    </section>

    {pagination.error ? <p role="alert">{pagination.error} <button className="button secondary" type="button" onClick={pagination.retry}>重试</button></p> : null}
    {filtered.length === 0 ? <EmptyState title="没有符合条件的游戏" description="尝试更换平台、时间范围或搜索关键词。" /> : <div className="recent-history-groups">
      {today.length > 0 ? <section className="recent-history-group" aria-labelledby="recent-today"><h2 id="recent-today">今天</h2><div className="recent-history-list">{today.map((game) => <RecentGameRow game={game} timeZone={timeZone} key={game.gameId} />)}</div></section> : null}
      {earlier.length > 0 ? <section className="recent-history-group" aria-labelledby="recent-earlier"><h2 id="recent-earlier">更早</h2><div className="recent-history-list">{earlier.map((game) => <RecentGameRow game={game} timeZone={timeZone} key={game.gameId} />)}</div></section> : null}
    </div>}
    <footer className="list-pagination" aria-label="最近游玩分页" aria-busy={loading}>
      <span aria-live="polite">{loading ? "正在加载… " : ""}当前展示 {filtered.length} / {page.filteredCount ?? 0} 款游戏</span>
      <div><button type="button" disabled={loading || pagination.index === 0} onClick={pagination.previous}>上一页</button>
        <span>第 {pagination.index + 1} 页 · 共 {Math.max(1, Math.ceil((page.filteredCount ?? 0) / 50))} 页</span>
        <button type="button" disabled={loading || !page.nextCursor} onClick={pagination.next}>下一页</button></div>
    </footer>
  </>;
}
