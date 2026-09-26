"use client";

import Image from "next/image";
import { useState, useSyncExternalStore } from "react";
import { platformArt } from "./platform-art";

export const homeMediaQuery = "(min-width: 1280px)";

function subscribe(onChange: () => void) {
  if (typeof window.matchMedia !== "function") {return () => {};}
  const query = window.matchMedia(homeMediaQuery);
  query.addEventListener("change", onChange);
  return () => query.removeEventListener("change", onChange);
}

type Props = { screenshotUrl: string | null; coverUrl: string | null; platformId: string; title: string };

// SSR and phones never mount media, so hiding the desktop layout does not download hero images.
export function HomeFeaturedMedia(props: Props) {
  const desktop = useSyncExternalStore(subscribe, () => typeof window.matchMedia === "function" && window.matchMedia(homeMediaQuery).matches, () => false);
  return desktop ? <FeaturedMedia key={`${props.screenshotUrl}:${props.coverUrl}:${props.platformId}`} {...props} /> : null;
}

function FeaturedMedia({ screenshotUrl, coverUrl, platformId, title }: Props) {
  const [rejected, setRejected] = useState<string[]>([]);
  const [loaded, setLoaded] = useState<string | null>(null);
  const artwork = platformArt(platformId);
  const source = [screenshotUrl, coverUrl, artwork].find((url): url is string => Boolean(url && !rejected.includes(url)));
  const isPlatform = source === artwork;
  const isSave = source === screenshotUrl;
  const ready = source === loaded;

  function reject(url: string) {
    setRejected((current) => current.includes(url) ? current : [...current, url]);
  }

  return <div className="home-featured-media" data-kind={!source ? "empty" : isPlatform ? "platform" : "landscape"}>
    {source ? <Image key={source} className={`home-featured-art${ready ? " is-ready" : ""}`} src={source}
      alt={isPlatform ? "" : `${title}${isSave ? " 上次存档截图" : " 游戏图片"}`} fill
      sizes="(min-width: 1800px) 1100px, 65vw" unoptimized
      onLoad={(event) => {
        const image = event.currentTarget;
        if (!isPlatform && image.naturalWidth <= image.naturalHeight) {reject(source);}
        else {setLoaded(source);}
      }} onError={() => reject(source)} /> : null}
    {ready && isSave ? <span className="home-scene-caption">上次存档画面</span> : null}
  </div>;
}
