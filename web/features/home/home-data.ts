import type { TagReference } from "@/components/tag-picker";
import type { HomePlatform } from "./home-rails";

export type RecentGame = {
  gameId: string;
  title: string;
  platform: { id: string; name: string };
  platformInstance: { id: string; name: string };
  lastPlayedAtMs: number;
  activeDurationMs: number;
  sessionCount: number;
  coverUrl: string | null;
  tags: TagReference[];
};

export type FeaturedGame = RecentGame & {
  hasSaveStates: boolean;
  lastSessionSave: null | {
    saveStateId: string;
    createdAtMs: number;
    activeDurationMs: number;
    screenshotUrl: string | null;
    discIndex: number | null;
    discLabel: string | null;
  };
};

export type LatestGame = {
  gameId: string;
  title: string;
  platform: { id: string; name: string };
  platformInstance: { id: string; name: string };
  createdAtMs: number;
  coverUrl: string | null;
  tags: TagReference[];
};

export type Home = {
  library: { gameCount: number; saveStateCount: number };
  play: { activeDurationMs: number };
  featuredGame: FeaturedGame | null;
  recentGames: RecentGame[];
  latestGames: LatestGame[];
  platforms: HomePlatform[];
  quickPlatforms: HomePlatform[];
};
