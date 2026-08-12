# EmulatorJS 4.2.3 non-streaming netplay demo

这个独立 example 在一个页面中运行两个彼此隔离的 EmulatorJS 4.2.3 实例。两端
各自执行 core、渲染画面和生成音频；网络只交换输入、state hash、必要的
savestate 和房间控制消息，不传输 canvas、音频或视频。

当前实现已从 fixed-delay POC 升级为产品候选验证：

- 最多八帧的远端输入 prediction；
- 迟到输入触发 state rewind 与逐帧 replay；
- replay 期间静音、fast-forward，并冻结展示画面；
- 从真实不同 state 开始的建房同步，以及 hash mismatch 后的 authority resync；
- A 先单机运行、B 延迟加载后从 A 的当前 state 建立新 netplay epoch 的 late join；
- 带 join token、profile 校验、TTL 和容量限制的 room API；
- 十秒 resume lease、指数退避重连，以及 canonical/hash history replay；
- FCEUmm/NES 与 FBNeo 两个真实 core 的浏览器 fault-injection smoke。

它不是 RetroArch wire-compatible netplay，也不是 Retrom 主工程的模块。目录复制到
任意位置后仍可独立安装、准备、测试和运行。

## 目录

- `src/`：rollback client、EmulatorJS bridge、session 和 transport adapter；
- `server/`：只使用 Node 标准库的 room hub 与 WebSocket framing；
- `patches/`：只针对 EmulatorJS 4.2.3 的版本化 adapter patch；
- `tools/`：可复现资产物化、HTTP/WS server 和真实浏览器 smoke；
- `test/`：prediction/rollback、state、room、resume、安全边界和 manifest 单测；
- `assets-manifest.json`：npm tarball integrity 与所有运行 payload SHA-256；
- `PROTOCOL.md`：room/WS 消息、恢复流程和模拟不变量；
- `EXPLORATION.md`：源码探索、能力矩阵与风险；
- `RESULTS.md`：最近一次真实浏览器历史证据。

ROM、BIOS、EmulatorJS runtime/core、下载缓存和 `node_modules` 均由
`.gitignore` 排除。仓库只提交代码、锁文件、manifest 与文档。

## 独立准备

前置条件：Node.js 20+、corepack、npm registry 与 GitHub raw 内容可访问，并且
准备阶段能读取一份合法的 Pegasus 游戏目录。demo 不读取 Retrom 根目录下的任何
文件，也没有 `../../data` 之类的路径依赖。

```bash
cd netplay-demo
make install
make prepare PEGASUS_ROOT=/path/to/Pegasus
make assets
```

`make prepare` 会完成：

1. 从 manifest 固定的 npm URL 下载 EmulatorJS、FCEUmm、FBNeo 4.2.3 tarball；
2. 先验证 npm SHA-512 integrity，再只抽取 allowlist 文件并验证逐文件 SHA-256；
3. 下载固定 commit 的 core license 并校验；
4. 从 `PEGASUS_ROOT` 复制两个 manifest 固定的 ROM 文件并校验。

包缓存位于本目录 `.cache/`，所有可运行文件位于 `vendor/` 和 `assets/`。完成这一步
以后，运行与测试不再依赖 Pegasus 路径或外网。FBNeo 的文件必须保留
`ldrun.zip` basename；FCEUmm 夹具 ZIP 内的 ROM 文件名必须保持 ASCII，避免
EmulatorJS 4.2.3 把旧式 GBK ZIP entry 解码为不可打开的路径。

若没有 Playwright 自带的 Chromium，可执行：

```bash
corepack npm exec -- playwright install chromium
```

也可在 smoke 时用 `CHROME_PATH=/path/to/chrome` 指定系统浏览器。

## 一键运行和验证

```bash
make start
```

一个 Node 进程同时提供静态页面、room API 和 WebSocket relay。默认监听
`127.0.0.1:4174`，明确拒绝主工程使用的 `3000`、`8080`：

```bash
make start PORT=4175
```

部署在 HTTPS 反向代理后时，应用自身仍监听明文 HTTP，并配置精确外部 origin：

```bash
PUBLIC_ORIGIN=https://netplay.example.test make start HOST=0.0.0.0 PORT=4175
```

不要把这个参数设置为通配符。互联网部署还需要账户权限、共享 room registry、
限流和监控，见 `PROTOCOL.md`。

完整验证：

```bash
make test
```

它依次运行 Node 协议/模拟测试、22 个资产校验，以及两个真实 core 的 Chrome smoke。
smoke 默认先让 A 单独运行 10 秒，再加载 B；加入后各运行 3000 个 netplay
帧、注入 100 ms RTT、双端按键、一次 WebSocket 断线和一次
接收端额外 core frame，并验证 rollback/replay 静音、resume、真实 state resync 与
最终 checkpoint。
当次机器可读证据会写入被 Git 忽略的
`test-results/netplay-smoke.json`。
快速检查可以使用：

```bash
make test SMOKE_TARGET=600
```

可用 `SMOKE_JOIN_DELAY=0|3000|10000` 覆盖浏览器验证的加入延迟。交互页面
另提供立即、3 秒、10 秒、1 分钟和 1 小时选项。

全部测试通过后继续保留人工页面：

```bash
make test-and-start
```

## 4.2.3 adapter patch

`patches/ejs-4.2.3-netplay.js` 在官方 loader/core 启动前完成以下窄改动：

- 将两个 iframe 的 save filesystem 隔离为各自的内存目录；
- 本地响应固定版本检查，运行期不访问外网；
- 在 runtime factory 初始化时 chain `postMainLoop`；
- 给 `GameManager` 增加 `runNetplayFrame()`，从暂停状态精确执行一个 core frame；
- 给 `GameManager` 增加 `loadStateAndWait()`：捕获 runtime 的原生 state-load 日志
  回调，在同步 deserialize 返回后的 microtask 才 resolve，并验证目标 state bytes；
- 处理无权限 wake lock 的降级，避免浏览器产生未处理 rejection。

4.2.3 的原始 `loadState()` 仍立即返回。adapter 在静音/隐藏输出边界内临时恢复
main loop，让 RetroArch blocking task queue 前进；一旦原生 callback 返回便立即
暂停。启动 smoke 会让 A 先单机运行并接受输入，之后才创建处于独立冷启动
state 的 B，再经 WebSocket 传入 authority state；只有 `changed=true`、
`nativeCompletion=true` 且加载后完整字节一致才继续。

checkpoint hash 只覆盖 RASTATE 的 `MEM ` core payload，避免把非确定性的前端
metadata 当成 core desync；传输与完整性校验仍覆盖整个 RASTATE。

## Late join 边界

在 `hosting` 阶段只创建 A 的 EmulatorJS 实例，A 不提交 netplay frame，而是按
普通单机模式持续运行。B 加入时，demo 在原生帧边界暂停 A，再加载并
预热 B。两端 profile 一致后，A 当前的完整 savestate 以 frame 0 传给 B；B
完成原生加载、完整 state digest 和 core digest 确认后，两端清空输入并从
epoch 1 / net frame 0 同时恢复。

因此 B 不需要重放 A 过去一小时的输入；加入成本只取决于当前 state
大小和加载时间。当前自动化为同页延迟创建 B 的验证，并在 B 出现前对 A
注入真实输入；把 A/B 拆到两个设备后仍需要产品的邀请、账户权限和跨设备
时序验证。

## 产品边界

本 demo 已证明两个固定 core/content 组合具备非串流 rollback netplay 所需原语，
并提供可拆出的前后端参考实现。进入正式产品实现时仍必须保留 core allowlist 和
版本指纹，不能把结论外推到其他 ROM、core 或 EmulatorJS 版本。

仍未覆盖的能力包括：RetroArch 协议互通、spectator、主机迁移、真实跨设备 late
join、跨设备/后台 tab 的长时间测试、共享多实例 room registry、账户授权、反滥用、
可观测性与公网运维。FCEUmm/FDS `Smash Ping Pong` 被排除在 allowlist 外：它在
组合 fault smoke 后发生周期性 state 漂移；这不应被“自动恢复成功”掩盖。
详见 `EXPLORATION.md` 与 `RESULTS.md`。
