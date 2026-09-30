"use client";

import { Fragment, useId, useRef, useState } from "react";
import { type SaveItem } from "@/features/saves/save-library";
import { SaveScreenshot } from "@/features/saves/save-screenshot";
import { SaveSizeLabel } from "@/features/saves/save-size-label";
import { GameDetailMedia } from "./game-detail-media";

export function GameDetailPreview({ title, coverUrl, videoUrl, save }: {
  title: string; coverUrl: string | null; videoUrl: string | null; save: SaveItem | null;
}) {
  const [video, setVideo] = useState(!save);
  const id = useId();
  const tabsRef = useRef<HTMLDivElement>(null);
  const tabbed = Boolean(save && videoUrl);
  const active = video ? 1 : 0;
  return <aside className="game-detail-feature-preview" aria-label="游戏预览">
    <header className="game-detail-preview-heading">
      {tabbed ? <div ref={tabsRef} className="game-detail-preview-tabs" role="tablist" aria-label="游戏预览内容">
        {["最近存档", "视频预览"].map((label, index) => <Fragment key={label}>
          {index > 0 ? <span className="game-detail-preview-divider" aria-hidden="true">/</span> : null}
          <button type="button" className="button secondary option-tab" role="tab" id={`${id}-tab-${index}`} aria-controls={`${id}-panel-${index}`} aria-selected={index === active} tabIndex={index === active ? 0 : -1} onClick={() => setVideo(index === 1)} onKeyDown={event => {
            const next = event.key === "Home" ? 0 : event.key === "End" ? 1
              : event.key === "ArrowLeft" || event.key === "ArrowRight" ? 1 - index : -1;
            if (next < 0) {return;}
            event.preventDefault();
            setVideo(next === 1);
            tabsRef.current?.querySelectorAll<HTMLButtonElement>("button")[next]?.focus();
          }}>{label}</button>
        </Fragment>)}
      </div> : <strong>{video ? "视频预览" : "将从这里继续"}</strong>}
    </header>
    <div className="game-detail-preview-panel" hidden={video} id={tabbed ? `${id}-panel-0` : undefined} role={tabbed ? "tabpanel" : undefined} aria-labelledby={tabbed ? `${id}-tab-0` : undefined}>
      {save ? <div className="game-detail-feature-shot">
        <SaveScreenshot screenshotUrl={save.screenshotUrl} alt={`${title} 最近存档`} sizes="(min-width: 1600px) 520px, 60vw" />
        <SaveSizeLabel sizeBytes={save.sizeBytes} />
      </div> : null}
    </div>
    <div className="game-detail-preview-panel" hidden={!video} id={tabbed ? `${id}-panel-1` : undefined} role={tabbed ? "tabpanel" : undefined} aria-labelledby={tabbed ? `${id}-tab-1` : undefined}>
      {video ? <GameDetailMedia title={title} coverUrl={coverUrl} videoUrl={videoUrl} landscape /> : null}
    </div>
  </aside>;
}
