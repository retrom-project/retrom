# EmulatorJS 4.2.3 rollback netplay exploration

本文只描述 `assets-manifest.json` 固定的 runtime/core/content 组合，不把结果外推
到其他 EmulatorJS 版本、core、ROM 或浏览器。协议设计参考 RetroArch 官方的
[netplay developer notes](https://docs.libretro.com/development/retroarch/netplay/)
与 [Netplay FAQ](https://docs.libretro.com/guides/netplay-faq/)，但没有实现其 wire
protocol。

## 结论矩阵

| 能力 | 结论 | 当前证据 |
| --- | --- | --- |
| 同页双实例 | PASS | 两个 same-origin iframe 各自创建真实 `EJS_emulator` |
| profile handshake | PASS | room 固定 runtime/adapter/core/content/BIOS/settings SHA-256，错误 profile 拒绝加入 |
| input capture/injection | PASS | 公共 `simulateInput` 捕获本地输入，底层函数注入 canonical P1/P2 snapshot |
| frame boundary/step | PASS（adapter） | factory `postMainLoop` hook + `runNetplayFrame()` 精确停一个 raw frame |
| bounded prediction | PASS | 缺失远端输入沿用最后确认值，默认最多八帧，超过窗口暂停 |
| rollback/replay | PASS（限定组合） | 100 ms RTT 下两个真实 core 均产生 rollback，并在最终 checkpoint 收敛 |
| replay 输出抑制 | PASS | 每次 replay 都记录 mute run；中间 canvas 由冻结帧覆盖，结束后恢复音量/画面 |
| waitable state load | PASS（adapter） | 从真实不同 state 加载；原生 callback 完成、完整 state byte-exact 后才 resolve |
| state resync | PASS（受限） | mismatch 后经 WS 转发 authority RASTATE、校验 payload/core digest 并确认后恢复 |
| WebSocket reconnect | PASS | slot 租约、opaque resume token、指数退避、canonical/hash replay 后完成 session |
| room service | PASS（单进程参考） | 同源建房、双 slot token、TTL、容量、profile、payload/origin 校验 |
| deterministic execution | PASS（allowlist） | FCEUmm/F-1 Race 与 FBNeo/Lode Runner 各 3000 帧；除注入故障外无额外 resync |
| RetroArch interoperability | NOT IMPLEMENTED | 模拟策略相似，但消息与 RetroArch netplay protocol 不兼容 |

## prediction 与 rollback

客户端使用独立逻辑 `netFrame`，不把每台机器的 RAF/frame counter 当网络时钟。
每个 frame-before 保存完整 state，同时提交本地 24-value controller snapshot。
canonical input 尚未回来时，本地输入使用当前 intent，远端使用最后连续确认值。

收到迟到 canonical 后，如果它与已经执行的预测不同：

1. 在帧边界暂停；
2. 加载 ring 中最早受影响帧的 frame-before state；
3. 对该帧到当前位置逐帧重放 canonical/历史预测；
4. 每个 replay frame 通过 adapter 的 exact-step 执行；
5. 全程强制音量为零、fast-forward，并隐藏中间 canvas；
6. 追平后恢复用户音量与最后画面。

ring 与最大 rollback 都限制为 120 帧，预测窗口默认八帧。checkpoint 必须等待该帧
canonical 连续确认、pending rollback 清空后才能计算，因此 hash 不会观察半完成
replay。

## 可靠 state load

官方 4.2.3 `GameManager.loadState()` 把文件写入 MEMFS，调用导出的 `load_state` 后
立刻返回。对应 RetroArch fork 的 C 代码只调用 `content_load_state(...)`；读取与
deserialize 位于 blocking task queue。源码虽然有
`content_wait_for_load_state_task()`，4.2.3 artifact 没有把它导出给 JS。

本 demo 没有修改官方 core bytes，而是在固定版本 adapter 中：

- 包装 runtime module 的 `print`/`printErr`，只观察 `game.state` 的原生 load
  callback；
- 暂停时临时恢复 main loop，让 task queue 前进；
- 原生同步 callback 完全返回后的 microtask 立即重新暂停；
- 再次 capture state，记录是否与请求 bytes 完全一致。

这消除了固定 sleep 和“冷启动 state 本来相同所以没走加载分支”的假阳性。每个
smoke 启动时都会先让接收端执行不同输入，再传输 authority state；结果必须包含
`changed=true`、`nativeCompletion=true`、`byteExact=true`。

长期更好的上游 patch 仍是把内存 state-load/deserialize 与明确的成功/失败回调
作为稳定导出，而不是依赖 4.2.3 的 runtime log contract。本 adapter 因此严格把
版本写入 profile，不允许未知版本回退。

## resync hash 边界

`getState()` 返回 RASTATE1 容器。checkpoint 只 hash `MEM ` block 中的 libretro
core payload；完整 RASTATE 可能包含不应参与 deterministic comparison 的前端
metadata。state 传输仍对完整 payload 做 SHA-256，服务端重新计算后才转发；同时
携带 core digest，用于判断接收端是否确实需要 native load。

测试在接收端暂停后额外执行一个不受 netplay 跟踪的真实 core frame，再安排双方
checkpoint。两端因此生成不同 core hash，authority state 必须经 WebSocket 传输，
接收端执行 `changed=true`、`nativeCompletion=true`、`byteExact=true` 的原生加载后
才能继续。建房同步还会从另一组真实不同 state 单独验证同一接口。

## room 与 reconnect

建房接口发放两个不可猜测 join token，WebSocket 还要求相同 profile digest。
服务端不根据裸 `slot=0/1` 信任身份。首次 `hello` 返回 resume token；意外断线后
room 保留 slot 十秒，客户端指数退避重连并声明最后收到的 canonical/hash frame。
服务端从 600 帧有界历史重放缺失消息后再 resume。

同源检查、2 MiB WebSocket message 上限、1 MiB state 上限、future window、room
容量和 TTL 已在 example 中实现。正式公网产品仍需要 TLS 反代、账户/邀请模型、
共享 registry、跨实例 affinity、分布式限流、审计、指标和告警。

## 独立依赖与可复现性

demo 的 npm 锁文件只安装 Playwright。EmulatorJS/core 不作为 npm runtime
dependency 安装，而由 `prepare-assets.mjs` 直接下载三个固定 tarball、校验 npm
SHA-512 integrity，再用内置安全 tar reader 抽取 22 个 allowlist payload。这样
不会因为 core package 对 `latest` 的依赖而漂移，也不会安装 EmulatorJS 的构建
工具链。

runtime 使用官方 npm 包内的可审计 source files，`EJS_DEBUG_XX=true`；npm 包不
包含 release 的生成 minified bundle。两个 core `.data` 与之前稳定版物化内容的
SHA-256 相同。core source/license 关联来自固定 commit，但二进制与源码 commit 的
精确 association 仍只应表述为上游 build timestamp 推断。

## 尚未覆盖

- 两个客户端仍位于同一页面/浏览器进程，未做跨设备、后台节流、移动网络切换；
- 无 spectator、join-in-progress、host migration 或多玩家扩展；
- room 内存态不能跨 server replica，进程退出即失效；
- 没有账号、好友邀请、ban/rate-limit backend、持久审计或生产 telemetry；
- NES 只验证 FCEUmm 的 `F-1 Race`。旧式 GBK ZIP entry 会被 EmulatorJS 4.2.3
  解码成损坏路径，因此 manifest 选择内部 ROM 名为 ASCII 的 ZIP；
- FBNeo 只验证精确 `ldrun.zip` romset；
- 4.2.3 adapter 观察 native log callback，未来版本必须重新审计，不得静默复用；
- FCEUmm/FDS `Smash Ping Pong` 在 RTT + reconnect + rollback 组合故障后出现周期性
  state 漂移，已明确排除在 allowlist 外；正式上线前仍需跨设备长测并设定 resync
  频率 SLO。
