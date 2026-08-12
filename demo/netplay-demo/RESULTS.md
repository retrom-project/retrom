# Browser smoke evidence

本文件记录 2026-08-12 的历史可复现实验，不是未来版本的替代验收证据。命令：

```bash
corepack npm test
corepack npm run verify-assets
corepack npm run smoke
```

环境：Chrome 149.0.7827.55、EmulatorJS 4.2.3、headless Linux、WebSocket
authoritative relay、input delay 4 frames、每 120 帧一次完整 state checkpoint。
smoke 在 session 启动后分别给 P1 Start 与 P2 A 注入 press/release。

| Core/content | Frames | Checkpoints | Desync | Stalls A/B | Canonical input | Initial state | Final SHA-256 |
| --- | ---: | ---: | ---: | ---: | --- | ---: | --- |
| FCEUmm / FDS Smash Ping Pong | 3000 | 25/25 | 0 | 0/0 | 4 transitions, 10 non-neutral frames | 112,464 B | `e56a4b133439b5623d019afa0bee988d7076c2d350d099b05ff304130d14b68a` |
| FBNeo / Lode Runner | 3000 | 25/25 | 0 | 0/1 | 4 transitions, 10 non-neutral frames | 78,976 B | `b6bd4c99bccc2293f0c18359223dde97bf2ae625e19aa0f0939fcddb9aca7d4b` |

两套 session 的初始摘要在传输前已相同，`stateSeedMode` 均为
`cold-start-aligned`。初始 `getState()` 同步调用耗时样本：FCEUmm 两端约
3.58/1.72 ms，FBNeo 两端约 1.61/0.47 ms。此单次 headless 样本只用于发现
数量级，不是性能承诺。

本次 PASS 证明 fixed-delay WebSocket lockstep 的限定可行性。它没有证明
savestate transfer/resync、prediction/rollback、跨设备或公网运行。
