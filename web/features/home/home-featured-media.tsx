import Image from "next/image";
import { useState } from "react";
import type { Game, Save } from "@/lib/api/types";
import { HomePlatformArt } from "./home-platform-art";

export function HomeFeaturedMedia({ game, save }: { game: Game; save?: Save }) {
  const [failedURL, setFailedURL] = useState<string>();
  const screenshot = save?.screenshotUrl;
  return (
    <div className="home-featured-media" data-kind={save ? "save" : "platform"}>
      {save ? screenshot && screenshot !== failedURL ? (
        <Image className="home-featured-art is-ready" src={screenshot} fill unoptimized
          alt={`${game.title}的存档画面`} sizes="60vw" onError={() => setFailedURL(screenshot)} />
      ) : <span className="home-scene-caption">存档截图暂不可用</span>
        : <div className="home-featured-platform-art" aria-hidden="true"><HomePlatformArt platformId={game.platformId} /></div>}
    </div>
  );
}
