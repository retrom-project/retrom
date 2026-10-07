"use client";
import Image from "next/image";
import Link from "next/link";
import { useCallback, useState } from "react";
import { AppIcon } from "@/components/app-icon";
import { BrowserTime } from "@/components/browser-time";
import { PageHeader, EmptyState } from "@/components/ui";
import { ResourceState } from "@/components/resource-state";
import { useResource } from "@/lib/use-resource";
import type { Game, Directory, Tag, Schema } from "@/lib/api/types";
import { loadDirectories, loadGames, loadTags } from "@/features/library/api";
import { useLibraryQuery } from "@/features/library/use-library-query";
import type { ListFilters } from "@/features/library/use-library-query";

type AdminKind = "admin" | "review";
type QueryValues = ReturnType<typeof useLibraryQuery>["values"];
export function AdminGameBrowser({ kind, initial }: { kind: AdminKind; initial: ListFilters }) {
  const { values, query, update } = useLibraryQuery(initial);
  const loader = useCallback(() => loadGames(kind, query), [kind, query]);
  const games = useResource(loader);
  const directories = useResource(loadDirectories);
  const tags = useResource(loadTags);
  const review = kind === "review";
  return (
    <div className={review ? "review-library" : "admin-game-library"}>
      <PageHeader
        title={review ? "待审核" : "游戏管理"}
        description={review
          ? "核对游戏资料与运行配置，试玩后批准入库或丢弃。"
          : "维护已发布游戏的信息、媒体和运行配置，快速定位需要处理的内容。"}
      />
      {!review ? <AdminGameSummary data={games.data} directoryCount={directories.data?.items.length} /> : null}
      <AdminGameFilters
        values={values}
        directories={directories.data?.items ?? []}
        tags={tags.data?.items ?? []}
        onApply={update}
      />
      {directories.error || tags.error ? <p role="alert">{directories.error || tags.error}</p> : null}
      <ResourceState resource={games}>
        {(data) => <>
          {data.items.length ? review ? (
            <div className="review-queue-list">
              {data.items.map((game) => <ReviewRow key={game.id} game={game} />)}
            </div>
          ) : <AdminGameTable games={data.items} directories={directories.data?.items ?? []} /> : (
            <EmptyState
              title={review ? "没有待审核的游戏" : "没有可管理的游戏"}
              description="当前搜索和筛选条件没有匹配项，请调整后重试。"
            />
          )}
          <footer className="list-pagination">
            <span>当前展示 {data.items.length} / {data.total} 款游戏</span>
            <div>
              <button className="button secondary" disabled={!values.offset} onClick={() => update({ offset: Math.max(0, values.offset - 24) }, false)}>上一页</button>
              <span>第 {Math.floor(values.offset / 24) + 1} 页</span>
              <button className="button secondary" disabled={values.offset + 24 >= data.total} onClick={() => update({ offset: values.offset + 24 }, false)}>下一页</button>
            </div>
          </footer>
        </>}
      </ResourceState>
    </div>
  );
}

function AdminGameFilters({ values, directories, tags, onApply }: {
  values: QueryValues;
  directories: Directory[];
  tags: Tag[];
  onApply: (values: Partial<QueryValues>) => void;
}) {
  const [draft, setDraft] = useState(values);
  return (
    <form className="admin-game-toolbar" onSubmit={(event) => { event.preventDefault(); onApply(draft); }}>
      <label className="admin-game-search">
        <span>搜索游戏</span>
        <span><AppIcon name="search" /><input aria-label="搜索游戏" placeholder="输入游戏名称或标签" value={draft.q} onChange={(event) => setDraft({ ...draft, q: event.target.value })} /></span>
      </label>
      <label><span>游戏目录</span><select value={draft.directory} onChange={(event) => setDraft({ ...draft, directory: event.target.value })}>
        <option value="">所有目录</option>
        {directories.map((directory) => <option key={directory.id} value={directory.id}>{directory.name}</option>)}
      </select></label>
      <label><span>标签</span><select value={draft.tag} onChange={(event) => setDraft({ ...draft, tag: event.target.value })}>
        <option value="">所有标签</option>
        {tags.map((tag) => <option key={tag.id} value={tag.id}>{tag.name}</option>)}
      </select></label>
      <label><span>排列顺序</span><select value={draft.sort} onChange={(event) => setDraft({ ...draft, sort: event.target.value === "recent" ? "recent" : "title" })}>
        <option value="title">名称排序</option><option value="recent">最近游玩</option>
      </select></label>
      <button className="button" type="submit">应用筛选</button>
      <button className="button secondary" type="button" onClick={() => {
        const reset = { ...values, q: "", directory: "", tag: "", platform: "", sort: "title" as const, offset: 0 };
        setDraft(reset); onApply(reset);
      }}>重置</button>
    </form>
  );
}
function AdminGameTable({ games, directories }: { games: Game[]; directories: Directory[] }) {
  return <div className="admin-game-table-scroll" tabIndex={0} aria-label="游戏管理表格，可横向滚动">
    <table className="admin-game-table">
      <thead><tr><th>封面</th><th>游戏</th><th>用户状态</th><th>运行环境 / 目录</th><th>最近更新</th><th>操作</th></tr></thead>
      <tbody>{games.map((game) => <tr key={game.id}>
        <td><AdminGameCover game={game} /></td>
        <td><div className="admin-game-identity"><Link href={`/admin/games/${game.id}`}>{game.title}</Link><p>{game.platformId}{game.releaseYear ? ` · ${game.releaseYear}` : ""}</p><span>{game.directoryName}</span></div></td>
        <td><span className="status good">用户可见</span></td>
        <td><strong>{game.directoryName}</strong><small>推荐 {directories.find((directory) => directory.id === game.platformInstanceId)?.defaultCoreId ?? "—"}</small></td>
        <td><strong><BrowserTime value={game.updatedAtMs} /></strong><small>{game.tags.length ? game.tags.map((tag) => tag.name).join(" · ") : "未设置标签"}</small></td>
        <td><Link className="button secondary" href={`/admin/games/${game.id}`}>管理</Link></td>
      </tr>)}</tbody>
    </table>
  </div>;
}
function ReviewRow({ game }: { game: Game }) {
  return <article className="review-queue-row">
    <div className="review-source"><AdminGameCover game={game} /><div><Link href={`/admin/reviews/${game.id}`}><strong>{game.title}</strong></Link><small>{game.tags.map((tag) => tag.name).join(" · ") || "未设置标签"}</small></div></div>
    <span>{game.directoryName}</span>
    <span className="status warn">待审核</span>
    <div className="review-queue-note"><strong>待核对资料</strong><small>确认后发布或丢弃</small></div>
    <div className="review-queue-note"><strong><BrowserTime value={game.updatedAtMs} /></strong><small>更新时间</small></div>
    <Link className="button" href={`/admin/reviews/${game.id}`}>审核</Link>
  </article>;
}
export function AdminGameCover({ game }: { game: Game }) {
  const cover = game.media.find((media) => media.kind === "cover");
  return <div className="admin-game-thumb">
    {cover ? <Image src={cover.url} alt={`${game.title} 封面`} fill sizes="102px" unoptimized /> : <span><strong>{game.title}</strong><small>{game.platformId}</small></span>}
  </div>;
}

function AdminGameSummary({ data, directoryCount }: { data: Schema<"GamePage"> | null; directoryCount: number | undefined }) {
  return <div className="admin-game-kpis">
    <article><span>匹配游戏</span><strong>{data?.total ?? "—"}</strong></article>
    <article><span>当前展示</span><strong>{data?.items.length ?? "—"}</strong></article>
    <article><span>游戏目录</span><strong>{directoryCount ?? "—"}</strong></article>
  </div>;
}
