"use client";

import { useState } from "react";
import { type SaveItem } from "@/features/saves/save-library";
import { SaveScreenshot } from "@/features/saves/save-screenshot";
import { SaveSizeLabel } from "@/features/saves/save-size-label";
import { GameDetailMedia } from "./game-detail-media";

export function GameDetailPreview({ title, coverUrl, videoUrl, save }: {
  title: string; coverUrl: string | null; videoUrl: string | null; save: SaveItem | null;
}) {
  const [video, setVideo] = useState(!save);
  return <aside className="game-detail-feature-preview" aria-label="游戏预览">
    <header className="game-detail-preview-heading">
      <strong>{video ? "视频预览" : "将从这里继续"}</strong>
      {save && videoUrl ? <button className="button secondary" type="button" onClick={() => setVideo(!video)}>{video ? "查看最近存档" : "查看视频"}</button> : null}
    </header>
    {video ? <GameDetailMedia title={title} coverUrl={coverUrl} videoUrl={videoUrl} landscape /> : save ? <div className="game-detail-feature-shot">
      <SaveScreenshot screenshotUrl={save.screenshotUrl} alt={`${title} 最近存档`} sizes="(min-width: 1600px) 520px, 60vw" />
      <SaveSizeLabel sizeBytes={save.sizeBytes} />
    </div> : null}
  </aside>;
}
