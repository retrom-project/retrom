"use client";
import Image from "next/image";
import Link from "next/link";
import { useState } from "react";
import type { Schema } from "@/lib/api/types";
import { AppIcon } from "@/components/app-icon";
import { DetailLaunch } from "./detail-launch";
import { SaveCard } from "@/features/saves/save-card";
import { FavoriteOrganizer } from "@/features/favorites/favorite-organizer";
type DetailProps = {
  detail: Schema<"GameDetail">;
  mode: "user" | "admin" | "review";
  onFavorite: () => void;
  onReview: (action: "approve" | "discard") => void;
  onChange: () => void;
  error: string;
};
export function GameDetailContent(props: DetailProps) {
  const { detail, mode, onChange } = props;
  const { game } = detail;
  const latest = detail.saves.find((save) => save.restorable);
  const video = game.media.find((media) => media.kind === "video");
  const cover = game.media.find((media) => media.kind === "cover");
  const hasPreview = !!(latest?.screenshotUrl || video);
  return (
    <div className="game-detail-content">
      <nav className="game-detail-breadcrumb" aria-label="返回导航">
        <Link href={mode === "review" ? "/admin/reviews" : "/library"}>
          <AppIcon name="arrow-left" />
          {mode === "review" ? "返回待审核" : "返回游戏库"}
        </Link>
      </nav>
      <section
        className={`game-detail-hero${hasPreview ? " has-preview" : ""}`}
      >
        <div className="game-detail-poster-shell">
          <div className="game-detail-media">
            <div className="game-detail-poster">
              {cover ? (
                <Image
                  src={cover.url}
                  alt={`${game.title}封面`}
                  fill
                  unoptimized
                  sizes="260px"
                />
              ) : (
                <div className="game-detail-media-placeholder">
                  <span>{game.title}</span>
                </div>
              )}
            </div>
          </div>
        </div>
        <DetailControls {...props} />
        {hasPreview ? <DetailPreview save={latest} video={video} /> : null}
      </section>
      <DetailOverview game={game} />
      <DetailSaves detail={detail} onChange={onChange} />
    </div>
  );
}
function DetailControls({
  detail,
  mode,
  onFavorite,
  onReview,
  onChange,
  error,
}: DetailProps) {
  const { game } = detail;
  return (
    <div className="game-detail-main">
      <p className="game-detail-eyebrow">{game.directoryName}</p>
      <div className="game-detail-title-row">
        <h1>{game.title}</h1>
        {mode !== "review" ? (
          <div className="favorite-actions favorite-actions-detail">
            <button
              className={`favorite-heart${game.favorite ? " is-favorite" : ""}`}
              aria-label={game.favorite ? "取消收藏" : "收藏游戏"}
              aria-pressed={game.favorite}
              onClick={onFavorite}
            >
              <AppIcon name="heart" />
            </button>
          </div>
        ) : null}
      </div>
      <div className="workspace-tags">
        {game.tags.map((tag) => (
          <Link
            className="status neutral"
            href={`/library?tagId=${tag.id}`}
            key={tag.id}
          >
            {tag.name}
          </Link>
        ))}
      </div>
      <DetailLaunch detail={detail} review={mode === "review"} error={error} />{" "}
      {mode === "review" ? (
        <div className="workspace-actions workspace-section">
          <button className="button danger" onClick={() => onReview("discard")}>
            拒绝 / 丢弃
          </button>
          <button className="button" onClick={() => onReview("approve")}>
            批准入库
          </button>
        </div>
      ) : (
        <FavoriteOrganizer detail={detail} onChange={onChange} />
      )}
    </div>
  );
}
function DetailPreview({
  save,
  video,
}: {
  save: Schema<"Save"> | undefined;
  video: Schema<"GameMedia"> | undefined;
}) {
  const [preview, setPreview] = useState<"save" | "video">("save");
  const showVideo = (preview === "video" || !save?.screenshotUrl) && video;
  return (
    <section className="game-detail-feature-preview">
      <div className="game-detail-preview-frame">
        <div className="game-detail-preview-heading is-overlay">
          <div className="game-detail-preview-tabs">
            {save?.screenshotUrl ? (
              <button
                className="button secondary"
                aria-pressed={preview === "save"}
                onClick={() => setPreview("save")}
              >
                最近存档
              </button>
            ) : null}
            {video ? (
              <button
                className="button secondary"
                aria-pressed={preview === "video"}
                onClick={() => setPreview("video")}
              >
                视频预览
              </button>
            ) : null}
          </div>
        </div>
        <div className="game-detail-feature-shot">
          {showVideo ? (
            <video
              src={video.url}
              controls
              playsInline
              preload="metadata"
              className="game-detail-media-video is-playing"
            />
          ) : save?.screenshotUrl ? (
            <Image
              src={save.screenshotUrl}
              alt="最近存档画面"
              fill
              unoptimized
              sizes="640px"
            />
          ) : null}
        </div>
      </div>
    </section>
  );
}
function DetailOverview({ game }: { game: Schema<"Game"> }) {
  const facts = [
    ["发行年份", game.releaseYear],
    ["玩家数", game.players],
    ["开发商", game.developer],
    ["发行商", game.publisher],
    ["类型", game.genre],
    ["游戏平台", game.platformId],
    ["游戏目录", game.directoryName],
  ];
  return (
    <div className="game-detail-overview">
      <section className="game-detail-about">
        <h2>关于游戏</h2>
        <div className="game-detail-description">
          <p>{game.description || "暂无介绍。"}</p>
        </div>
      </section>
      <section className="game-detail-facts">
        <h2>游戏资料</h2>
        <div className="game-detail-info-strip">
          {facts.map(([label, value]) => (
            <div className="game-detail-fact" key={label}>
              <span>{label}</span>
              <strong>{value ?? "—"}</strong>
            </div>
          ))}
        </div>
      </section>
    </div>
  );
}
function DetailSaves({
  detail,
  onChange,
}: {
  detail: Schema<"GameDetail">;
  onChange: () => void;
}) {
  return (
    <section className="game-detail-saves">
      <div className="game-detail-saves-head">
        <div>
          <h2>游戏存档</h2>
          <p>最近 3 份游戏数据；原生存档恢复后请在游戏内读档。</p>
        </div>
        <Link href={`/saves?gameId=${detail.game.id}`}>查看全部存档</Link>
      </div>
      {detail.saves.length ? (
        <div className="game-detail-save-grid">
          {detail.saves.slice(0, 3).map((save) => (
            <SaveCard compact save={save} key={save.id} onChange={onChange} />
          ))}
        </div>
      ) : (
        <div className="game-detail-saves-empty">
          暂无存档，开始游戏后可以保存进度。
        </div>
      )}
    </section>
  );
}
