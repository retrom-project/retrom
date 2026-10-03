"use client";

import Image from "next/image";
import { useEffect, useId, useRef, useState } from "react";
import type { PreviewAsset } from "./review-actions-model";

export function ReviewMediaEditor({ cover, videoUrl, disabled, restoreLabel, onUpload, onRestore, videoRestoreLabel, onUploadVideo, onRestoreVideo }: {
  cover: PreviewAsset | null;
  videoUrl: string | null;
  videoRestoreLabel: string | null;
  onUploadVideo: (file: File) => void;
  onRestoreVideo: () => void;
  disabled: boolean;
  restoreLabel: string | null;
  onUpload: (file: File) => void;
  onRestore: () => void;
}) {
  const [selected, setSelected] = useState(0);
  const id = useId();
  const tabs = useRef<HTMLDivElement>(null);
  const upload = useRef<HTMLInputElement>(null);
  return <aside className="review-cover-panel review-workflow-cover-side" aria-label="发布媒体">
    <div className="review-media-tabs" ref={tabs} role="tablist" aria-label="发布媒体内容">
      {["封面", "视频"].map((label, index) => <button
        key={label} type="button" className="button secondary option-tab" role="tab"
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
    <div className="review-media-stage">
      <div className="review-media-panel" role="tabpanel" id={`${id}-panel-0`} aria-labelledby={`${id}-tab-0`} hidden={selected !== 0} tabIndex={0}>
        {cover ? <Image src={cover.url} alt="当前选择的游戏封面" width={cover.width} height={cover.height} unoptimized /> : <span className="review-media-empty">暂无封面</span>}
      </div>
      <div className="review-media-panel" role="tabpanel" id={`${id}-panel-1`} aria-labelledby={`${id}-tab-1`} hidden={selected !== 1} tabIndex={0}>
        {selected === 1 && videoUrl ? <ReviewVideo key={videoUrl} url={videoUrl} /> : <span className="review-media-empty">暂无视频</span>}
      </div>
    </div>
    <footer className="review-media-footer">
      <input ref={upload} hidden type="file" aria-label={selected === 0 ? "上传封面" : "上传视频"} accept={selected === 0 ? "image/png,image/jpeg,image/webp" : "video/mp4,video/webm"} disabled={disabled} onChange={(event) => {
        const file = event.currentTarget.files?.[0];
        if (file) {(selected === 0 ? onUpload : onUploadVideo)(file);}
        event.currentTarget.value = "";
      }} />
      <button type="button" className="button secondary" disabled={disabled} onClick={() => upload.current?.click()}>{selected === 0 ? cover ? "替换封面" : "上传封面" : videoUrl ? "替换视频" : "上传视频"}</button>
      {(selected === 0 ? restoreLabel : videoRestoreLabel) ? <button type="button" className="button secondary" disabled={disabled} onClick={selected === 0 ? onRestore : onRestoreVideo}>{selected === 0 ? restoreLabel : videoRestoreLabel}</button> : null}
      <small>{selected === 0 ? "PNG / JPEG / WebP · 最大 10 MiB" : "MP4 / WebM · 最大 256 MiB"}</small>
    </footer>
  </aside>;
}

function ReviewVideo({ url }: { url: string }) {
  const ref = useRef<HTMLVideoElement>(null);
  useEffect(() => {
    const player = ref.current;
    return () => player?.pause();
  }, []);
  return <video ref={ref} controls playsInline preload="metadata" src={url} aria-label="视频预览">浏览器无法播放这段视频。</video>;
}
