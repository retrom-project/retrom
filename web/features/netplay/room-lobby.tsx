"use client";

import Image from "next/image";
import Link from "next/link";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { AppIcon } from "@/components/app-icon";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { EmptyState, PageHeader, StatusBadge } from "@/components/ui";
import { useAuth } from "@/features/auth/auth-provider";
import { applyRoomSnapshot, NetplayAPIError, netplayBlocker, roomMutation, type AuthenticatedFetch, type NetplayGame, type NetplayGameList, type NetplayLaunch, type NetplayRoom } from "./client";
import { TagChips } from "@/components/tag-picker";

type Filters = { query: string; platformId: string; platformInstanceId: string; availability: "SUPPORTED" | "ALL"; sort: "RECENT_DESC" | "ADDED_DESC" | "TITLE_ASC" };
export type NetplayFilterParams = Partial<Record<"q" | "platformId" | "platformInstanceId" | "availability" | "sort", string>>;

function filtersFromParams(values: NetplayFilterParams): Filters {
  return {
    query: values.q?.slice(0, 100) ?? "", platformId: values.platformId ?? "",
    platformInstanceId: values.platformInstanceId ?? "",
    availability: values.availability === "ALL" ? "ALL" : "SUPPORTED",
    sort: values.sort === "ADDED_DESC" || values.sort === "TITLE_ASC" ? values.sort : "RECENT_DESC",
  };
}

function availableSeats(room: NetplayRoom) {
  return Array.from({ length: 4 }, (_, index) => index + 1).filter((playerNo) => playerNo <= (room.game?.maxPlayers ?? 1));
}

function GamePicker({ initialGames, initialNextCursor, authenticatedFetch, busy, initialFilterParams, onSelect }: {
  initialGames: NetplayGame[]; initialNextCursor: string | null; authenticatedFetch: AuthenticatedFetch;
  busy: boolean; initialFilterParams: NetplayFilterParams;
  onSelect: (game: NetplayGame) => void;
}) {
  const searchInput = useRef<HTMLInputElement>(null);
  const loadMoreTarget = useRef<HTMLDivElement>(null);
  const userScrolled = useRef(false);
  const [games, setGames] = useState(initialGames);
  const [nextCursor, setNextCursor] = useState(initialNextCursor);
  const [loadingMore, setLoadingMore] = useState(false);
  const [loadMoreError, setLoadMoreError] = useState("");
  const [filters, setFilters] = useState<Filters>(() => filtersFromParams(initialFilterParams));
  const platforms = useMemo(() => [...new Map(games.filter((game) => filters.availability === "ALL" || game.availability === "SUPPORTED").map((game) => [game.platformId, game.platformName])).entries()], [filters.availability, games]);
  const collections = useMemo(() => [...new Map(games.filter((game) => !filters.platformId || game.platformId === filters.platformId).map((game) => [game.platformInstanceId, game.platformInstanceName])).entries()], [filters.platformId, games]);
  const filtered = useMemo(() => {
    const query = filters.query.trim().slice(0, 100).toLocaleLowerCase("zh-CN");
    return games.filter((game) => {
      if (filters.availability === "SUPPORTED" && game.availability !== "SUPPORTED") {return false;}
      if (filters.platformId && game.platformId !== filters.platformId) {return false;}
      if (filters.platformInstanceId && game.platformInstanceId !== filters.platformInstanceId) {return false;}
      return !query || [game.title, game.platformName, game.platformInstanceName, ...game.netplayProfiles.map((profile) => profile.coreName), ...game.tags.map((tag) => tag.name)].some((value) => value.toLocaleLowerCase("zh-CN").includes(query));
    }).sort((left, right) => {
      const title = left.title.localeCompare(right.title, "zh-CN") || left.gameId.localeCompare(right.gameId);
      if (filters.sort === "TITLE_ASC") {return title;}
      if (filters.sort === "ADDED_DESC") {return right.addedAtMs - left.addedAtMs || title;}
      return (right.lastPlayedAtMs ?? -1) - (left.lastPlayedAtMs ?? -1) || title;
    });
  }, [filters, games]);
  useEffect(() => {
    const values = new URLSearchParams();
    if (filters.query) {values.set("q", filters.query);}
    if (filters.platformId) {values.set("platformId", filters.platformId);}
    if (filters.platformInstanceId) {values.set("platformInstanceId", filters.platformInstanceId);}
    values.set("availability", filters.availability); values.set("sort", filters.sort);
    window.history.replaceState(null, "", `${window.location.pathname}?${values}`);
  }, [filters]);
  useEffect(() => {
    const search = searchInput.current;
    const focusSearch = (event: KeyboardEvent) => {
      if (event.key === "/" && !(event.target instanceof HTMLInputElement || event.target instanceof HTMLSelectElement || event.target instanceof HTMLTextAreaElement)) {
        event.preventDefault(); search?.focus();
      }
    };
    search?.setAttribute("data-shortcut-ready", "true");
    window.addEventListener("keydown", focusSearch); return () => {
      window.removeEventListener("keydown", focusSearch);
      search?.removeAttribute("data-shortcut-ready");
    };
  }, []);
  const loadMore = useCallback(async () => {
    if (!nextCursor || loadingMore) {return;}
    setLoadingMore(true); setLoadMoreError("");
    try {
      const response = await authenticatedFetch(`/api/v1/netplay/games?availability=ALL&limit=100&cursor=${encodeURIComponent(nextCursor)}`, { cache: "no-store" });
      if (!response.ok) {throw new Error("无法加载更多联机游戏");}
      const page = await response.json() as NetplayGameList;
      setGames((current) => {
        const seen = new Set(current.map((game) => game.gameId));
        return [...current, ...page.items.filter((game) => !seen.has(game.gameId))];
      });
      setNextCursor(page.nextCursor);
    } catch (caught) {
      setLoadMoreError(caught instanceof Error ? caught.message : "无法加载更多联机游戏");
    } finally {
      setLoadingMore(false);
    }
  }, [authenticatedFetch, loadingMore, nextCursor]);
  useEffect(() => {
    const markScrolled = () => { userScrolled.current = true; };
    window.addEventListener("scroll", markScrolled, { passive: true });
    return () => window.removeEventListener("scroll", markScrolled);
  }, []);
  useEffect(() => {
    const target = loadMoreTarget.current;
    if (!target || !nextCursor || typeof IntersectionObserver === "undefined") {return;}
    const observer = new IntersectionObserver((entries) => {
      if (entries.some((entry) => entry.isIntersecting) && userScrolled.current) {void loadMore();}
    }, { rootMargin: "320px 0px" });
    observer.observe(target);
    return () => observer.disconnect();
  }, [loadMore, nextCursor]);
  return <section className="netplay-picker" aria-labelledby="netplay-picker-title">
    <div className="netplay-picker-title"><div><p className="eyebrow">选择游戏</p><h2 id="netplay-picker-title">选择经过验证的联机组合</h2></div><strong>当前显示 {filtered.length} 款 · 已加载 {games.length} 款</strong></div>
    <div className="library-toolbar"><div className="library-tool-row">
      <label className="library-search"><AppIcon name="search" /><input ref={searchInput} id="netplay-game-search" type="search" maxLength={100} aria-label="搜索联机游戏" placeholder="搜索游戏、平台或核心…" value={filters.query} onChange={(event) => setFilters((current) => ({ ...current, query: event.target.value }))} /></label>
      <label><span className="sr-only">游戏集合</span><select value={filters.platformInstanceId} onChange={(event) => setFilters((current) => ({ ...current, platformInstanceId: event.target.value }))}><option value="">全部游戏集合</option>{collections.map(([id, name]) => <option key={id} value={id}>{name}</option>)}</select></label>
      <label><span className="sr-only">联机支持</span><select value={filters.availability} onChange={(event) => setFilters((current) => ({ ...current, availability: event.target.value as Filters["availability"] }))}><option value="SUPPORTED">支持联机</option><option value="ALL">全部游戏</option></select></label>
    </div><div className="library-platform-row"><span className="library-platform-label">平台</span><button className={!filters.platformId ? "is-active" : ""} type="button" onClick={() => setFilters((current) => ({ ...current, platformId: "", platformInstanceId: "" }))}>全部</button>{platforms.map(([id, name]) => <button className={filters.platformId === id ? "is-active" : ""} key={id} type="button" onClick={() => setFilters((current) => ({ ...current, platformId: id, platformInstanceId: current.platformId === id ? current.platformInstanceId : "" }))}>{name}</button>)}<label className="netplay-sort"><span className="sr-only">排序</span><select value={filters.sort} onChange={(event) => setFilters((current) => ({ ...current, sort: event.target.value as Filters["sort"] }))}><option value="RECENT_DESC">最近游玩</option><option value="ADDED_DESC">最近添加</option><option value="TITLE_ASC">标题排序</option></select></label></div></div>
    {filtered.length ? <div className="library-game-grid">{filtered.map((game) => <article className={`library-game-card netplay-game-card${game.availability === "UNSUPPORTED" ? " is-disabled" : ""}`} key={game.gameId}>
      <div className="library-game-cover">{game.coverUrl ? <Image src={game.coverUrl} alt={`${game.title} 封面`} fill sizes="280px" unoptimized /> : <span className="library-poster"><small>RETROM NETPLAY</small><strong>{game.title}</strong><span>{game.platformName}</span></span>}<span className="library-platform-tag">{game.platformName}</span></div>
      <div className="library-game-body"><div className="library-game-title-row"><h2>{game.title}</h2></div><p><span>{game.platformInstanceName}</span><span>{game.netplayProfiles[0]?.coreName ?? "未验证核心"}</span></p><TagChips tags={game.tags} limit={2} label={`${game.title} 的标签`} />{game.availability === "SUPPORTED" ? <button className="button netplay-select-game" type="button" disabled={busy} onClick={() => onSelect(game)}>选择</button> : <p className="netplay-blocker">{netplayBlocker(game.blockerCode)}</p>}</div>
    </article>)}</div> : <EmptyState title={filters.availability === "SUPPORTED" ? "当前已加载范围没有支持联机的游戏" : "没有符合条件的游戏"} description={nextCursor ? "继续向下加载，或调整搜索与平台范围。" : "调整搜索或平台范围后重试。"} action={<button className="button secondary" type="button" onClick={() => setFilters((current) => ({ ...current, query: "", availability: "ALL" }))}>查看全部游戏</button>} />}
    <div className="netplay-game-pagination" ref={loadMoreTarget} aria-live="polite">
      {loadMoreError ? <span role="alert">{loadMoreError}</span> : null}
      {nextCursor ? <button className="button secondary" type="button" disabled={loadingMore} onClick={() => void loadMore()}>{loadingMore ? "正在加载…" : loadMoreError ? "重试加载" : "加载更多"}</button> : <span>已加载全部联机游戏</span>}
    </div>
  </section>;
}

type RoomMutation = (
  path: string,
  method: "POST" | "PUT" | "DELETE",
  body?: unknown,
  retryReadyPrecondition?: boolean,
) => Promise<void>;

function DraftRoomView({ authenticatedFetch, busy, error, games, initialFilterParams, initialNextCursor, onProfileClose, onSelect, profileGame, room }: {
  authenticatedFetch: AuthenticatedFetch;
  busy: boolean;
  error: string;
  games: NetplayGame[];
  initialFilterParams: NetplayFilterParams;
  initialNextCursor: string | null;
  onProfileClose: () => void;
  onSelect: (game: NetplayGame, profileId?: string) => void;
  profileGame: NetplayGame | null;
  room: NetplayRoom;
}) {
  return <div className="page-layout netplay-room-page">
    <PageHeader eyebrow={`房间 #${room.roomId.slice(0, 8)}`} title="选择联机游戏" description="可选择使用已验证 EmulatorJS 与核心组合的 READY 游戏。" actions={<Link className="button secondary" href="/netplay">返回联机首页</Link>} />
    {error ? <div className="feedback-banner bad" role="alert"><div>{error}</div></div> : null}
    <GamePicker initialGames={games} initialNextCursor={initialNextCursor} authenticatedFetch={authenticatedFetch} busy={busy} initialFilterParams={initialFilterParams} onSelect={onSelect} />
    {profileGame ? <ProfileDialog game={profileGame} onClose={onProfileClose} onSelect={(id) => onSelect(profileGame, id)} /> : null}
  </div>;
}

function RoomSeat({ busy, mutate, playerNo, room }: { busy: boolean; mutate: RoomMutation; playerNo: number; room: NetplayRoom }) {
  const member = room.members.find((candidate) => candidate.playerNo === playerNo);
  const supported = availableSeats(room).includes(playerNo);
  if (!member) {
    return <article className={`netplay-seat${!supported ? " is-disabled" : ""}`}><strong>P{playerNo}</strong><span className="netplay-empty-seat" aria-hidden="true">+</span><h3>{supported ? "空座位" : "当前游戏不支持"}</h3>{supported && room.permissions.canJoin ? <button type="button" disabled={busy} onClick={() => void mutate(`/api/v1/netplay/rooms/${room.roomId}/members/me/seat`, "PUT", { playerNo })}>选择 P{playerNo}</button> : null}</article>;
  }
  const role = member.role === "HOST" ? "房主" : member.ready ? "已准备" : "未准备";
  const canRemove = room.permissions.host && member.role === "GUEST" && room.state === "WAITING";
  return <article className="netplay-seat is-occupied"><strong>P{playerNo}</strong><span className="netplay-avatar" aria-hidden="true">{member.displayName.slice(0, 1)}</span><h3>{member.displayName}</h3><p>{role}</p>{canRemove ? <button type="button" disabled={busy} onClick={() => void mutate(`/api/v1/netplay/rooms/${room.roomId}/members/${member.memberId}`, "DELETE")}>移出</button> : null}</article>;
}

function RoomSeats({ busy, mutate, room }: { busy: boolean; mutate: RoomMutation; room: NetplayRoom }) {
  return <section className="netplay-seats" aria-labelledby="seat-title">
    <div className="netplay-section-head"><div><h2 id="seat-title">玩家座位</h2><p>准备后需先取消准备才能换座</p></div></div>
    <div className="netplay-seat-grid">{[1, 2, 3, 4].map((playerNo) => <RoomSeat busy={busy} mutate={mutate} playerNo={playerNo} room={room} key={playerNo} />)}</div>
  </section>;
}

function RoomActions({ busy, mutate, onConfirm, room, self, terminal }: {
  busy: boolean;
  mutate: RoomMutation;
  onConfirm: (action: "leave" | "close") => void;
  room: NetplayRoom;
  self: NetplayRoom["members"][number] | undefined;
  terminal: boolean;
}) {
  if (terminal) {return <div className="netplay-terminal"><h2>本次联机已结束</h2><p>原因：{room.endReason ?? "NORMAL"}</p><Link className="button" href="/netplay">返回联机首页</Link></div>;}
  return <div className="netplay-actions">
    {self && room.permissions.canReady ? <button className={`button${self.ready ? " secondary" : ""}`} type="button" disabled={busy} onClick={() => void mutate(`/api/v1/netplay/rooms/${room.roomId}/members/me/ready`, "PUT", { ready: !self.ready }, true)}>{self.ready ? "取消准备" : "准备"}</button> : null}
    {room.permissions.host ? <><button className="button" type="button" disabled={busy || !room.permissions.canStart} onClick={() => void mutate(`/api/v1/netplay/rooms/${room.roomId}/start`, "POST", {})}>开始联机</button><button className="button secondary" type="button" disabled={busy || room.state !== "WAITING"} onClick={() => void mutate(`/api/v1/netplay/rooms/${room.roomId}/game`, "DELETE")}>更换游戏</button><button className="button danger" type="button" disabled={busy} onClick={() => onConfirm("close")}>关闭房间</button></> : self ? <button className="button secondary" type="button" disabled={busy} onClick={() => onConfirm("leave")}>离开房间</button> : null}
  </div>;
}

function ActiveRoomView({ busy, confirmAction, copied, error, mutate, onConfirm, onConfirmCancel, onCopy, room, self, terminal }: {
  busy: boolean;
  confirmAction: "leave" | "close" | null;
  copied: boolean;
  error: string;
  mutate: RoomMutation;
  onConfirm: (action: "leave" | "close") => void;
  onConfirmCancel: () => void;
  onCopy: () => void;
  room: NetplayRoom;
  self: NetplayRoom["members"][number] | undefined;
  terminal: boolean;
}) {
  const title = room.game?.title ?? "等待房主选择游戏";
  const description = room.game ? `${room.game.platformName} · ${room.game.coreName} · EmulatorJS ${room.game.emulatorjsVersion}` : "游戏锁定后即可选择座位。";
  const statusTone = room.state === "RUNNING" ? "good" : terminal ? "neutral" : "info";
  const closing = confirmAction === "close";
  return <div className="page-layout netplay-room-page">
    <PageHeader eyebrow={`房间 #${room.roomId.slice(0, 8)}`} title={title} description={description} actions={<Link className="button secondary" href="/netplay">联机首页</Link>} />
    {error ? <div className="feedback-banner bad" role="alert"><div>{error}</div></div> : null}
    <div className="netplay-room-summary"><div><StatusBadge tone={statusTone}>{room.state}</StatusBadge><span>{room.members.length} 位玩家</span></div>{room.game && !terminal ? <button className="button secondary" type="button" onClick={onCopy}>{copied ? "已复制链接" : "复制房间链接"}</button> : null}</div>
    <RoomSeats busy={busy} mutate={mutate} room={room} />
    <RoomActions busy={busy} mutate={mutate} onConfirm={onConfirm} room={room} self={self} terminal={terminal} />
    <ConfirmDialog open={confirmAction !== null} title={closing ? "关闭联机房间？" : "离开联机房间？"} tone="danger" busy={busy} confirmLabel={closing ? "关闭房间" : "离开房间"} onCancel={onConfirmCancel} onConfirm={() => {
      onConfirmCancel();
      if (closing) {void mutate(`/api/v1/netplay/rooms/${room.roomId}`, "DELETE");}
      else {void mutate(`/api/v1/netplay/rooms/${room.roomId}/members/me`, "DELETE");}
    }}>{closing ? "关闭会立即撤销当前本局所有玩家的联机凭据。" : "运行中离开会结束所有参与者的当前本局，并释放你的座位。"}</ConfirmDialog>
  </div>;
}

export function NetplayRoomLobby({ initialRoom, games, initialGamesNextCursor = null, initialFilterParams = {} }: {
  initialRoom: NetplayRoom; games: NetplayGame[]; initialGamesNextCursor?: string | null; initialFilterParams?: NetplayFilterParams;
}) {
  const { authenticatedFetch } = useAuth();
  const router = useRouter();
  const [room, setRoom] = useState(initialRoom);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [copied, setCopied] = useState(false);
  const [confirmAction, setConfirmAction] = useState<"leave" | "close" | null>(null);
  const [profileGame, setProfileGame] = useState<NetplayGame | null>(null);
  const roomRef = useRef(initialRoom);
  const launchSession = useRef<string | null>(null);
  const pollTimer = useRef<number | null>(null);

  const applyIncomingRoom = useCallback((incoming: NetplayRoom) => {
    const applied = applyRoomSnapshot(roomRef.current, incoming);
    roomRef.current = applied.room;
    setRoom((current) => applyRoomSnapshot(current, incoming).room);
    return applied;
  }, []);

  const refresh = useCallback(async () => {
    const response = await authenticatedFetch(`/api/v1/netplay/rooms/${initialRoom.roomId}`, { cache: "no-store" });
    if (!response.ok) {throw new Error("无法刷新房间状态");}
    const next = await response.json() as NetplayRoom;
    return applyIncomingRoom(next).room;
  }, [applyIncomingRoom, authenticatedFetch, initialRoom.roomId]);

  useEffect(() => {
    let failures = 0;
    const source = new EventSource(`/api/v1/netplay/rooms/${initialRoom.roomId}/events`, { withCredentials: true });
    const receive = (event: MessageEvent<string>) => {
      failures = 0;
      try {
        const incoming = JSON.parse(event.data) as NetplayRoom;
        const applied = applyIncomingRoom(incoming);
        if (applied.gap) {void refresh();}
      } catch { void refresh(); }
    };
    for (const name of ["room.snapshot", "room.updated", "member.updated", "session.updated", "room.ended"]) {source.addEventListener(name, receive as EventListener);}
    source.onerror = () => {
      failures += 1;
      if (failures < 3) {return;}
      source.close();
      if (pollTimer.current === null) {pollTimer.current = window.setInterval(() => void refresh().catch(() => undefined), 5_000);}
    };
    return () => { source.close(); if (pollTimer.current !== null) {window.clearInterval(pollTimer.current);} };
  }, [applyIncomingRoom, initialRoom.roomId, refresh]);

  useEffect(() => {
    if (!copied) {return;}
    const timer = window.setTimeout(() => setCopied(false), 2_400);
    return () => window.clearTimeout(timer);
  }, [copied]);

  useEffect(() => {
    const session = room.currentSession;
    if (room.state !== "STARTING" || !session || !room.permissions.member || launchSession.current === session.sessionId) {return;}
    launchSession.current = session.sessionId;
    void roomMutation<NetplayLaunch>(authenticatedFetch, `/api/v1/netplay/rooms/${room.roomId}/sessions/${session.sessionId}/launch`, "POST", {
      body: { clientCapabilities: { secureContext: window.isSecureContext, crossOriginIsolated: window.crossOriginIsolated, sharedArrayBuffer: typeof SharedArrayBuffer !== "undefined" } },
    }).then((created) => window.location.replace(created.playUrl)).catch((caught: unknown) => {
      launchSession.current = null; setError(caught instanceof Error ? caught.message : "无法准备联机运行环境");
    });
  }, [authenticatedFetch, room]);

  async function mutate(
    path: string,
    method: "POST" | "PUT" | "DELETE",
    body?: unknown,
    retryReadyPrecondition = false,
  ) {
    if (busy) {return;}
    setBusy(true); setError("");
    try {
      let next: NetplayRoom | null;
      try {
        next = await roomMutation<NetplayRoom | null>(authenticatedFetch, path, method, {
          version: roomRef.current.version, body,
        });
      } catch (caught) {
        if (!(retryReadyPrecondition && caught instanceof NetplayAPIError && caught.code === "PRECONDITION_FAILED")) {
          throw caught;
        }
        const latest = await refresh();
        const desired = (body as { ready?: boolean } | undefined)?.ready;
        const currentSelf = latest.members.find((member) => member.memberId === latest.selfMemberId);
        if (currentSelf?.ready === desired) {return;}
        if (latest.state !== "WAITING" || !latest.permissions.canReady) {throw caught;}
        next = await roomMutation<NetplayRoom | null>(authenticatedFetch, path, method, {
          version: latest.version, body,
        });
      }
      if (next) {applyIncomingRoom(next);}
      else {router.replace("/netplay");}
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "联机操作失败");
      await refresh().catch(() => undefined);
    } finally { setBusy(false); }
  }

  function selectGame(game: NetplayGame, profileId?: string) {
    if (!profileId && game.netplayProfiles.length > 1) { setProfileGame(game); return; }
    const profile = profileId ?? game.netplayProfiles[0]?.id;
    if (!profile) {return;}
    setProfileGame(null);
    void mutate(`/api/v1/netplay/rooms/${room.roomId}/game`, "PUT", { gameId: game.gameId, netplayProfileId: profile });
  }

  async function copyRoomLink() {
    try {
      if (navigator.clipboard?.writeText) {await navigator.clipboard.writeText(window.location.href);}
      else {
        const field = document.createElement("textarea");
        field.value = window.location.href; field.setAttribute("readonly", ""); field.style.position = "fixed"; field.style.opacity = "0";
        document.body.append(field); field.select();
        if (!document.execCommand("copy")) {throw new Error("copy unavailable");}
        field.remove();
      }
      setCopied(true);
    } catch {
      setError("浏览器未允许复制，请从地址栏复制房间链接。");
    }
  }

  const self = room.members.find((member) => member.memberId === room.selfMemberId);
  const terminal = room.state === "ENDED" || room.state === "EXPIRED";
  if (room.state === "DRAFT" && room.permissions.host) {
    return <DraftRoomView authenticatedFetch={authenticatedFetch} busy={busy} error={error} games={games} initialFilterParams={initialFilterParams} initialNextCursor={initialGamesNextCursor} onProfileClose={() => setProfileGame(null)} onSelect={selectGame} profileGame={profileGame} room={room} />;
  }
  return <ActiveRoomView busy={busy} confirmAction={confirmAction} copied={copied} error={error} mutate={mutate} onConfirm={setConfirmAction} onConfirmCancel={() => setConfirmAction(null)} onCopy={() => void copyRoomLink()} room={room} self={self} terminal={terminal} />;
}

function ProfileDialog({ game, onClose, onSelect }: { game: NetplayGame; onClose: () => void; onSelect: (id: string) => void }) {
  const dialog = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const buttons = () => Array.from(dialog.current?.querySelectorAll<HTMLButtonElement>("button:not(:disabled)") ?? []);
    buttons()[0]?.focus();
    const keydown = (event: KeyboardEvent) => {
      if (event.key === "Escape") { event.preventDefault(); onClose(); return; }
      if (event.key !== "Tab") {return;}
      const items = buttons();
      if (!items.length) {return;}
      const current = items.indexOf(document.activeElement as HTMLButtonElement);
      const next = event.shiftKey ? (current - 1 + items.length) % items.length : (current + 1) % items.length;
      event.preventDefault(); items[next]?.focus();
    };
    document.addEventListener("keydown", keydown);
    return () => { document.removeEventListener("keydown", keydown); previous?.focus(); };
  }, [onClose]);
  return <div className="netplay-dialog-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) {onClose();} }}><div ref={dialog} className="netplay-profile-dialog" role="dialog" aria-modal="true" aria-labelledby="profile-dialog-title"><h2 id="profile-dialog-title">选择 {game.title} 的联机配置</h2><p>每位玩家将使用完全相同的核心和运行时。</p><div>{game.netplayProfiles.map((profile) => <button key={profile.id} type="button" onClick={() => onSelect(profile.id)}><strong>{profile.coreName}</strong><span>EmulatorJS {profile.emulatorjsVersion} · 最多 {profile.maxPlayers} 人</span></button>)}</div><button className="button secondary" type="button" onClick={onClose}>取消</button></div></div>;
}
