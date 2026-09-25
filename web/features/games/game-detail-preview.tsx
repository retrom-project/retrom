"use client";

import { useRef, useState } from "react";
import { ResponsiveSheet } from "@/components/responsive-sheet";
import { saveDisplayTime, type SaveItem } from "@/features/saves/save-library";
import { SaveScreenshot } from "@/features/saves/save-screenshot";
import { SaveSizeLabel } from "@/features/saves/save-size-label";
import { useSaveTimeFormatter } from "@/features/saves/use-save-time";
import { GameDetailMedia } from "./game-detail-media";

export function GameDetailPreview({ title, coverUrl, videoUrl, save, nowMs }: {
  title: string; coverUrl: string | null; videoUrl: string | null; save: SaveItem | null; nowMs: number;
}) {
  const [video, setVideo] = useState(!save);
  const [open, setOpen] = useState(false);
  const trigger = useRef<HTMLButtonElement>(null);
  const formatTime = useSaveTimeFormatter();
  return <aside className="game-detail-feature-preview" aria-label="游戏预览">
    <header className="game-detail-preview-heading">
      <strong>{video ? "视频预览" : "将从这里继续"}</strong>
      {save && videoUrl ? <button className="button secondary" type="button" onClick={() => setVideo(!video)}>{video ? "查看最近存档" : "查看视频"}</button> : null}
    </header>
    {video ? <GameDetailMedia title={title} coverUrl={coverUrl} videoUrl={videoUrl} landscape /> : save ? <>
      <button ref={trigger} type="button" className="game-detail-feature-shot" disabled={!save.screenshotUrl} aria-label={save.screenshotUrl ? "查看最近存档大图" : "最近存档没有截图"} onClick={() => setOpen(true)}>
        <SaveScreenshot screenshotUrl={save.screenshotUrl} alt={`${title} 最近存档`} sizes="(min-width: 1600px) 520px, 60vw" />
        <SaveSizeLabel sizeBytes={save.sizeBytes} />
      </button>
      <div className="game-detail-preview-caption"><span><time dateTime={new Date(saveDisplayTime(save)).toISOString()}>{formatTime(saveDisplayTime(save), nowMs)}</time> · {save.core.name}{save.discLabel ? ` · ${save.discLabel}` : ""}</span>{save.screenshotUrl ? <span aria-hidden="true">查看大图</span> : null}</div>
      <ResponsiveSheet open={open} title="存档截图预览" placement="fullscreen" onClose={() => setOpen(false)} returnFocusRef={trigger} className="game-detail-feature-lightbox">
        <div className="game-detail-preview-image"><SaveScreenshot screenshotUrl={save.screenshotUrl} alt={`${title} 存档截图完整预览`} width={1920} height={1080} /><SaveSizeLabel sizeBytes={save.sizeBytes} /></div>
      </ResponsiveSheet>
    </> : null}
  </aside>;
}
