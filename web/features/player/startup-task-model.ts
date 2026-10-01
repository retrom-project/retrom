import type {RuntimeStartupKindV1, RuntimeStartupTaskV1} from "./runtime/contract";

export const startupLabels: Record<RuntimeStartupKindV1, [string, string]> = {
  LAUNCH_CONFIG: ["启动信息准备中", "启动信息已就绪"],
  PROVIDER_MODULE: ["运行模块准备中", "运行模块已就绪"],
  ENVIRONMENT: ["运行环境准备中", "运行环境已就绪"],
  BIOS: ["BIOS 准备中", "BIOS 已就绪"],
  GAME_CONTENT: ["游戏内容准备中", "游戏内容已就绪"],
  DEPENDENCIES: ["依赖资源准备中", "依赖资源已就绪"],
  CORE_ASSETS: ["核心资源准备中", "核心资源已就绪"],
  CORE_INITIALIZATION: ["核心初始化中", "核心初始化完成"],
  CONTENT_MOUNT: ["游戏内容装载中", "游戏内容装载完成"],
  RESTORE_LOAD: ["存档准备中", "存档已就绪"],
  RESTORE_APPLY: ["存档恢复中", "存档恢复完成"],
  GAME_START: ["游戏启动中", "游戏启动完成"],
  PLAYER_SETUP: ["画面与控制准备中", "画面与控制已就绪"],
};

/** Bounded visible history; hidden active tasks cannot reappear on progress updates. */
export class StartupTimeline {
  private rows: RuntimeStartupTaskV1[] = [];
  private readonly hiddenActive = new Map<string, RuntimeStartupTaskV1>();
  private readonly summaries = new Map<string, RuntimeStartupTaskV1>();
  private readonly seen = new Set<string>();
  message(): string {
    const active = this.snapshot().filter(task => task.state === "RUNNING").at(-1) ?? [...this.hiddenActive.values()].at(-1);
    return active ? startupLabels[active.kind][0] : "正在启动游戏…";
  }
  receive(task: RuntimeStartupTaskV1): RuntimeStartupTaskV1[] {
    if (!validTask(task)) {return this.snapshot();}
    if (this.hiddenActive.has(task.id)) {
      const current = this.hiddenActive.get(task.id)!;
      if (current.kind !== task.kind || !!current.summary !== !!task.summary) {return this.snapshot();}
      if (task.state !== "RUNNING") {this.hiddenActive.delete(task.id);}
      return this.snapshot();
    }
    const index = this.rows.findIndex(row => row.id === task.id);
    if (index >= 0) {
      const current = this.rows[index];
      if (current.kind !== task.kind || current.state !== "RUNNING" || !!current.summary !== !!task.summary) {return this.snapshot();}
      this.rows = this.rows.map((row, position) => position === index ? task : row);
    } else if (task.summary) {
      this.receiveSummary(task);
    } else if (task.state === "RUNNING") {
      if (this.seen.has(task.id)) {return this.snapshot();}
      this.seen.add(task.id);
      this.rows = [...this.rows, task];
    }
    return this.snapshot();
  }

  private receiveSummary(task: RuntimeStartupTaskV1): void {
    const current = this.summaries.get(task.id);
    if (current) {
      if (current.kind !== task.kind) {return;}
      if (task.state === "RUNNING") {this.summaries.set(task.id, task);}
      else {
        if (this.visibleSummary()?.id === task.id) {this.rows.push(task);}
        this.summaries.delete(task.id);
      }
    } else if (task.state === "RUNNING" && !this.seen.has(task.id)) {
      this.seen.add(task.id); this.summaries.set(task.id, task);
    }
  }

  private visibleSummary(): RuntimeStartupTaskV1 | undefined {
    return this.rows.some(task => task.state === "RUNNING") ? undefined : [...this.summaries.values()].at(-1);
  }

  private snapshot(): RuntimeStartupTaskV1[] {
    // Completion moves above active work; progress never changes the relative order of active steps.
    this.rows = [...this.rows.filter(task => task.state !== "RUNNING"), ...this.rows.filter(task => task.state === "RUNNING")];
    const summary = this.visibleSummary();
    while (this.rows.length > (summary ? 2 : 3)) {
      const removed = this.rows.shift()!;
      if (removed.state === "RUNNING") {this.hiddenActive.set(removed.id, removed);}
    }
    return summary ? [...this.rows, summary] : [...this.rows];
  }
}

export function startupPercentage(task: RuntimeStartupTaskV1): number | null {
  return task.progress ? Math.floor(task.progress.loadedBytes * 100 / task.progress.totalBytes) : null;
}

function validTask(task: RuntimeStartupTaskV1): boolean {
  if (!task || typeof task.id !== "string" || task.id.length < 1 || task.id.length > 128 ||
    !Object.hasOwn(startupLabels, task.kind) || !["RUNNING", "COMPLETED", "FAILED"].includes(task.state) ||
    task.summary !== undefined && typeof task.summary !== "boolean") {return false;}
  const progress = task.progress;
  return progress === null || !!progress && Number.isSafeInteger(progress.loadedBytes) && Number.isSafeInteger(progress.totalBytes) &&
    progress.totalBytes > 0 && progress.loadedBytes >= 0 && progress.loadedBytes <= progress.totalBytes;
}
