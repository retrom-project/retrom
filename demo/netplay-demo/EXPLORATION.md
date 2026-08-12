# EmulatorJS 4.2.3 netplay exploration

本报告只描述 `assets-manifest.json` 固定的 runtime/core/content 组合，不把结果
外推到其他 EmulatorJS 版本、core、ROM 或浏览器。

## 结论矩阵

| 能力 | 结论 | 证据 |
| --- | --- | --- |
| 同页双实例 | PASS | 两个 same-origin iframe 各自创建 `EJS_emulator`，两个 core 均完成 3000 帧 smoke |
| 公共 input hook | PASS | 4.2.3 键盘、gamepad、virtual pad 源码均汇入 `gameManager.simulateInput`；真实 debug input 的 4 次边沿被 relay 记录 |
| raw input injection | PASS | 两端使用 `gameManager.functions.simulateInput` 应用 P1/P2 canonical snapshot，3000 帧无 desync |
| frame hook | PASS | runtime factory 初始化时 chain `postMainLoop`；每个 net frame 连续且目标帧精确停住 |
| pause/resume | PASS | `toggleMainLoop(0/1)` 支撑等待 input/hash 和手动暂停；最长测试只有一次可恢复 stall |
| state capture | PASS | `getState()` 可重复序列化完整 RASTATE；FCEUmm 112,464 B，FBNeo 78,976 B |
| state transfer | UNKNOWN | 4.2.3 `loadState()` 异步排队且无完成回调；两端冷启动摘要已相同，所以正确跳过了传输分支 |
| deterministic execution | PASS（限定组合） | 两套 core/content 各 3000 帧、25 个完整 state checkpoint、0 desync |
| WebSocket lockstep | PASS | 两个独立 WS 连接进入同一 room，服务端按 slot 生成 canonical frame 并比较摘要 |
| rollback/replay | FAIL（能力缺失） | 4.2.3 没有精确执行 N 帧、等待 state load、replay 静音等公开原语 |

## 源码与运行时事实

EmulatorJS 4.2.3 的 `GameManager` 已公开 `getState`、`loadState`、
`simulateInput`、`toggleMainLoop` 和 `getFrameNum`。键盘、物理 gamepad、虚拟
摇杆/按键最终都调用公共 `simulateInput`；现有实验性 netplay 自身也使用底层
`functions.simulateInput`。对应上游源码：

- [`GameManager.js` v4.2.3](https://github.com/EmulatorJS/EmulatorJS/blob/v4.2.3/data/src/GameManager.js)
- [`emulator.js` v4.2.3](https://github.com/EmulatorJS/EmulatorJS/blob/v4.2.3/data/src/emulator.js)

本地对 EmulatorJS RetroArch fork 的源码检查显示，导出的 `load_state` 只调用
`content_load_state(...)` 后立即返回；代码库虽有
`content_wait_for_load_state_task()`，但 4.2.3 前端没有可调用的导出。浏览器实验
也观察到延迟加载会在调用返回后改变 frame/state。这个源码关联只用于解释观察
到的行为，并不声称 manifest 中的 core artifact 可被精确追溯到该 commit。

`getState()` 返回以 `RASTATE1` 开头的容器；两端摘要不同时，差异落在 `MEM `
core state block，而非截图或前端 metadata。因此 checkpoint 比较的是实际模拟
状态，而不是画面相似度。

## 4.2.3 必要补丁

本 POC 必须在运行时 factory 初始化前注入 `postMainLoop` callback。官方
4.2.3 提供的方法足以完成冷启动 fixed-delay lockstep，其 runtime/core bytes
无需重编译。

要扩展到 RetroArch 风格的生产 netplay，建议给 EmulatorJS/其 RetroArch fork
增加稳定且版本化的 adapter 能力：

1. `loadStateAndWait(bytes)`：加载完成或失败后才 resolve，并返回加载后的 frame；
2. `runExactFrames(count, {audio, video})`：暂停状态下精确 replay N 帧；
3. replay 输出控制：追帧期间禁止中间视频提交并抑制/丢弃旧音频；
4. state compatibility fingerprint：显式包含 runtime、core、content、BIOS、
   core options 和确定性相关设置；
5. resync/rollback 期间的原子 pause/load/input-history/replay 边界。

固定延迟模式可先作为 core allowlist 的实验能力；不应把当前结果直接描述为
“兼容 RetroArch netplay”。RetroArch 官方设计包含 deterministic input-frame
sync、序列化 state 与 replay/rollback，且要求参与者的 core、content 和设置
一致；本 demo 只完成了其中的确定性与 fixed-delay 基线。

## 未覆盖与风险

- NES 验证内容是通过 FCEUmm 运行的 FDS `Smash Ping Pong`，需要
  `disksys.rom`；它证明 NES core 的可行性，不代表普通 `.nes` 内容全集。
- 探索中同一 4.2.3 runtime 下，尝试的普通 `.nes` 内容在 FCEUmm/Nestopia
  `getState()` 返回 size zero，未进入正式夹具；原因仍需 core/runtime 专项调查。
- FBNeo 仅验证 `ldrun.zip` 精确 romset；不同 arcade driver 的确定性不能推断。
- audio 被设置为 0，避免双实例重叠；没有验证 replay 音频策略。
- 两个客户端目前同 tab、同进程；WebSocket 路径真实，但未覆盖跨设备时钟、
  background throttling、丢包、断网恢复与公网安全部署。
- demo server 只用于 localhost 验证，具备 same-origin handshake、room/slot、
  future-window、immutable frame 和 payload 上限；它不是 Retrom 生产后端。
