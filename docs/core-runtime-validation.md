# Provider Target 产品链路验证基线

| 属性 | 内容 |
| --- | --- |
| 文档状态 | 已实施 / 一期验收基线 |
| 版本 | 2.0 |
| 日期 | 2026-09-03 |

## 1. 证据边界

只有经过 Retrom Upload/Import/Review/Publish、创建 Launch、读取 Launch Envelope、由共享 dispatcher mount Provider、加载受限内容并在真实 Chrome 中产生帧和输入结果，才算产品链路证据。独立引擎页面、解析器、HTTP 200、静止 canvas、结构测试或 Provider 自测都不能替代产品证据。

每次执行证据必须记录精确 `providerId/bundleSha256/targetId`、当前内容 manifest、依赖 snapshot 与测试 fixture digest。Provider Target 之外不得再记录或选择内部路由、宿主适配器或旧式运行构件身份。

## 2. 已覆盖产品 Target

| Target/能力 | 入口 | 确定性输入 | 已证明边界 |
| --- | --- | --- | --- |
| `mgba` | `make web-e2e`、`ACC-RUN-002`、`ACC-PEG-006`、`ACC-IMM-004/005/010` | `testdata/public-roms/gba-smoke/` | 普通与服务器导入、审核发布、真实帧、手柄、显式 checkpoint 与跨 Launch 恢复 |
| `fceumm`、`nestopia` | `ACC-RUN-009`、`ACC-NP-014/016/018` | `testdata/public-roms/nes-smoke/` | 单机恢复、双浏览器输入、state transfer、rollback/lockstep、断线重连 |
| `snes9x` | `ACC-RUN-008`、`ACC-NP-017` | `testdata/public-roms/snes-smoke/` | 单机恢复、checkpoint 收敛、冻结和重连 |
| `mame2003`、`mame2003_plus`、`fbneo`、`fbalpha2012_cps1/cps2` | `ACC-RUN-006/007/010/011/012`、`ACC-NP-015/019/020/021/022`、`ACC-IMM-006` | `testdata/public-roms/arcade-smoke/` | DAT parent/BIOS 闭包、真实画面/输入、checkpoint、双浏览器与沉浸多手柄 |
| Saturn 多盘 | `ACC-MDISC-001..008` | 确定性临时 fixture | 导入、盘序、换盘事件和 checkpoint；不代表真实商业 ROM 兼容 |
| RPG Maker 2000/2003/XP/VX/VX Ace/MV | `ACC-RPG-002..007` | `testdata/public-roms/rpgmaker-smoke/` | 单一虚拟 Core 选择 retrom-runtime Target，真实地图/输入/音频、A→B→C、跨 Launch 恢复 B |
| RPG Maker MZ | `ACC-RPG-008` | 操作者合法输入 | 与 MV 相同的 unique-origin、场景、帧、输入和恢复；缺输入时 BLOCKED |
| ONS、KiriKiri、Butterscotch、TyranoScript | 各自 `ACC-*-001` | 操作者合法输入 | Review Preview、Product、按需内容、checkpoint 和跨 Launch 恢复；结论只覆盖当次样本 |
| WASM-4 | PFB loose开发层产品Case | 锁定上游合法 cart | cart 校验、画面、输入、checkpoint、跨 cart 拒绝和清理 |

未列 Target 只有 schema、构建、依赖或相邻逻辑测试，不得声明浏览器产品兼容。没有合法可再分发输入时必须明确 `BLOCKED`，不能下载不明第三方内容补齐。

## 3. 共享验证规则

- Go 和 TypeScript 必须对同一 Launch Envelope fixtures 得出相同接受/拒绝结果。
- Provider Module 的 URL、SHA-256、Provider 身份、API 版本与 Bundle 必须一致。
- `runtime.capabilities` 必须与返回的 `PlayerRuntimeV1` 行为闭合；声明支持却缺方法、未声明却暴露行为均失败。
- checkpoint 只按 format/size/hash 处理，Host 不解析字节；恢复必须由 Target `readFormats` 明确允许。
- content、BIOS、parent、多盘、pack、unique-origin 与 netplay 资源必须全部来自 envelope grant。
- 所有测试必须证明退出/失败/卸载会清理 Provider、撤销 Launch 并停止输入与帧回调。

## 4. EmulatorJS 特殊边界

EmulatorJS Provider declaration 是 35 个 Target 的唯一行为 registry。`mame2003` 的 4.2.1 core 覆盖、DOSBox Pure 的 state 修复、线程 core、shader、启动动作、多盘和八个 netplay profile 都封装在该 Provider 中。Retrom 只看 Target declaration 与标准能力，不按 core 名在 Go 或前端复制规则。

指定存档不能在首帧盲目自动加载；Provider 必须等待目标核心可序列化，再执行原生 load 并以明确失败 fail closed。普通开始必须清理浏览器遗留的隐式目录存档，只有用户点击“创建存档”才上传显式 checkpoint。

## 5. retrom-runtime 特殊边界

RPG 世代检测只选择 `retrom-runtime` Provider 内的 Target；用户仍只看到一个 RPG Maker Core。EasyRPG、mkxp、Native Web、ONS、KiriKiri、Butterscotch、TyranoScript 和 WASM-4 的文件策略、bridge、OPFS/Range、输入和 checkpoint codec 都属于 Provider 私有实现。

Native Web 必须使用每 Launch unique origin，拒绝应用 cookie、普通 API、跨 Launch 项目和 ticket 重放。RPG Runtime Validation 复用相同 Provider Module，14 个 gate 证明帧、输入、音频、A/B/C、checkpoint、不同 restore Launch、截图和恢复后输入。

## 6. 升级验证

Provider 升级必须在同一数据库上顺序启动旧版与更高版本，证明：

- active Bundle 只向前移动；
- 同版本不同 bytes、降级和 Target 删除均拒绝 readiness；
- 稳定 `providerId/targetId` 保持一致；
- 兼容升级的 `readFormats` 包含旧 checkpoint format，旧 Save 可由新 Bundle 恢复；
- 不兼容格式仍保留 Save 记录但创建恢复 Launch 返回 `LAUNCH_SAVE_INCOMPATIBLE`；
- 新普通 Launch 和新 Save 都绑定新 Bundle/Target declaration；
- 不再从旧 Bundle 静态端点或旧模块 fallback。

PFB只能证明当前worktree、基座Provider与loose revision组合的产品行为；正式Release授权后必须使用production lock重跑相同Case，才可成为发布证据。

## 7. 必跑门禁

共享运行层改变时至少运行 `ACC-PROVIDER-001..008`、`make web-e2e`、全部已有受影响产品 Case、Provider 仓库全量 lint/typecheck/test/build/package 检查，以及 Retrom 的 API、Go、Web、集成、数据和镜像/PFB 验证。真实硬件兼容结论仍需 Chrome `mapping=standard` 的实体手柄 smoke；自动注入不能替代硬件验收。
