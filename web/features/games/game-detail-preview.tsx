"use client";

import { Fragment, useId, useRef, useState } from "react";
import { type SaveItem } from "@/features/saves/save-library";
import { SaveScreenshot } from "@/features/saves/save-screenshot";
import { SaveSizeLabel } from "@/features/saves/save-size-label";
import { GameDetailMedia } from "./game-detail-media";

export function GameDetailPreview({ title, coverUrl, videoUrl, save }: {
  title: string; coverUrl: string | null; videoUrl: string | null; save: SaveItem | null;
}) {
  const [selectedVideo, setVideo] = useState(!save);
  const video = Boolean(videoUrl) && (selectedVideo || !save);
  const tabs = [{label: "最近存档", index: 0, available: Boolean(save)}, {label: "视频预览", index: 1, available: Boolean(videoUrl)}].filter(tab => tab.available);
  const id = useId();
  const tabsRef = useRef<HTMLDivElement>(null);
  const active = video ? 1 : 0;
  if (!tabs.length) {return null;}
  return <aside className="game-detail-feature-preview" aria-label="游戏预览">
    <div className="game-detail-preview-frame">
      <header className="game-detail-preview-heading is-overlay"><div ref={tabsRef} className="game-detail-preview-tabs" role="tablist" aria-label="游戏预览内容">
          {tabs.map(({label, index}, position) => <Fragment key={label}>
            {position > 0 ? <span className="game-detail-preview-divider" aria-hidden="true">/</span> : null}
            <button type="button" className="button secondary option-tab" role="tab" id={`${id}-tab-${index}`} aria-controls={`${id}-panel-${index}`} aria-selected={index === active} tabIndex={index === active ? 0 : -1} onClick={() => setVideo(index === 1)} onKeyDown={event => {
              const next = event.key === "Home" ? 0 : event.key === "End" ? tabs.length - 1
                : event.key === "ArrowLeft" || event.key === "ArrowRight" ? (position + 1) % tabs.length : -1;
              if (next < 0) {return;}
              event.preventDefault();
              setVideo(tabs[next].index === 1);
              tabsRef.current?.querySelectorAll<HTMLButtonElement>("button")[next]?.focus();
            }}>{label}</button>
          </Fragment>)}
      </div></header>
      {save ? <div className="game-detail-preview-panel" hidden={video} id={`${id}-panel-0`} role="tabpanel" aria-labelledby={`${id}-tab-0`}>
        {save ? <div className="game-detail-feature-shot">
          <SaveScreenshot screenshotUrl={save.screenshotUrl} alt={`${title} 最近存档`} sizes="(min-width: 1600px) 520px, 60vw" />
          <SaveSizeLabel sizeBytes={save.sizeBytes} />
        </div> : null}
      </div> : null}
      {videoUrl ? <div className="game-detail-preview-panel" hidden={!video} id={`${id}-panel-1`} role="tabpanel" aria-labelledby={`${id}-tab-1`}>
        {video ? <GameDetailMedia title={title} coverUrl={coverUrl} videoUrl={videoUrl} landscape /> : null}
      </div> : null}
    </div>
  </aside>;
}
