"use client";

import Link from "next/link";
import { useState } from "react";
import { useRouter } from "next/navigation";
import { EmptyState, PageHeader, StatusBadge } from "@/components/ui";
import { useAuth } from "@/features/auth/auth-provider";
import { roomMutation, type NetplayRoom } from "./client";

function RoomCard({ room }: { room: NetplayRoom }) {
  return <Link className="netplay-room-card" href={`/netplay/rooms/${room.roomId}`}>
    <div><StatusBadge tone={room.state === "RUNNING" ? "good" : room.state === "ENDED" || room.state === "EXPIRED" ? "neutral" : "warn"}>{room.state}</StatusBadge><span>#{room.roomId.slice(0, 8)}</span></div>
    <h2>{room.game?.title ?? "等待选择游戏"}</h2>
    <p>{room.game ? `${room.game.platformName} · ${room.game.coreName}` : "创建后先选择一个经过验证的联机游戏"}</p>
    <footer><span>{room.members.length} / {room.game?.maxPlayers ?? 4} 位玩家</span><strong>进入房间 →</strong></footer>
  </Link>;
}

export function NetplayRoomList({ active, recent }: { active: NetplayRoom[]; recent: NetplayRoom[] }) {
  const { authenticatedFetch } = useAuth();
  const router = useRouter();
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState("");

  async function createRoom() {
    if (creating) return;
    setCreating(true); setError("");
    try {
      const room = await roomMutation<NetplayRoom>(authenticatedFetch, "/api/v1/netplay/rooms", "POST", { body: {} });
      router.replace(`/netplay/rooms/${room.roomId}`);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "创建房间失败");
      setCreating(false);
    }
  }

  return <div className="page-layout netplay-page">
    <PageHeader eyebrow="NETPLAY" title="联机游玩" description="每位玩家在自己的浏览器运行同一套已验证内容，服务器只同步控制输入与确定性状态。" actions={<button className="button" type="button" disabled={creating} onClick={() => void createRoom()}>{creating ? "正在创建…" : "创建房间"}</button>} />
    {error ? <div className="feedback-banner bad" role="alert"><div>{error}</div></div> : null}
    <section className="netplay-room-section"><div className="netplay-section-head"><div><h2>当前房间</h2><p>你主持或已经占座的房间</p></div><span>{active.length}</span></div>
      {active.length ? <div className="netplay-room-grid">{active.map((room) => <RoomCard key={room.roomId} room={room} />)}</div> : <EmptyState title="还没有当前房间" description="创建房间、选择游戏，再把本站房间链接分享给已登录的朋友。" />}
    </section>
    <section className="netplay-room-section"><div className="netplay-section-head"><div><h2>最近联机</h2><p>过去 24 小时内你参与的终局</p></div><span>{recent.length}</span></div>
      {recent.length ? <div className="netplay-room-grid">{recent.map((room) => <RoomCard key={room.roomId} room={room} />)}</div> : <p className="netplay-muted">暂无最近结束的联机房间。</p>}
    </section>
  </div>;
}
