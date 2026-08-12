# Netplay demo protocol

本文档是 `netplay-demo` 自身的协议说明。协议只服务于 EmulatorJS 4.2.3、
FCEUmm 与 FBNeo 的产品候选验证，不承诺与 RetroArch wire protocol 兼容。

## Room API

`POST /api/rooms` 接受：

```json
{"profileDigest":"<64 位小写 SHA-256>"}
```

请求必须来自配置的同源 `Origin`。响应包含随机 `roomId`，以及 slot 0/1 各自的
一次性 join token。`GET /api/rooms/{roomId}` 只返回公开状态和计数，不返回 token。
room 默认五分钟无活动过期，服务进程最多保留 100 个 room。

浏览器随后连接：

```text
GET /netplay?room=…&slot=0&token=…&profile=…&resume=…&after=…&hafter=…
```

- `profile` 必须与建房时完全一致；它覆盖 runtime、adapter、core、content、BIOS
  和确定性设置；
- 第一次连接不带 `resume`；服务端在 `hello` 中返回不透明 resume token；
- 意外断线后 slot 保留十秒。重连必须同时提供原 join token 与 resume token；
- `after`/`hafter` 是客户端已收到的最后 canonical frame/hash frame。服务端按序
  重放有界历史中的缺失消息，全部 delivery 完成后才发送 `resume`；客户端不能把
  WebSocket `hello` 当成恢复模拟的边界。

## WebSocket messages

客户端到服务端：

- `contribution { frame, values[24] }`：本 slot 在一个逻辑帧的完整输入快照；
- `hash { frame, digest }`：RASTATE `MEM ` core payload 的 SHA-256；
- `state { transferId, frame, digest, coreDigest, state }`：slot 0 发布的、base64
  编码的完整 RASTATE；
- `state-applied { transferId, digest }`：slot 1 在校验并处理 state 后的确认。

服务端到客户端：

- `hello`：连接身份、resume token、租约时间与是否为恢复连接；
- `frame { frame, players[2][24] }`：两个 contribution 组成的不可变 canonical
  frame；
- `hash-result { frame, matched, digests[2] }`：同一逻辑帧的 core state 比较；
- `state` / `state-applied`：经服务端验证、限制为 1 MiB 的状态转发与确认；
- `pause` / `resume`：peer 离线与租约恢复边界；
- `metrics`：不参与模拟结果的观测计数；
- `error`：协议校验失败，随后以 WebSocket policy error 关闭。

所有 input contribution、canonical frame 与已发布 hash 都不可修改。服务端限制
future window、WebSocket/message 大小、room 数量和租约时间。WebSocket 依赖 TCP
的可靠、有序传输；这里没有在应用层实现 UDP 丢包补偿。

## Late-join epoch

late join 不重放 authority 的全部历史。demo 使用 stop-the-world 边界：

1. `hosting`：只有 slot 0 的 core 存在，按单机模式运行，尚未产生 canonical
   netplay frame；
2. `joining`：在原生帧边界暂停 slot 0，记录 authority native frame，再创建和
   预热 slot 1；
3. `synchronizing`：校验两端 profile，连接 room，将 authority 当前 RASTATE 作为
   epoch 1 的 frame 0 传输给 slot 1；
4. slot 1 只有在原生 load callback 完成、完整 payload byte-exact 且 core digest
   匹配后才发送 `state-applied`；
5. 两端重置输入、prediction 和 rollback ring，从新 epoch 的 net frame 1 开始。

这一过程的网络成本与 A 已运行的时长无关，但仍受 1 MiB state 上限、core
确定性和外部磁盘/RTC 状态的约束。当前浏览器验证在同一页延迟挂载 slot 1；
真实跨设备邀请与持久房间发现不在本 example 内。

## Simulation invariants

每个客户端维护独立的逻辑 `netFrame`，不使用 EmulatorJS 的原始 RAF frame 作为
网络时钟。执行规则如下：

1. 每帧先保存 frame-before state，并立即发送本地完整输入；
2. canonical input 尚未返回时，沿用最后确认的远端输入；预测最多八帧；
3. 迟到 canonical 与已执行快照不同，则加载最早受影响帧之前的 state，按历史
   逐帧 replay 到当前位置；
4. replay 期间音量强制为零、fast-forward 打开，并用冻结 canvas 遮住中间画面；
5. checkpoint 只在该帧 canonical 已确认、所有 rollback 已完成后计算；
6. mismatch 时 slot 0 是 state authority。传输 payload 先校验完整 state digest，
   是否需要加载则比较 `coreDigest`；加载/跳过完成后两端再次验证 core digest。

state ring 最多保留 120 帧，单次 rollback 超过此范围会 fail closed。room server
不会执行 core，也不接触 ROM/BIOS；它只处理 input、hash、状态转发和租约。

## Production integration boundary

当前 room service 是可部署的单进程参考实现，不是最终互联网控制面。正式产品仍
应在它前面补充 TLS 终结、账户/邀请权限、持久化或共享 room registry、跨实例
路由、速率/配额、审计与监控。若经过反向代理运行，必须设置准确的
`PUBLIC_ORIGIN=https://host.example`，不能把任意 Origin 加入 allowlist。
