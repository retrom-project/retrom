import type { RuntimeStartupKindV1 } from "./runtime/contract";
export function startupLabel(kind: RuntimeStartupKindV1) {
  return labels[kind];
}
const labels: Record<RuntimeStartupKindV1, string> = {
  LAUNCH_CONFIG: "正在准备启动配置…",
  PROVIDER_MODULE: "正在加载运行模块…",
  ENVIRONMENT: "正在检查运行环境…",
  BIOS: "正在加载 BIOS…",
  GAME_CONTENT: "正在加载游戏内容…",
  DEPENDENCIES: "正在加载游戏依赖…",
  CORE_ASSETS: "正在加载运行核心…",
  CORE_INITIALIZATION: "正在初始化运行核心…",
  CONTENT_MOUNT: "正在准备游戏资源…",
  RESTORE_LOAD: "正在读取存档…",
  RESTORE_APPLY: "正在恢复游戏进度…",
  GAME_START: "正在开始游戏…",
  PLAYER_SETUP: "正在准备播放器…",
};
