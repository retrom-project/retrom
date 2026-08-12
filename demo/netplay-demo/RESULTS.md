# Browser smoke evidence

本文件记录 2026-08-12 的历史可复现实验，不替代未来版本的当次验收。命令：

```bash
make test
```

环境：Chrome 149.0.7827.55、EmulatorJS/core 4.2.3、headless Linux、WebSocket
authoritative relay、100 ms 模拟 RTT、input prediction window 8 帧、每 120 帧一次
core-state checkpoint、late-join delay 10 秒。每个 session 都包含：

1. 只启动 A，在 B iframe 仍为 `about:blank` 时运行 10 秒并注入一次单机输入；
2. 暂停 A，之后才启动 B，将 A 的当前 state 以 epoch 1 / frame 0 传给 B；
3. B 对真实不同 state 完成 byte-exact 原生加载；
4. P1/P2 press/release，制造迟到输入与 rollback；
5. slot 1 WebSocket 主动断线、使用 opaque resume token 恢复；
6. slot 1 额外执行一个未跟踪 core frame，制造真实 hash mismatch；
7. authority savestate resync 后继续到 3000 帧。

| Core/content | A solo | Checkpoints | Rollback A/B | Stall A/B | Resync | Reconnect | State | Final core SHA-256 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| FCEUmm / F-1 Race | 10.045 s / 579 frames | 26 | 6/3（13/6 帧） | 0/0 | 1 @ 93 | 1 | 13,752 B | `276b9ac048a1606bc8a09dad1bbdf1477108b45b7948304f3444f6988305af69` |
| FBNeo / Lode Runner | 10.040 s / 551 frames | 26 | 5/4（9/8 帧） | 0/0 | 1 @ 92 | 1 | 78,976 B | `2e5c64c8f869865ee91d3a5bc218c328c15a2dda4d081700afcf5bec6c6cbfcf` |

两套组合都只发生注入故障对应的一次 resync；该次加载与初始建房加载均为
`changed=true`、`nativeCompletion=true`、`byteExact=true`。每次 rollback 的
replay 都记录为静音运行，late join 前 A 各接收 2 个单机输入事件，最终
checkpoint 匹配，浏览器 console 无 error。
由于按键由 wall-clock 调度，不同次运行的具体 canonical input frame 可不同，
因而最终 digest 不是跨运行的 golden value；门禁断言的是同一 canonical
history 下双端 digest 一致。每次 smoke 的机器证据写入被忽略的
`test-results/netplay-smoke.json`。

探索时使用的 FCEUmm/FDS `Smash Ping Pong` 在同一 3000 帧组合故障后产生 24 次
resync，故未纳入 allowlist。另有中文 GBK entry 名的普通 NES ZIP 会被 4.2.3
解码为损坏路径；当前 F-1 Race ZIP 内部为 ASCII 文件名。两项负向发现都不应外推
为整个 core 的结论，正式产品仍须以 core/content 指纹 allowlist 管理。
