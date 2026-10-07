import Image from "next/image";
import Link from "next/link";
import type { Schema } from "@/lib/api/types";
import { BrowserTime } from "@/components/browser-time";
import { LaunchButton } from "@/features/player/launch-button";
import { AppIcon } from "@/components/app-icon";
export function RecentCard({ item }: { item: Schema<"RecentGame"> }) {
  const cover = item.game.media.find((media) => media.kind === "cover");
  const href = `/games/${item.game.id}`;
  return (
    <article className="recent-history-row">
      <Link className="recent-history-cover" href={href}>
        {cover ? (
          <Image src={cover.url} fill unoptimized sizes="174px" alt="" />
        ) : (
          <span>{item.game.title}</span>
        )}
      </Link>
      <div className="recent-history-content">
        <div className="recent-history-main">
          <Link href={href}>
            <h2>{item.game.title}</h2>
          </Link>
          <p>
            <AppIcon name="gamepad" />
            {item.game.directoryName}
          </p>
        </div>
        <div className="recent-history-facts">
          <div className="recent-history-fact">
            <span>最后游玩</span>
            <strong>
              <BrowserTime value={item.lastPlayedAtMs} />
            </strong>
          </div>
        </div>
      </div>
      <div className="recent-history-actions">
        <div className="recent-history-launch">
          <LaunchButton gameId={item.game.id} />
        </div>
        <Link className="recent-history-detail" href={href}>
          游戏详情<span>›</span>
        </Link>
      </div>
    </article>
  );
}
