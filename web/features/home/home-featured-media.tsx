import Image from "next/image";
import { useState } from "react";
import type { Game, Save } from "@/lib/api/types";
import { HomePlatformArt } from "./home-platform-art";

export function HomeFeaturedMedia({ game, save }: { game: Game; save?: Save }) {
  const [failed, setFailed] = useState<string[]>([]);
  const candidates = [
    ...(save?.screenshotUrl ? [{ kind: "save", url: save.screenshotUrl }] : []),
    ...[...game.media].filter(item => item.kind === "screenshot").sort((a, b) => a.ordinal - b.ordinal),
    ...game.media.filter(item => item.kind === "cover"),
  ];
  const media = candidates.find(item => !failed.includes(item.url));
  function fail() {
    if (media) { setFailed(previous => [...previous, media.url]); }
  }
  return (
    <div className="home-featured-media" data-kind={media?.kind ?? "platform"}>
      {media?.kind === "cover" ? <>
        <Image className="home-featured-backdrop" src={media.url} fill unoptimized alt="" sizes="60vw" />
        <div className="home-featured-cover">
          <Image className="home-featured-art" src={media.url} fill unoptimized alt={`${game.title}的封面`} sizes="30vw" onError={fail} />
        </div>
      </> : media ? <>
        <Image className="home-featured-art" src={media.url} fill unoptimized alt={`${game.title}的${media.kind === "save" ? "存档画面" : "游戏截图"}`} sizes="60vw" onError={fail} />
        {media.kind === "save" ? <span className="home-scene-caption">存档画面</span> : null}
      </> : <div className="home-featured-platform-art" aria-hidden="true"><HomePlatformArt platformId={game.platformId} /></div>}
    </div>
  );
}
