# Retrom non-streaming netplay demo

这个 example 在一个页面中运行两个彼此隔离、真实的 EmulatorJS 4.2.3
实例。两端各自执行 core、渲染画面；网络只传输带帧号的完整手柄快照和
state hash，不传输 canvas、音频或视频。

默认使用 demo 自带的 WebSocket 权威中继：两个 iframe 分别建立连接，服务端
为同一 room 的 slot 0/1 生成不可变 canonical frame。也可以切换到 in-page
relay，便于单独调试同步算法。

## 目录边界

- `src/`：fixed-delay lockstep、EmulatorJS bridge 和浏览器 WebSocket adapter；
- `server/`：无第三方运行时依赖的 demo-only WebSocket framing 与权威 room hub；
- `patches/`：加载在官方 4.2.3 loader 之前的窄补丁；
- `tools/`：依赖物化、静态/WS server 和真实浏览器 smoke；
- `test/`：protocol、hub、lockstep 和补丁的 Node 单元测试；
- `Makefile`：资产准备、全量验证和交互式启动的一键入口；
- `assets-manifest.json`：所有不可提交 payload 的大小与 SHA-256 事实源；
- `EXPLORATION.md`：源码探索、实验结论和未解决能力；
- `RESULTS.md`：本次 3000 帧浏览器证据。

ROM、BIOS、EmulatorJS runtime/core 和 `node_modules` 都被 Git 忽略。准备后，
运行 demo 所需的文件都位于本目录；仓库不会提交这些第三方或游戏 payload。

## 准备

需要 Node.js 20+。先安装固定版本的 smoke 依赖，再从本机已物化的
EmulatorJS 4.2.3 runtime 和 Pegasus 游戏目录复制、逐字节校验资产：

```bash
make install
make prepare PEGASUS_ROOT=/path/to/Pegasus
make assets
```

若本机尚无 Playwright 可用的 Chrome/Chromium，另执行
`corepack npm exec -- playwright install chromium`；也可在 smoke 时通过
`CHROME_PATH` 使用系统浏览器。

也可显式传入 `--pegasus-root` 与 `--runtime-root`：

```bash
node tools/prepare-assets.mjs \
  --pegasus-root /path/to/Pegasus \
  --runtime-root /path/to/emulatorjs/4.2.3
```

物化脚本只接受 manifest 中固定的 17 个文件；源或目标的大小/SHA-256 不一致
都会立即失败。FBNeo ROM 必须保留 `ldrun.zip` basename，否则 core 无法识别
romset。

## 运行

Makefile 提供一键入口。静态页面与 WebSocket relay 由同一个 demo-only Node
进程提供，因此不需要分别启动前后端：

```bash
make start
```

默认监听 `127.0.0.1:4174`，不会占用主工程的 `3000` 或 `8080`；服务端也会
拒绝把 `PORT` 覆盖成这两个保留端口。需要使用其他独立端口时：

```bash
make start PORT=4175
```

首次运行或本地资产缺失时，先物化 manifest 固定的依赖：

```bash
make prepare PEGASUS_ROOT=/path/to/Pegasus
```

打开命令输出的本地 URL，选择 NES/FCEUmm 或 FBNeo，然后点击 **Start
lockstep**。默认运行 3000 帧，每 120 帧暂停两端、序列化完整 state 并比较
SHA-256；任何不一致都会暂停 session 并显示失败。

页面内的 P1/P2 debug pad 会调用 EmulatorJS 公共 `gameManager.simulateInput`，
因此与键盘/手柄使用相同的捕获入口。canonical input 则从底层
`gameManager.functions.simulateInput` 注入，避免远端输入重新上行。

真实浏览器回归：

```bash
make test
```

脚本启动随机本地端口，在 Chrome/Chromium 中依次运行两个 core，各执行 3000
帧并注入 P1/P2 press/release。可用 `CHROME_PATH` 指定 Chrome 可执行文件。
若希望完整自动测试通过后继续启动人工验证页面，使用：

```bash
make test-and-start
```

快速回归可将帧数改为 manifest 支持的 600 或 1200，例如
`make test SMOKE_TARGET=600`。

## EmulatorJS 4.2.3 补丁边界

`patches/ejs-4.2.3-netplay.js` 只在每个 iframe 内完成三件事：

- 将 `/data/saves` 改为内存目录，避免两个同源实例竞争同一 IDBFS；
- 本地响应 4.2.3 的稳定版本检查，保证验证不访问外网；
- 在 `EJS_Runtime` factory 初始化时 chain `postMainLoop`，暴露可靠的帧完成
  hook；运行时初始化之后再赋值不能稳定生效。

官方 loader、runtime 和 core 二进制均未修改，且由 manifest 固定 SHA-256。

## 如何解释结果

通过意味着：这两个精确的 core/content 组合可以在双实例浏览器中，通过两个
真实 WebSocket 和服务端 canonical input，维持非串流 fixed-delay lockstep。

这还不是 RetroArch 完整 netplay：没有 prediction/rollback、replay 期间静音、
savestate resync、断线重连或公网房间服务。尤其 EmulatorJS 4.2.3 的
`loadState()` 返回时，底层任务可能仍在队列中；当前测试因两个冷启动 state
已经相同而跳过传输。因此“从不同 state 建房/重同步”仍需 patch 一个可等待的
原生加载完成接口。详见 `EXPLORATION.md`。
