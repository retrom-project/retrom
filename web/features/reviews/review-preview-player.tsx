"use client";

import {PlayerShell} from "@/features/player/player-shell";

export function ReviewPreviewPlayer({previewId}: {previewId: string}) {
  return <PlayerShell launchId={previewId} />;
}
