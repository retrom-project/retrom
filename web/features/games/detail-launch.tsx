"use client";
import { useState } from "react";
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
  error,
}: {
  detail: Schema<"GameDetail">;
  review: boolean;
  error: string;
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
  const choose = (
    <>
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
      {loadingCapability ? (
        <label className="field">
          内容加载
          <select
            aria-label="内容加载"
            value={
              loadingCapability === "PRELOAD_ONLY" ? "PRELOAD" : contentLoading
            }
            disabled={loadingCapability === "PRELOAD_ONLY"}
            onChange={(event) =>
              setContentLoading(event.target.value as ContentLoading)
            }
          >
            <option value="ON_DEMAND">按需加载</option>
            <option value="PRELOAD">提前加载全部内容</option>
          </select>
        </label>
      ) : null}
    </>
  );
  const start = (
    <LaunchButton
      gameId={detail.game.id}
      coreId={core}
      purpose={review ? "review" : "play"}
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
            {choose}
            {start}
            {error ? <p role="alert">{error}</p> : null}
          </div>
        </ResponsiveSheet>
      </>
    );
  }
  return (
    <div className="launch-panel">
      <div className="launch-options">
        <div className="launch-option-panel">{choose}</div>
      </div>
      <div className="launch-actions">
        {resume}
        {start}
      </div>
      {error ? (
        <p className="launch-hint" role="alert">
          {error}
        </p>
      ) : null}
    </div>
  );
}
