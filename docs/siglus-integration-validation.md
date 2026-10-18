# Siglus 核心集成候选

2026-10-17 首轮在 `siglus-compat` PFB 中完成 `retrom-runtime / siglus` Target 的本地集成和限定产品验收，核心为独立 `siglus_rs` 候选。尚未发布核心或 runtime Release，也未改变 Retrom 的正式发行锁定；两款样本通过不代表全部 Siglus 作品兼容。

## 10 月 18 日对白语音回调修复

`0.71.2-dev.2` 的限定按需检查没有覆盖首次带语音的对白。随后用户在《初恋 1/1》中正常点击画面时报告 `SuspendError: trying to suspend without WebAssembly.promising`。从原生第二槽读档后连续点击，在第 11 次点击触发了完全相同的堆栈：WASM `0x67c017` 至 `0x92b059`，生成 JS `bt` 的 `1:68549` 和 `i` 的 `2:1869`。这不是旧候选或缺少浏览器 JSPI API。

根因是 KOE 对白语音通过 `wasm_bindgen_futures::spawn_local` 调度解码，在后续 wasm-bindgen 回调中读取文件；外层 `RetromGame` 的 Promise 入口已经结束。修复让 Web 语音读取留在当前串行的 JSPI 引擎栈内，公共 Content I/O 等待每个 Range 时继续让出事件循环；原生构建仍使用工作线程。Web 上的 CPU 解码仍在浏览器线程执行，与此前 `spawn_local` 相同。没有包裹所有浏览器回调或增设内容缓存。

新增的 `tools/run_jspi_voice_probe.mjs` 使用合成 PCM 和真实 `KoeEngine`。旧代码即使所有可变 Host 入口都已包装，仍在约一秒内稳定出现同一个 SuspendError；修复后两次成功语音各读取 3 段，中间一次异步读取失败正常返回，随后语音仍可启动，事件循环持续推进，没有未捕获异常。探针仅由 `compatibility-probe` feature 编入测试 WASM，不在产品核心内。生成胶水转换同时限定在 `RetromGame` 类内，避免测试 feature 的其他 `stop()` 方法与 Host 签名冲突。

| 语音修复候选 | 身份 |
| --- | --- |
| 本地版本 | `0.71.2-dev.3` |
| 核心源码树 SHA-256 | `5509da0a6726fe7888fd7ea45a450477cbbaf7843adbee0ac4c4fddc3d8c4457` |
| 两次相同完整 ZIP SHA-256 | `19ee1912efdd5436151679d007bbb7bbef147af8505428a67a4b891fbe924258`；15,245,831 字节 |
| runtime 源码树 SHA-256 | `7e6c14e5d0217ffdc6104ad3bde4522f92d279bfca2e1fd30e84c2133c54c291`；本次未改 runtime 源码 |
| 实际 Provider Module SHA-256 | `c8b465ca4acb2afa567ee729280254a8c09b009ce7ad9a4a00c869512a79109f` |
| Provider Bundle SHA-256 | `cf434bc64453b164b24c697e1df23ff495a45344201163e57429b8fdd7a4dfae` |
| 核心文件指纹 | `510a7559553ebe7e29762d89ab4886bd1336f3c01de177ef135b8367b2ea533f` |

真实 Product Run `1b1d2ad0-0847-4c19-b035-0146367f214b` 从原生第二槽读取相同起点，连续鼠标点击 24 次，经过此前第 11 次触发的语音位置，推进至“真是的”。实际语音资源 `koe/z0001.ovk` 共走过 180 次 VFS 分段读取，单段最大 262,144 字节，没有核心失败或未捕获异常；前后截图已逐图检查，见 `mouse-repro-{red,green}.json` 和 `green-dialogue-*.png`。

随后真实写入第三槽 `savedata/0003.sav`（306,264 字节）及缩略图，平台自动同步为新存档 `0d5f7fa9-15bc-4bb7-a5b7-b025a0091320`，旧存档保留。服务端 payload 解压一次即为 9 文件原生集合，包含新的进度文件。正常退出后，用新 Run `7a050ebc-0cc7-430c-b7b5-58387bacde6d` 恢复该集合，从游戏内第三槽读档，再点击 8 次推进至新的中文对白；同核心恢复没有变化警告。`green-native-save.json`、`green-server-save.json`、`green-save-payload-proof.json`、`green-restore-proof.json` 和前后截图记录该链路。

核心 Rust lib 重新通过 269 项、7 项原有忽略，核心 JS 11 项通过；PFB 正常校验 `20261018T070950Z` 为 VERIFIED。新候选通过完整 Provider 的正常准备／导入／启动使用。runtime 源码与此前相同，配套的其他 Target 指纹均未变；本次没有重跑此前的 runtime 全量、全后端或四视口 ACC，也没有重做全目录下载，不把历史记录改称当前新通过。

证据保留于忽略的 `.pfb/evidence/siglus-integration/jspi-callback/`，包括旧产品堆栈、探针红绿结果、重复归档和实际 Run 身份。修复后的两个 Run 无 console error、pageerror 或 crash；`green-console-final.json` 保留 iframe sandbox、Next Image LCP 和截图回读时的图形管线 stall 提示。已有 Run 冻结原 Provider 和核心资产，修复后须从游戏详情新建 Run。

## 10 月 18 日按需加载修复（dev.2 阶段记录）

以下为 `0.71.2-dev.2` 的实际限定验证；本段结果不覆盖上文随后发现的首次语音回调。

原启动下载 EXE、备份和全部 BGM 的原因是 Target 声明了 `PRELOAD_ONLY`，适配层同时将整个目录物化为 Blob。现在采用与 RPGMaker 相同的公共 Range policy；核心桥接只把原生文件读取转给公共 reader，并通过 WebAssembly JSPI 等待异步结果。浏览器须提供 `WebAssembly.Suspending` 和 `WebAssembly.promising`；实际验证使用 Chromium 149。文件路径／大小及生成的密钥配置是元数据，游戏内容不在桥接中另设缓存。

首次按需原型暴露了鼠标悬停也会加载资源的问题：只有帧循环被包装为异步入口时，进入读档菜单会触发 `SuspendError`。修复覆盖所有可变核心入口，并等待输入回调完成后再执行后续操作；异常清理也保证释放音频。旧行为必红的回归、实际编译后入口检查和同一个原生读档菜单均已复验。

| 按需阶段候选 | 身份 |
| --- | --- |
| 本地开发版本 | `0.71.2-dev.2`；不是正式发行锁定 |
| 核心源码树 SHA-256 | `dd9614f864ee4d897c57658e59169d5ffe993d0602debc6cb3ff78afd4b38a86` |
| 两次相同 ZIP SHA-256 | `5c59528030a998a7dd6f7fec37d1a3756b542dc6d0d2a7fe7e0a64c6ac7698c7`；15,045,771 字节 |
| runtime 源码树 SHA-256 | `7e6c14e5d0217ffdc6104ad3bde4522f92d279bfca2e1fd30e84c2133c54c291` |
| 实际 Provider Module SHA-256 | `2d7ffd4cd1273879c2fc9841c428dc85c5c005c4a9291e1e211d721b730c3331` |
| Provider Bundle SHA-256 | `dad18e4be5dca722be9f1ae43d40372a4731bd7470aad4fe1b3a7a3221f594a8` |
| 核心文件指纹 | `1bfaf360f4eb74106bfa60fda7b1cd033fe6f4dc4b18c0f28c8595bc55698304` |

本轮最终证据位于忽略的 `.pfb/evidence/siglus-integration/on-demand-v2/`，中间失败及修复证据保留在同级 `on-demand/` 与 `on-demand-*.json`。浏览器普通 HTTP 缓存已禁用，游戏文件请求与页面、索引、Provider 和存档请求分别统计。

| dev.2 构建的实际验证 | 结果与证据 |
| --- | --- |
| 空持久缓存，默认按需启动 | 《纯白》到标题画面读取 69 个文件、84,536,707 字节，约为整个 7,994,917,791 字节目录的 1.06%；只读取实际播放的 `a1.nwa`，没有读取 EXE、DLL 或备份。《初恋 1/1》到中文 CAUTION 首屏读取 25 个文件、36,388,990 字节，未读 BGM 或可执行文件；此数不是到标题画面的体积。见 `cold-results.json`、`cold-network.json` 和逐图检查的截图 |
| 显式全量下载与跨 Run 复用 | 《纯白》完成 3,174 文件／7,994,917,791 字节的公共预下载，再启动独立 ON_DEMAND Run，经过标题、原生 Load 菜单、中文正文，游戏文件请求为零。完整覆盖包括此前按需读取、中断预下载和最终续下，按唯一索引文件统计，没有把重试相加当作下载体积。见 `full-complete.json`、`full-cache-coverage.json`、`warm-on-demand-proof.json` |
| 断网继续操作 | 上述独立 Run 恢复中文存档后断网，标准 Gamepad 方向及 A 推进可见对白，游戏文件请求为零、无核心失败。前后截图已逐图检查；不代表未加载站点时可以离线冷启动。见 `offline-proof.json`、`mashiro-warm-restored-before-offline.png`、`mashiro-offline-after-gamepad.png` |
| 汉化原生存档与新 Run 恢复 | 《初恋 1/1》真实写入新的 `savedata/0002.sav`（299,847 字节）和缩略图；平台 payload 解压一次即为包含进度的 7 文件集合。同核心新 Run 从游戏内第二槽读取，鼠标继续推进到“坐在椅子上的我不禁缩成一团。”，没有核心变化警告。见 `hatsukoi-save-payload-proof.json`、`hatsukoi-restore-final.json`、`hatsukoi-restored-final-*-input.png` |
| 审核、发布与原生退出 | 《纯白》返回审核后使用最终候选预览，标题内退出触发平台退出检查、画布移除、音频关闭；随后正常通过并发布，再执行 Product 验证。见 `review-run.json`、`native-exit-proof.json` |
| 既有核心回归 | 同 Provider 下 WASM4 从原有即时存档恢复，核心指纹保持不变；标准 Gamepad 左移后，绿色方块左边界从 38 移至 0。见 `wasm4-regression.json` 与前后截图 |

缓存不可用、取消和失败清理另有公共读取回归及浏览器受控故障证据：显式 PRELOAD 在持久存储不可用时明确失败，相同故障下 ON_DEMAND 可以通过网络启动；预下载退出并稳定后不再请求游戏文件。浏览器故障注入在本轮早期候选完成，其 runtime 源码与当前候选相同，证据为 `on-demand/cache-failure.json`、`cache-unavailable-on-demand.json`、`preload-cancel.json`；没有将其冒称为最终核心重做的故障测试。当前核心对异步读取拒绝、后续读取恢复、挂起输入的串行执行和异常退出清理另有实际编译入口检查及 JS 回归。

本次 runtime 全量为 343 文件／1,987 项通过，lint、类型、构建和包校验通过；核心 Rust lib 为 269 通过、7 项原有忽略，核心 JS 11 项通过。实际编译后的 JSPI 检查覆盖所有可变入口，600,000 字节读取分为 262,144、262,144、75,712 字节，等待期间浏览器事件循环继续推进，异步拒绝保留错误原因并能进行后续读取。两次完整核心 ZIP 字节相同，PFB 正常校验 `20261018T055335Z` 为 VERIFIED，运行中的 Provider Module 与上表一致。

最终主要浏览器会话及独立冷缓存会话均无 console error、pageerror 或 crash。`console-final.json` 保留 iframe sandbox、Next Image LCP 和存档截图回读的图形管线 stall 提示；后者是性能提示，没有重现多采样 blit 非法调用。只复验了上述受影响链路和代表性既有核心；没有重跑全部后端、四视口 ACC 或将 10 月 17 日结果提升为本次构建的新通过。

以下 10 月 17 日结果保留为此前阶段的历史记录。

## 产品行为

- 以包含 `Scene.pck` 与 `Gameexe.dat` 或 `Gameexe.ini` 的完整目录导入，使用 `SIGLUS_PROJECT` 配置。脚本、图像、音频和影片保留为独立文件。
- 同一项目根目录同时提供 `scene.chs` 和 `gameexe.chs` 时，默认选择简体中文资源；管理员可显式改用原版。汉化图像覆盖由核心处理。
- 原始资源需要的密钥由核心从原始资源对恢复，不执行游戏的 EXE、DLL 或补丁程序，不依赖人工生成的 `key.toml`。
- 当前声明 `ON_DEMAND_AND_PRELOAD`，默认按需加载。目录挂载只注册路径和大小，核心实际读取时直接使用公共 `LazyContentReader`、Range、校验、持久缓存和取消；显式选择全量下载时由同一公共 Content I/O 在启动前补全缓存。Siglus 桥接不维护下载器或资源字节缓存。原生解码器仍可能完整读取一个已选中的压缩文件。
- 存档为 `GAME_SAVE`：玩家先在游戏中保存，再由平台同步原生存档集合；新运行在引擎启动前导入集合，玩家从游戏内读档。设置和已读标记不能单独充当进度存档。公共层对整个集合执行一次 gzip。
- 手柄沿用公共指针：方向键或左摇杆移动，A 左键确认，B 右键；键盘和鼠标保持独立。
- 影片采用有界解码和帧队列；大文件仍占浏览器磁盘缓存，不能把磁盘缓存大小等同于播放内存需求。声音和压缩资源的具体上限以核心文档为准。

导入时发现旧目录总量校验误用了 8 GiB 单文件上限。现分别验证单文件不超过 8 GiB、整个目录或归档展开内容不超过 64 GiB，保留 10,000 个项目文件的既有边界。

## 10 月 17 日首次候选身份（历史）

| 输入 | 当前身份 |
| --- | --- |
| Retrom / runtime 分支 | `codex/siglus-compat` |
| 核心分支 / 上游基线 | `feat/siglus-compat` / `5f358ef1360b0439e92aed2b749e8f25d8721954` |
| 核心 ABI | `siglus-host-v1` |
| 核心源码树 SHA-256 | `d315b7cea47a25960120f9f6a6607815ca0ab0fd215a367b40ff150ba189770d` |
| 重复构建完整 ZIP SHA-256 | `53959770dd15c84afa1a03ca8ea11df0c196c2b3f164c8f63659a9f65b2f09f6` |
| runtime 源码树 SHA-256 | `15ead34b321eebffa4c9003d61a848d163aaffb51dd0a20b3e941d62033cc902` |
| 实际开发 Provider Module SHA-256 | `95208e4f07555fba1ae70769538661adadc6bb2da2d494f5ba9a5252d7d1eb85` |
| 实际 Provider Bundle SHA-256 | `51da94744827e832d9859783e1e6b8c9397d85486e00212f1689a3c30594f6c7` |
| Siglus 核心文件指纹 | `a23c7b1fbcb86b52e138997dd5c90b3d7408276e2f38902f2ce32062b95e0a37` |

两个完整核心构建（含描述符与许可证）得到字节相同的 15,036,504 字节 ZIP。完整 Provider 候选经正常准备、校验和 PFB 导入使用；不是手工替换宿主资产。开发基座沿用已验证的 `0.71.1` 版本，来源明确为候选，不代表发行了新的同名正式版本。源码与素材证据保存在忽略的 `.pfb/evidence/siglus-integration/`，质量日志在相邻的 `siglus-feasibility/logs/`；商业游戏、存档和构建产物不提交。

## 集成时修复的实际问题

| 问题与触发 | 修复与验证 |
| --- | --- |
| 9.8 GB 目录被当作超限单文件拒绝 | 分离 8 GiB 单文件与 64 GiB 总量；边界回归、真实样本重扫及受管复制通过 |
| 并发大目录预下载时，健康的持久写入超过原 500 ms 截止时间 | 必需预下载使用有界 5 s 存储预算；普通按需读取保留原预算与降级。回归覆盖慢写、及时取消和永久不响应，真实浏览器 750 ms 慢写及不可用后端通过 |
| 已完成缓存的 Blob 交付再次遍历全部内容字节 | 复用不可变 OPFS generation 的完成凭据、准确文件大小和租约，直接交付 File；缺失或大小不符仍走校验／恢复路径。实际跨实例 BLOB 测试无内容回读、无网络请求 |
| WASM 已启动但画面黑屏，并有 WebGL 多采样 blit 错误 | 创建 WebGL2 上下文时显式关闭 antialias，与 wgpu 的单采样输出一致；实际首帧、标题和正文显示正常，原 GL 错误消失 |
| 一次释放数千文件租约超过公共 RPC 的并发上限 | 有界顺序释放，并在某项失败时继续清理其余项；500 文件回归及两个真实大目录退出通过 |
| 暂停只停止画面，音频继续运行 | 在 WASM glue 缓存音频构造器之前安装生命周期跟踪；初始化失败也清理。构造器缓存时序回归先红后绿；最终浏览器暂停时帧数、AudioContext 时间和音频提交数保持不变，恢复后三者继续推进 |
| 完整缓存仍可能在后续 Blob 交付中重新下载 | 预下载之后的完整物化也持有有界 5 s 磁盘预算，并在成功、失败或取消后释放；普通按需读取仍用原预算。真实浏览器将文件引用读取延迟 650 ms，旧代码退回 Blob 复制，新代码保持 File、零内容回读和零网络 |
| 游戏内结束已执行，浏览器仍继续调帧 | `SiglusHost::step()` 同时观察 `SYSCOM.END_GAME` 设置的结束标志；原悬挂脚本不必先自然 halt。旧行为必红的控制回归、完整 Rust lib 和重复构建通过；最终两样本游戏内退出均停止调帧、移除画布、关闭音频，并进入平台退出检查 |

这些修复保持公共 Provider V1、Launch Envelope 和 Save API 的现有语义，没有增加宿主专用运行入口或数据库表。

## 10 月 17 日产品验证结果（历史）

实际样本为《初恋 1/1》汉化目录（6,549 文件，9,818,574,783 字节）和《纯白》汉化目录（标题画面为 `ましろ色シンフォニー`，3,174 文件，7,994,917,791 字节）。它们验证不同资源、汉化和影片路径，不代表全部 Siglus 游戏兼容。

已完成：正常服务端扫描及受管复制；大型目录导入边界红绿回归与原样本重扫；预下载进度；下载中退出后不再请求游戏文件；持久缓存不可用时明确失败。浏览器普通 HTTP 缓存禁用。缓存故障通过只在测试浏览器的预下载 Worker 中关闭持久元数据存储注入，保留原 Provider 字节，未修改商业素材。

冷缓存经过失败修复和中断续下，最终请求覆盖两个目录全部 9,723 个文件、17,813,492,574 字节，没有把重试次数当作唯一下载数。核心实际收到准确数量和大小的磁盘 Blob 文件；独立 Review 与 Product 运行复用完成缓存，游戏文件请求为零。页面、索引、Provider 程序和存档请求另计。原素材只读，导入副本的来源路径随后移走，游戏继续从受管副本运行。

两个样本已经走过审核预览、正常发布、Product Launch、手柄方向移动与 A 确认、中文正文和游戏内保存。最终核心另行复验《纯白》的 Review，再发布并运行；两款游戏都在最终核心下创建新存档，并用不同 Run 恢复、从游戏内 Load 进入中文正文。核心变化时的正常确认保留旧存档，后续进度另存为新存档；使用最终核心的新存档再次启动没有核心变化警告。

公共服务器存档解压一次即为原生文件集合。最终《纯白》的 716,778 字节数字 `.sav` 记录“自己那向着天花板伸去的手。”，《初恋》的 299,923 字节数字 `.sav` 记录两句中文对白，同时包含缩略图、设置及已读数据。新 Run 恢复后，《纯白》鼠标确认推进到下一段，《初恋》标准 Gamepad A 继续追加下一句。恢复可能从该原生保存点重新执行一段场景，不是即时状态恢复。

断开浏览器网络后，《初恋》Gamepad A 从第一句推进到第二句，《纯白》鼠标确认推进中文段落，游戏内容请求为零。最终核心的独立 Review、Product 及两次新存档恢复运行，合计游戏文件请求仍为零；这不包括页面、索引、Provider 和存档请求，也不宣称浏览器在未加载站点时可以离线冷启动。

| 最终证据（忽略的 `siglus-integration/` 目录） | 实际结果 |
| --- | --- |
| `mashiro-release-proof.json` / `hatsukoi-release-proof.json` | 一次 gzip、原生进度文件、文件大小、作用域和中文记录 |
| `same-core-restores-final.json` 及 `same-core-*-restored-text.png` / `*-continued*.png` | 两个独立 Run 接收原生文件，在同一最终核心中读取并继续操作 |
| `cache-offline-final.json` / `full-preload-network-coverage.json` | 完整冷覆盖、最终候选零游戏文件请求复用，以及断网后的可见输入结果 |
| `native-exit-final.json` / `audio-pause-final.json` / `volume-final.json` | 游戏内退出、暂停／恢复、静音／恢复；退出后音频关闭 |
| `wasm4-final-regression.json` | 既有 WASM4 在最终 Provider 中恢复真实即时存档；标准手柄左移使绿色方块从原位置向左移动 150 像素，核心指纹未变 |
| `console-final.json` | 最终阶段没有 console error、pageerror 或 crash；保留下述性能及开发提示 |

最终 console 中仍有既有 iframe sandbox 提示、Next Image LCP 建议，以及原生存档截图回读时的图形管线 stall 性能提示。最后一项不是 WebGL 非法调用错误；存档截图和恢复有效。本轮首帧黑屏时的多采样 blit 错误已通过原 Case 复测消失，不能把“没有 error 级日志”等同于“没有 warning”。

## 10 月 17 日画面和性能范围（历史）

逐图检查桌面 1440×960、390×844、4K／150% 及回到桌面。游戏原生画布始终为 1280×720，CSS 分别为 1440×810、390×219 和 2560×1440；iframe 随视口，画面保持约 16:9、没有横向溢出。4K 使用逻辑 2560×1440、DPR 1.5，并另外保存和检查 CDP 直接取得的 3840×2160 原始截图。手机竖屏保留完整画面，原生文字较小；这不是手机触屏可读性通过声明。

软件渲染 Chromium／SwiftShader 的四个约 5 秒窗口实际得到 35、22、38、37 帧（约 4.4–7.6 fps），父页和游戏页观察器没有捕获到超过 50 ms 的 Long Task。软件渲染速度较低，不能据此声称硬件加速桌面或移动设备性能达标，也不能用零 Long Task 推定总渲染负载很低。实际尺寸、计数和观察器记录在 `layout-performance-final.json`，帧率结论只适用于该环境。

内存采样是同一个持久 Chromium 的整个进程树 RSS 求和，包含多标签页、两个游戏和 SwiftShader，共享页也可能重复计数；不作为单游戏物理内存需求。最终阶段此口径峰值约 4,281 MiB。完整游戏目录仍保存在磁盘缓存中，影片的有界解码不会因为影片文件大而一次预解码全部帧。

## 10 月 17 日质量与 Case 范围（历史）

最终源码已通过 runtime 342 文件／1,981 项测试、lint、类型、构建和包校验，18 项实际 Content I/O 浏览器检查，Web 99 文件／498 项测试及 lint／类型／UI 规则／生产构建，后端质量、API 生成一致性和显式独立 PostgreSQL 全量集成测试。核心 Rust lib 为 269 通过、7 项原有忽略；浏览器音频初始化修复另有 3 项 JS 生命周期回归，并进入核心候选构建门禁。

最终未过滤 `ACC-RF-BROWSER` 为 `118353827ea6483c`：四视口 32/32 通过，耗时 915.757 秒，输入摘要 `9d6c13fa6e2cdf50fbd0b8b822f0cf64a17ca6836a9c022301e056cfa03a9ebf`。原始结果在 `.artifacts/acceptance/118353827ea6483c/cases/acc-rf-browser/`。PFB 正常校验 `20261017T070240Z` 为 VERIFIED；门禁日志及摘要集中在 `release-quality-proof.json`。

中间两次完整浏览器命令被终端生命周期终止，保留为中断而非通过；改用独立有界验收进程后得到最终完整结果。runtime 重复全量测试中曾有一个未改动的压缩测试超过既有 5 秒截止时间；没有抬高超时或降低断言，最终全量使用 `--maxWorkers=2` 通过。浏览器辅助脚本曾在恢复暂停完成前点击、或在原生菜单动画未结束时选槽位；修正等待并逐图重跑，没有把这些自动化操作失误登记为新的核心缺陷。

T01–T47 逐项记录在忽略的 `case-scope.json`，这里按本轮证据范围汇总，不提升整个项目的验收状态：

| Case | 本轮范围 |
| --- | --- |
| T03 | 新增导入体量边界及真实大目录重扫通过；不是全部字段人工复验 |
| T07、T08、T10、T39、T40、T44 | Pegasus 导入、受管副本、审核和扫描计数的受影响链路；未新增 EmulationStation 或 BIOS 故障专项 |
| T15、T16、T17、T22、T30、T36、T46 | 公共声明／配置／Launch／审核／最近游玩；无核心注册 migration、种子或运行凭据 |
| T18、T19 | 真实原生存档、新 Run 恢复、继续输入、核心变化警告和旧存档保留；WASM4 未变指纹回归 |
| T27 | 上述视口逐图检查；手机真机触摸、可读性和性能未覆盖 |
| T32、T33、T34、T37、T47 | 同 PFB 跨仓构建、职责边界和最终门禁；只覆盖新增 Target 及代表性既有核心 |
| T28 | 未执行 20,000 Game、50,000 文件查询与 200 用户负载 |
| T38 | 本地候选配套一致性通过；正式核心／runtime 发行和 Retrom 正式固定尚未进行 |
| T42、T43 | 新 Target 不要求 BIOS；不扩大既有 BIOS 人工验收范围 |
| T01、T02、T04、T05、T06、T09、T11、T12、T13、T14、T20、T21、T23、T24、T25、T26、T29、T31、T35、T41、T45 | 领域未改变，保留独立 PostgreSQL 和完整浏览器自动回归；不将其视为每项全部人工／故障窗口新通过 |

10 月 17 日候选仅支持完整预下载；此限制已由 10 月 18 日的公共按需读取修复解除。显式全量下载仍需要足够浏览器持久存储空间。没有承诺所有汉化补丁、全部路线、影片、浏览器、物理手柄或移动设备兼容。128 MiB 平台存档集合上限、单文件及目录导入上限仍适用。

## 发布边界

核心 fork 地址与维护分支仍是本地候选的发布准备。正式发行前还须完成核心链接依赖的许可证清单、精确源码配套和不可移动的核心资产发布，runtime 固定其 Release 身份并发布新版本，Retrom 再固定正式配套输入、复验同一产品案例。当前 `data/runtime-inputs.json` 保持原正式输入；本轮没有新提交、远端写入、PR、合并或 tag。

改动保留在 `siglus-compat` 的三个独立工作树：Retrom 与 runtime 为 `codex/siglus-compat`，核心为 `feat/siglus-compat`，包括前一阶段既有核心修复。PFB 保持运行供人工试用。基线三个 checkout 检查时均干净，根仓库原有未跟踪 `docs/` 保留；未在根仓库登记或暂存子仓库源码。
