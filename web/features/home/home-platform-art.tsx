"use client";
import Image from "next/image";
import { useState } from "react";
import { AppIcon } from "@/components/app-icon";
import { platformArt } from "./platform-art";

export function HomePlatformArt({ platformId }: { platformId: string }) {
  const [failed, setFailed] = useState(false);
  const src = platformArt(platformId);
  if (!src || failed) {
    return <AppIcon name="gamepad" className="home-platform-art" />;
  }
  return <Image className="home-platform-art" src={src} width={64} height={52}
    unoptimized alt="" onError={() => setFailed(true)} />;
}
