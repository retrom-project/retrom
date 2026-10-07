"use client";
import { useCallback, useDeferredValue, useState } from "react";
import { loadDetail, loadGames } from "@/features/library/api";
import { useResource } from "@/lib/use-resource";
import { ResponsiveSheet } from "@/components/responsive-sheet";

export function SaveGameFilter({
  gameId,
  onChange,
}: {
  gameId: string;
  onChange: (gameId: string) => void;
}) {
  const [search, setSearch] = useState("");
  const [open, setOpen] = useState(false);
  const q = useDeferredValue(search);
  const [offset, setOffset] = useState(0);
  const loader = useCallback(
    () => loadGames("library", { q, offset, limit: 20, sort: "title" }),
    [q, offset],
  );
  const selectedLoader = useCallback(
    async () => (gameId ? (await loadDetail(gameId, "user")).game : null),
    [gameId],
  );
  const games = useResource(loader);
  const selected = useResource(selectedLoader);
  const options = new Map(games.data?.items.map((game) => [game.id, game]));
  if (selected.data) {
    options.set(selected.data.id, selected.data);
  }
  return (
    <div className="save-game-filter">
      <label>
        <span>游戏</span>
        <button className="save-game-choice" onClick={() => setOpen(true)}>
          {gameId ? (selected.data?.title ?? "当前游戏") : "所有游戏"}
        </button>
      </label>
      <ResponsiveSheet
        open={open}
        title="选择游戏"
        onClose={() => setOpen(false)}
      >
        <div className="stack">
          <label className="field">
            查找游戏
            <input
              type="search"
              aria-label="查找游戏"
              placeholder="查找游戏名称"
              value={search}
              onChange={(event) => {
                setSearch(event.target.value);
                setOffset(0);
              }}
            />
          </label>
          <label className="field">
            游戏
            <select
              aria-label="游戏筛选"
              value={gameId}
              onChange={(event) => {
                onChange(event.target.value);
                setOpen(false);
              }}
            >
              <option value="">所有游戏</option>
              {gameId && !options.has(gameId) ? (
                <option value={gameId}>当前游戏</option>
              ) : null}
              {[...options.values()].map((game) => (
                <option key={game.id} value={game.id}>
                  {game.title} · {game.directoryName}
                </option>
              ))}
            </select>
          </label>
          {games.error || selected.error ? (
            <span role="alert">{games.error || selected.error}</span>
          ) : null}
          <div className="save-game-paging">
            <button
              aria-label="上一组游戏"
              disabled={offset === 0}
              onClick={() => setOffset(Math.max(0, offset - 20))}
            >
              ‹
            </button>
            <span>{games.data?.total ?? "—"} 款可选</span>
            <button
              aria-label="下一组游戏"
              disabled={!games.data || offset + 20 >= games.data.total}
              onClick={() => setOffset(offset + 20)}
            >
              ›
            </button>
          </div>
        </div>
      </ResponsiveSheet>
    </div>
  );
}
