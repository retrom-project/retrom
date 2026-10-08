import Image from "next/image";
import Link from "next/link";
import type { Save } from "@/lib/api/types";
import { BrowserTime } from "@/components/browser-time";
import { LaunchButton } from "@/features/player/launch-button";
export function SaveFeatured({ save }: { save: Save | undefined }) {
  return (
    <section
      className="save-latest-section"
      aria-labelledby="save-latest-heading"
    >
      <div className="save-section-label">
        <h2 id="save-latest-heading">最近保存</h2>
        <p>最近保存的一份可恢复存档</p>
      </div>
      {save ? (
        <div className="save-latest-card">
          <div className="save-latest-shot">
            {save.screenshotUrl ? (
              <Image
                src={save.screenshotUrl}
                alt={`${save.game.title}最近存档画面`}
                fill
                unoptimized
                sizes="360px"
              />
            ) : (
              <span className="save-screenshot-placeholder">暂无截图</span>
            )}
          </div>
          <div className="save-latest-copy">
            <div className="save-latest-kicker">
              <i />
              最近保存
            </div>
            <Link href={`/games/${save.game.id}`}>
              <h3>{save.game.title}</h3>
            </Link>
            <p>
              {save.game.directoryName} · {save.extinfo.coreId}
            </p>
            <div className="save-latest-facts">
              <div>
                <span>保存时间</span>
                <strong>
                  <BrowserTime value={save.updatedAtMs} />
                </strong>
              </div>
              <div>
                <span>存档类型</span>
                <strong>
                  {save.kind === "game_save" ? "游戏内存档" : "即时存档"}
                </strong>
              </div>
              <div>
                <span>存档状态</span>
                <strong>可以继续</strong>
              </div>
            </div>
          </div>
          <div className="save-latest-actions">
            <LaunchButton
              gameId={save.game.id}
              coreId={save.extinfo.coreId}
              saveId={save.id}
              returnTo="/saves"
            >
              从这里继续
            </LaunchButton>
            <Link className="button secondary" href={`/games/${save.game.id}`}>
              查看游戏详情
            </Link>
            <small>
              {save.kind === "game_save"
                ? "恢复后使用游戏内读档菜单"
                : "直接恢复这份即时存档"}
            </small>
          </div>
        </div>
      ) : (
        <div className="save-latest-unavailable">
          当前没有可以恢复的存档，请在下方查看原因。
        </div>
      )}
    </section>
  );
}
