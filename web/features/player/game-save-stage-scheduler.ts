/** Coalesce native writes without letting continuous activity postpone a draft indefinitely. */
export class GameSaveStageScheduler {
  private timer: ReturnType<typeof setTimeout> | null = null;
  private startedAt = 0;
  private revision: string | undefined;

  constructor(private readonly stage: () => void) {}

  schedule(revision: string) {
    if (this.timer !== null && revision === this.revision) {return;}
    if (this.timer === null) {this.startedAt = Date.now();}
    else {clearTimeout(this.timer);}
    this.revision = revision;
    this.timer = setTimeout(() => {this.cancel(); this.stage();},
      Math.max(0, Math.min(1_000, this.startedAt + 5_000 - Date.now())));
  }

  cancel() {
    if (this.timer !== null) {clearTimeout(this.timer);}
    this.timer = null;
    this.revision = undefined;
  }
}

export async function waitForGameSaveDraft(pending: Promise<boolean>, deadline: number) {
  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    return await Promise.race([pending, new Promise<never>((_, reject) => {
      timer = setTimeout(() => reject(Error("GAME_DATA_BUSY")), Math.max(0, deadline - Date.now()));
    })]);
  } finally {if (timer !== undefined) {clearTimeout(timer);}}
}
