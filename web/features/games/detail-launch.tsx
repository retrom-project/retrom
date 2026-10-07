"use client";
import { useState } from "react";
import type { ReactNode } from "react";
import type { Schema } from "@/lib/api/types";
import { usePhoneLayout } from "@/lib/use-phone-layout";
import { LaunchButton } from "@/features/player/launch-button";
import { ResponsiveSheet } from "@/components/responsive-sheet";
import { AppIcon } from "@/components/app-icon";
import { api, result } from "@/lib/api/client";
import { useResource } from "@/lib/use-resource";
import { targetContentLoading } from "@/features/player/content-loading";
import type { ContentLoading } from "@/features/player/content-loading";
async function loadCatalog() {
  return result(await api.GET("/api/v1/runtime/catalog"));
}
export function DetailLaunch({
  detail,
  review,
}: {
  detail: Schema<"GameDetail">;
  review: boolean;
}) {
  const [core, setCore] = useState(detail.defaultCoreId);
  const [options, setOptions] = useState(false);
  const phone = usePhoneLayout();
  const catalog = useResource(loadCatalog);
  const [contentLoading, setContentLoading] =
    useState<ContentLoading>("ON_DEMAND");
  const loadingCapability = targetContentLoading(
    catalog.data,
    core,
    detail.runtimeConfig,
  );
  const latest = detail.saves.find((save) => save.restorable);
  const coreField = (
      <label className="field">
        运行核心
        <select value={core} onChange={(event) => setCore(event.target.value)}>
          {detail.coreIds.map((id) => (
            <option value={id} key={id}>
              {catalog.data?.cores.find((item) => item.id === id)?.name ?? id}
              {id === detail.defaultCoreId ? "（默认）" : ""}
            </option>
          ))}
        </select>
      </label>
  );
  const loadingField = (
    <ContentLoadingField
      capability={loadingCapability}
      value={contentLoading}
      onChange={setContentLoading}
    />
  );
  const start = (
    <LaunchButton
      gameId={detail.game.id}
      coreId={core}
      purpose={review ? "review" : "play"}
      variant={latest && !review ? "secondary" : "primary"}
      contentLoading={contentLoading}
    >
      {review ? "试玩" : "重新开始游戏"}
    </LaunchButton>
  );
  const resume =
    latest && !review ? (
      <LaunchButton
        gameId={detail.game.id}
        saveId={latest.id}
        coreId={latest.extinfo.coreId}
        contentLoading={contentLoading}
      >
        从存档继续
      </LaunchButton>
    ) : null;
  if (phone) {
    return (
      <>
        <div className="phone-detail-launch">
          <button
            className="phone-launch-options"
            onClick={() => setOptions(true)}
          >
            <AppIcon name="settings" />
            <span>启动选项</span>
          </button>
          {resume ?? start}
        </div>
        <ResponsiveSheet
          open={options}
          title="启动选项"
          placement="bottom"
          onClose={() => setOptions(false)}
        >
          <div className="phone-launch-fields">
            {coreField}
            {loadingField}
            {start}
          </div>
        </ResponsiveSheet>
      </>
    );
  }
  return (
    <DesktopDetailLaunch
      coreName={catalog.data?.cores.find((item) => item.id === core)?.name ?? core}
      coreField={coreField}
      loadingField={loadingField}
      resume={resume}
      start={start}
      review={review}
    />
  );
}

function DesktopDetailLaunch({
  coreName, coreField, loadingField, resume, start, review,
}: {
  coreName: string;
  coreField: ReactNode;
  loadingField: ReactNode;
  resume: ReactNode;
  start: ReactNode;
  review: boolean;
}) {
  const [options, setOptions] = useState(false);
  return (
    <div className="launch-panel">
      <div className="launch-options">
        <div className="launch-option-panel">{loadingField}</div>
      </div>
      <div className="launch-actions">
        {resume ?? (!review ? (
          <button className="button secondary" disabled>从存档继续</button>
        ) : null)}
        {start}
      </div>
      <p className="launch-hint">
        {resume ? "恢复最近存档，使用保存时的运行核心。" : "暂无可恢复的存档。"}
      </p>
      <div className="launch-runtime-row">
        <div className="launch-runtime-choice">
          <small>运行方式</small>
          <strong>{coreName || "—"}</strong>
          <button className="button secondary option-tab" onClick={() => setOptions(true)}>
            更换
          </button>
        </div>
      </div>
      <ResponsiveSheet open={options} title="运行核心" onClose={() => setOptions(false)}>
        <div className="stack">{coreField}</div>
      </ResponsiveSheet>
    </div>
  );
}

function ContentLoadingField({ capability, value, onChange }: {
  capability: ReturnType<typeof targetContentLoading>;
  value: ContentLoading;
  onChange: (value: ContentLoading) => void;
}) {
  return (
    <>
      <label className="field">
        内容加载
        <select
          aria-label="内容加载"
          value={capability === "PRELOAD_ONLY" ? "PRELOAD" : value}
          disabled={!capability || capability === "PRELOAD_ONLY"}
          onChange={(event) => onChange(event.target.value as ContentLoading)}
        >
          {capability ? (
            <>
              <option value="ON_DEMAND">按需加载</option>
              <option value="PRELOAD">下载完成后开始</option>
            </>
          ) : <option value="ON_DEMAND">—</option>}
        </select>
      </label>
      <p className="launch-hint">
        {capability === "PRELOAD_ONLY"
          ? "此运行方式仅支持完整下载游戏内容。"
          : capability
            ? "按需加载可边下载边开始游戏。"
            : "当前运行方式没有内容加载选项。"}
      </p>
    </>
  );
}
