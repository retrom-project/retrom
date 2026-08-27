# RPG Maker runtime 固定 Release 资产

本目录的 RPG Maker runtime 采用与 EmulatorJS 相同的预构建资产消费模式。Retrom 不在
`prepare-deps`、`make dev`、镜像构建或应用启动期间编译 EasyRPG/mkxp，也不从本机其他 ignored
目录复制 runtime。发布主体仅允许以下两个仓库：

- <https://github.com/xxxsen/Player>
- <https://github.com/xxxsen/mkxp-z-libretro-emscripten>

每项产品 runtime 由 `manifest.json` 固定 repository、不可变 release tag、tag 的完整 commit、asset
文件名/URL 和 adapter ABI。不得使用 `latest`、branch、GitHub Pages 或第三个发布主体。

## 身份与完整性边界

Release 准入身份是：

```text
repository + release tag + tag commit + asset filename/URL + adapter ABI
```

Git manifest 不声明远端 asset 的 expected size/SHA-256，也不以 SHA 判定同 tag 的兼容性。产品决策是：
固定 tag 下同名资产的重新构建结果，只要 metadata 仍精确绑定同一 tag commit 和 adapter ABI，就视为
兼容。下载器仍会记录每次物化得到的 observed size/SHA-256，但它们只用于：

- 检测本机缓存被截断或篡改；
- 为同源 runtime 内容响应提供长度、ETag 和 Blob 重建坐标；
- 冻结 Retrom 数据库中的 artifact-set 与 save/Launch 绑定；
- 诊断具体一次下载或运行结果。

Observed digest 不是远端 Release 准入身份，也不得回写成 manifest 的 expected digest。

## 当前固定 Release

### EasyRPG Player

- repository：`xxxsen/Player`
- tag：`retrom-web-0.8.1.1-r2`
- tag commit：`41a98c367685e0bd7ccb5f8f180b2799a1fae909`
- adapter ABI：`easyrpg-save-v1`
- assets：`easyrpg-player.js`、`easyrpg-player.wasm`、`retrom-runtime-release.json`
- release：<https://github.com/xxxsen/Player/releases/tag/retrom-web-0.8.1.1-r2>

该 tag 的 workflow 锁定实际使用的 EasyRPG Player、liblcf、buildscripts 与 Retrom patch，并随 Release
提供机器可校验的元数据。构建同时把 EasyRPG 的游戏文件基址固定为
`/runtime/rpg-project/`；不得使用上游默认的页面相对 `games/`，否则 `/play/games/{launchId}/index.json`
会绕过 Retrom 的项目内容端点并稳定 404。2000/2003 复用同一 JS/Wasm bytes，但以独立 route、engine mode
和 artifact 登记，不能由一个世代的产品结果外推另一个世代。

### mkxp-z libretro Web

- repository：`xxxsen/mkxp-z-libretro-emscripten`
- tag：`retrom-web-f2efc98-r3`
- tag commit：`6e75022c2d015a7af29a433014599cb56d6d2262`
- adapter ABI：`mkxp-state-v1`
- assets：`mkxp-z_libretro.js`、`mkxp-z_libretro.wasm`、`retrom-runtime-release.json`
- release：<https://github.com/xxxsen/mkxp-z-libretro-emscripten/releases/tag/retrom-web-f2efc98-r3>

该 tag 固定 mkxp-z 与 RetroArch commit，并在构建时应用 Retrom 的 Web/runtime、256 MiB state 和恢复
边界 patch。r3 还在 `retro_unserialize` 成功后、sandbox 瞬时为空的 lifecycle 窗口保护 movie-frame 查询，
避免恢复启动阶段因空 `boost::optional` 解引用而中断。XP/VX/VX Ace 复用同一 JS/Wasm bytes，但必须分别以
RGSS1/2/3 执行独立产品验收。

## 物化与机器门禁

`python3 data/dat/rpgmaker/v1/build.py data-check` 必须验证：

- Git 小文件、当前七条 selected route（EasyRPG 为 V4，mkxp 为 V5，MV Native Web 为 V4、MZ Native Web 为 V7）以及仍可启动的历史 route 与 Go/TS/frontend registry 双向一致；
- release repository 恰为上述两个允许值；
- tag 非浮动且 tag commit 为完整小写 SHA-1；
- asset URL 精确指向该 repository/tag/filename；
- metadata 文件声明同一 repository/tag/commit/adapter ABI；
- release 声明不包含 expected size/SHA 字段；
- source offer、patch、workflow 与许可证映射完整。

`prepare` 对固定 URL 执行有限重定向、硬大小上限、完整响应和原子 rename，并在 ignored runtime 根生成
`.release-assets-observed.json`。已有缓存只有在 observed 记录仍匹配本机 bytes 时才可复用。缺少或损坏资产
必须 fail closed，不能回退到 Pages、历史路径或本地 builder。

`deps-check` 逐文件复核 ignored observed 记录、runtime allowlist 和 source/license/notice 闭包。它只证明
当前本机物化结果完整，不证明远端 asset 永远不会变化，也不替代对应核心的 Retrom 产品验收。

## 发布与升级规则

1. patch 必须先进入上述 fork 的提交历史。
2. 每次变更创建新不可变 tag；失败 tag 保留，不移动、不覆盖。
3. workflow 构建并上传固定命名的 JS/Wasm 与 metadata；Retrom 不接受手工旁路资产。
4. Retrom manifest 在同一变更中切到新 tag/commit/URL、runtime version 和新 route。
5. clean-lineage 开发重建只登记当前七条 route；以后正式升级仍新增 immutable artifact/route，不原地覆盖
   已有 product save 或 Launch 的绑定。
6. runtime 资产可下载只解决供应链门禁。每个核心仍必须经 Retrom Upload/Review/Validation/Launch/Player
   完成 A→B→C、不同 restore Launch 精确恢复 B、恢复后输入和发布后普通 save 恢复，才能报告完成。

## 对应源码与许可

不要求 Retrom 本地复现 Release bytes，但必须保留 GPL 对应源码能力。source offer 应覆盖 tag 实际使用的
上游 commit、Retrom patch、release workflow/build scripts、静态链接依赖源码及其许可证，并把二进制关系如实
标记为 `TAGGED_RELEASE_COMPATIBLE`。未被 tag 构建消费的历史 recipe 或本地诊断产物不得描述为该 Release
的精确来源，也不得登记为产品 artifact。
