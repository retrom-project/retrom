"use client";

import Image from "next/image";
import { useId, useRef, useState, type ChangeEvent } from "react";
import type { AdminGameManagerViewProps } from "./admin-game-manager-view";

type MediaProps = Pick<AdminGameManagerViewProps, "clientReady" | "cover" | "disabled" | "game" | "onRemoveVideo" | "onReplaceAsset" | "video">;

export function AdminGameMedia(props: MediaProps) {
  const [selected, setSelected] = useState(0);
  const id = useId();
  const tabs = useRef<HTMLDivElement>(null);
  const upload = useRef<HTMLInputElement>(null);
  const isVideo = selected === 1;
  const asset = isVideo ? props.video : props.cover;
  const disabled = props.disabled || (isVideo && !props.clientReady);
  const replace = (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    if (file) {props.onReplaceAsset(file, isVideo ? "VIDEO" : "COVER", 0);}
    event.target.value = "";
  };

  return <section className="panel admin-game-media" id="admin-game-media">
    <div className="panel-head">
      <h2>媒体</h2>
      <div className="admin-game-media-tabs" ref={tabs} role="tablist" aria-label="媒体内容">
        {["封面", "视频"].map((label, index) => <button
          className="button secondary option-tab" type="button" role="tab" key={label}
          id={`${id}-tab-${index}`} aria-controls={`${id}-panel-${index}`}
          aria-selected={selected === index} tabIndex={selected === index ? 0 : -1}
          onClick={() => setSelected(index)}
          onKeyDown={(event) => {
            const next = event.key === "Home" ? 0 : event.key === "End" ? 1
              : event.key === "ArrowLeft" || event.key === "ArrowRight" ? 1 - index : -1;
            if (next < 0) {return;}
            event.preventDefault();
            setSelected(next);
            tabs.current?.querySelectorAll<HTMLButtonElement>("button")[next]?.focus();
          }}
        >{label}</button>)}
      </div>
    </div>
    <div className="panel-body admin-game-media-body">
      <div className="admin-game-media-stage">
        <div className="admin-game-media-panel" role="tabpanel" id={`${id}-panel-0`} aria-labelledby={`${id}-tab-0`} hidden={isVideo} tabIndex={0}>
          <CoverPreview cover={props.cover} title={props.game.title} />
        </div>
        <div className="admin-game-media-panel" role="tabpanel" id={`${id}-panel-1`} aria-labelledby={`${id}-tab-1`} hidden={!isVideo} tabIndex={0}>
          {isVideo && props.video ? <video src={props.video.url} controls playsInline preload="metadata" aria-label={`${props.game.title} 管理视频预览`} /> : <span className="admin-game-media-empty"><strong>暂无视频</strong><small>支持 MP4 / WebM，最大 256 MiB</small></span>}
        </div>
      </div>
      <footer className="admin-game-media-footer">
        <span>{isVideo ? "视频保留原始比例" : coverDimensions(props.cover)}</span>
        <div>
          <input ref={upload} hidden type="file" aria-label={isVideo ? "上传视频" : "上传封面"} accept={isVideo ? "video/mp4,video/webm" : "image/png,image/jpeg,image/webp"} disabled={disabled} onChange={replace} />
          <button className="button secondary" type="button" disabled={disabled} onClick={() => upload.current?.click()}>{asset ? "替换" : "添加"}{isVideo ? "视频" : "封面"}</button>
          {isVideo && props.video ? <button className="button secondary" type="button" disabled={props.disabled} onClick={props.onRemoveVideo}>移除视频</button> : null}
        </div>
      </footer>
    </div>
  </section>;
}

function CoverPreview({ cover, title }: { cover: MediaProps["cover"]; title: string }) {
  return <div className="admin-game-cover-frame">
    {cover ? <Image src={cover.url} alt={`${title} 封面`} fill sizes="(min-width: 1400px) 360px, 260px" unoptimized /> : <span>暂无封面</span>}
  </div>;
}

function coverDimensions(cover: MediaProps["cover"]) {
  return cover?.widthPx && cover.heightPx ? `${cover.widthPx} × ${cover.heightPx}` : "建议使用 3:4 图片";
}
