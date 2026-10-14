import type { MetadataRoute } from "next";

export default function manifest(): MetadataRoute.Manifest {
  return {
    id: "/",
    name: "Retrom",
    short_name: "Retrom",
    description: "自己的游戏库，随时继续游玩。",
    lang: "zh-CN",
    start_url: "/",
    scope: "/",
    display: "standalone",
    background_color: "#101521",
    theme_color: "#101521",
    icons: [
      { src: "/pwa/icons/icon-192.png", sizes: "192x192", type: "image/png", purpose: "any" },
      { src: "/pwa/icons/icon-512.png", sizes: "512x512", type: "image/png", purpose: "any" },
      { src: "/pwa/icons/maskable-512.png", sizes: "512x512", type: "image/png", purpose: "maskable" },
    ],
  };
}
