"use client";
import Link from "next/link";
import { usePhoneLayout } from "@/lib/use-phone-layout";
import { PhoneHome } from "./phone-home";
import { api, result } from "@/lib/api/client";
import { useResource } from "@/lib/use-resource";
import { PageHeader } from "@/components/ui";
import { ResourceState } from "@/components/resource-state";
import { AppIcon } from "@/components/app-icon";
import {
  HomeFeatured,
  HomeFavorites,
  HomeRecent,
  HomeDirectories,
  featuredGameId,
} from "./home-sections";
async function loadHome() {
  return result(await api.GET("/api/v1/home"));
}
export function HomePage() {
  const home = useResource(loadHome);
  const phone = usePhoneLayout();
  if (phone) {
    return (
      <ResourceState resource={home}>
        {(data) => <PhoneHome data={data} />}
      </ResourceState>
    );
  }
  return (
    <div className="home-page">
      <PageHeader
        title="今天，玩点什么？"
        description="继续上次的冒险，或挑选你的下一款游戏。"
        actions={
          <>
            <form className="home-search" action="/library">
              <AppIcon name="search" />
              <input name="q" placeholder="搜索游戏…" aria-label="搜索游戏" />
            </form>
            <Link
              className="button secondary home-immersive-entry"
              href="/immersive"
            >
              <AppIcon name="expand" />
              沉浸模式
            </Link>
          </>
        }
      />
      <ResourceState resource={home}>
        {(data) => (
          <>
            <section className="home-layer">
              <HomeFeatured data={data} />
            </section>
            <HomeRecent
              recent={data.recent.filter(
                (item) => item.game.id !== featuredGameId(data),
              )}
            />
            <HomeFavorites games={data.favorites} />
            <HomeDirectories directories={data.directories} />
            <section
              className="home-layer home-summary"
              aria-label="资料库概况"
            >
              <span>
                资料库 <strong>{data.summary.gameCount}</strong> 款游戏
              </span>
              <span>
                <strong>{data.summary.saveCount}</strong> 份存档
              </span>
              <Link href="/library">浏览游戏库</Link>
            </section>
          </>
        )}
      </ResourceState>
    </div>
  );
}
