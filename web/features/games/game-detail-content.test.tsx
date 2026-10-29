import type { ReactNode } from "react";
import { cleanup, fireEvent, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { render } from "@/components/toast-test-utils";
import type { Schema } from "@/lib/api/types";
import type { ContentLoading } from "@/features/player/content-loading";
import { GameDetailContent } from "./game-detail-content";

vi.mock("@/lib/use-resource", () => ({useResource: () => ({data: {cores: []}})}));
vi.mock("@/lib/use-phone-layout", () => ({usePhoneLayout: () => false}));
vi.mock("./detail-title", () => ({DetailTitle: ({title}: {title: string}) => <h1>{title}</h1>}));
vi.mock("@/features/player/content-loading", () => ({targetContentLoading: () => "ON_DEMAND_AND_PRELOAD"}));
vi.mock("@/features/player/launch-button", () => ({
  LaunchButton: ({children, contentLoading = "ON_DEMAND"}: {children: ReactNode; contentLoading?: ContentLoading}) =>
    <button data-content-loading={contentLoading}>{children}</button>,
}));
afterEach(cleanup);

const game: Schema<"Game"> = {
  id: "game", platformInstanceId: "directory", directoryName: "PSP", platformId: "psp",
  title: "测试游戏", titleLetter: "#", sortName: "", description: "", developer: "", publisher: "", genre: "", players: null,
  releaseYear: null, status: "published", source: "server_import", version: 1,
  contentHash: "a".repeat(64), tags: [], media: [], favorite: false, lastPlayedAtMs: null,
  createdAtMs: 0, updatedAtMs: 0,
};
const save: Schema<"Save"> = {
  id: "save", name: "进度", kind: "game_save", slot: null, version: 1,
  sizeBytes: 1024, createdAtMs: 0, updatedAtMs: 0, screenshotUrl: null,
  restorable: true, restoreReason: "", restoreWarning: "", game,
  extinfo: {coreId: "ppsspp", providerId: "retrom-runtime", targetId: "ppsspp",
    coreFingerprint: "b".repeat(64), romHash: game.contentHash, checkpointFormat: "ppsspp-game-save-v1-storage-v1",
    content: {kind: "SINGLE_FILE", entryFile: "game.iso"}, runtimeOptions: {}},
};
const detail: Schema<"GameDetail"> = {
  game, files: [], coreIds: ["ppsspp"], defaultCoreId: "ppsspp", saves: [save], favoriteFolderIds: [],
  runtimeConfig: {content: {kind: "SINGLE_FILE", entryFile: "game.iso"}},
};

it("uses the selected loading mode for every game-detail launch, including individual saved progress", () => {
  render(<GameDetailContent detail={detail} mode="user" busy={false}
    onFavorite={vi.fn()} onReview={vi.fn()} onChange={vi.fn()} />);
  const loading = screen.getByRole("combobox", {name: "内容加载"});
  for (const mode of ["PRELOAD", "ON_DEMAND"] as const) {
    fireEvent.change(loading, {target: {value: mode}});
    for (const name of ["重新开始游戏", "从存档继续", "从这里继续"]) {
      expect(screen.getByRole("button", {name})).toHaveAttribute("data-content-loading", mode);
    }
  }
});
