"use client";
import { useState } from "react";
import { HomeFeatured } from "./home-sections";
import { HomeDiscover } from "./home-discover";
import type { HomeData } from "./home-data";
import { AppIcon } from "@/components/app-icon";
export function PhoneHome({ data }: { data: HomeData }) {
  const [query, setQuery] = useState("");
  return (
    <div className="phone-home">
      <form className="phone-home-search" action="/library">
        <AppIcon name="search" />
        <input
          name="q"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder="想玩什么？搜一下"
          aria-label="搜索游戏"
        />
        <button>搜索</button>
      </form>
      <section className="phone-continue">
        <h1>今天，玩点什么？</h1>
        <div className="phone-continue-card">
          <HomeFeatured data={data} />
        </div>
      </section>
      <HomeDiscover phone />
    </div>
  );
}
