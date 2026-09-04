# EmulatorJS 4.2.3 Arcade DAT 基线

| 属性 | 内容 |
| --- | --- |
| 状态 | 来源与最终字节已校验；payload 按需物化 |
| 基线版本 | EmulatorJS `v4.2.3` |
| 数据目录 | `data/dat/emulatorjs/4.2.3/` |
| 适用核心 | `fbneo`、`mame2003`、`mame2003_plus`、`fbalpha2012_cps1`、`fbalpha2012_cps2` |
| 用途 | Arcade machine、ROM entry、parent、BIOS/base archive 依赖识别 |
| 非用途 | 游戏封面、简介等展示元信息刮削 |

## 1. 为什么按 EmulatorJS 版本绑定

Arcade DAT 描述的是某个具体核心构建所接受的 machine 和 ROM 内容，不是仅按核心名字长期有效的静态字典。即使都叫 `fbneo` 或 `mame2003_plus`，核心更新也可能增删 machine、修改 ROM hash 或改变 parent/BIOS 关系。

因此 Retrom 的兼容性主键应是：

~~~mermaid
flowchart LR
    Release["EmulatorJS release + 发布包 SHA-256"] --> Artifact["Core artifact + SHA-256"]
    Artifact --> Source["Core source commit + 关联证据"]
    Source --> Dat["DAT bytes + SHA-256"]
    Dat --> Snapshot["解析器版本 + 依赖快照"]
    Snapshot --> Variant["GameVariant 锁定版本"]
~~~

只保存 `core_id + dat_version` 不足以复现运行环境；数据库中的 `dat_versions` 还必须关联 EmulatorJS version 和实际 core artifact hash。

## 2. 官方发布物证据

当前方案锁定 [EmulatorJS v4.2.3 release](https://github.com/EmulatorJS/EmulatorJS/releases/tag/v4.2.3)：

- Git tag commit：`e150dc0491ae747028919fb82d6598954976ede6`。
- 发布资产：`4.2.3.7z`，303,554,683 bytes。
- 官方资产 SHA-256：`07d451bc06fa3ad04ab30d9b94eb63ac34ad0babee52d60357b002bde8f3850b`。
- 本地下载后重新计算的 SHA-256 与官方 digest 一致。

发布包内 build report 给出的构建窗口：

| Core | buildStart | buildEnd | 官方报告 |
| --- | --- | --- | --- |
| `fbneo` | 2025-06-14 18:50:28 UTC | 18:54:56 UTC | [fbneo.json](https://cdn.emulatorjs.org/4.2.3/data/cores/reports/fbneo.json) |
| `mame2003` | 2025-06-14 19:06:32 UTC | 19:09:40 UTC | [mame2003.json](https://cdn.emulatorjs.org/4.2.3/data/cores/reports/mame2003.json) |
| `mame2003_plus` | 2025-06-14 19:09:40 UTC | 19:14:42 UTC | [mame2003_plus.json](https://cdn.emulatorjs.org/4.2.3/data/cores/reports/mame2003_plus.json) |
| `fbalpha2012_cps1` | 2025-06-14 18:23:15 UTC | 18:24:26 UTC | [fbalpha2012_cps1.json](https://cdn.emulatorjs.org/4.2.3/data/cores/reports/fbalpha2012_cps1.json) |
| `fbalpha2012_cps2` | 2025-06-14 18:24:26 UTC | 18:25:39 UTC | [fbalpha2012_cps2.json](https://cdn.emulatorjs.org/4.2.3/data/cores/reports/fbalpha2012_cps2.json) |

所有 normal、legacy、thread 和 thread-legacy core artifact 的大小及 SHA-256 均已写入 [`manifest.json`](../data/dat/emulatorjs/4.2.3/manifest.json)。运行时部署核心时必须命中 manifest 中的发布 artifact 或显式 `tested_runtime_override`，否则这套 DAT 只能标记为“兼容性未知”，不能自动作为活动基线。当前 `mame2003` 4.2.3 bundle 有已复现的启动回归，实际验证使用的官方 4.2.1 覆盖及 DAT 同一性证据见[核心运行时验证基线](./core-runtime-validation.md)。

## 3. 核心源码关联的证据等级

EmulatorJS `v4.2.3` 的 build report 没有声明 core source commit。其官方 [build repository](https://github.com/EmulatorJS/build) 在当时的脚本会直接 clone/pull `cores.json` 指向的 EmulatorJS core fork；FBNeo 与 MAME2003-Plus 据各自 `buildStart` 之前的最后提交推定。MAME2003 则在真实调试启动日志中输出了嵌入的 Git version，证据等级更高：

| Core | EmulatorJS core fork | 绑定提交 | 状态 |
| --- | --- | --- | --- |
| `fbneo` | [EmulatorJS/FBNeo](https://github.com/EmulatorJS/FBNeo) | `c8c70ba4858458c44b7200b88a29ebbb48c9bb23` | 按官方构建时间推定 |
| `mame2003` | [EmulatorJS/mame2003-libretro](https://github.com/EmulatorJS/mame2003-libretro) | `0ee1c0e4daf9fb7d96119ebf4627ee5d9af2c312` | 官方 core 运行日志内嵌 Git version |
| `mame2003_plus` | [EmulatorJS/mame2003-plus-libretro](https://github.com/EmulatorJS/mame2003-plus-libretro) | `09e84fe55799031e225b9da5e526d82ee85b9cd8` | 按官方构建时间推定 |
| `fbalpha2012_cps1` | [EmulatorJS/fbalpha2012_cps1](https://github.com/EmulatorJS/fbalpha2012_cps1) | `d6f5267a3abe8b079ed31d2ab3521911e6498372` | 按官方构建时间推定 |
| `fbalpha2012_cps2` | [EmulatorJS/fbalpha2012_cps2](https://github.com/EmulatorJS/fbalpha2012_cps2) | `3fb5b89d2ab719e45e814e1ad0b5ff721bffdff2` | 按官方构建时间推定 |

manifest 为每个核心单独保存 `association_status`。FBNeo 与 MAME2003-Plus 仍不得在 UI 或审计记录中改写为“官方精确提交”；MAME2003 可以说明为“core 运行日志内嵌版本”，但不能误写成 build report 声明。如果未来发布物提供可验证 SBOM，应以更强证据替换现状。

## 4. 可重复物化的真实 DAT

| Core | 文件 | 大小 | SHA-256 | machine | ROM entry |
| --- | --- | ---: | --- | ---: | ---: |
| `fbneo` | `fbneo/fbneo-arcade.dat` | 13,330,353 | `99605be7351a04eb619fe1555483a00229c89e74db7657173d30fde666d73637` | 7,980 | 155,706 |
| `mame2003` | `mame2003/mame2003.xml` | 19,304,927 | `dacf9d5739ddf386705bc703f7f70239ac61b8ab44c438f76f6658c7156da147` | 4,727 | 70,951 |
| `mame2003_plus` | `mame2003_plus/mame2003-plus.xml` | 21,961,641 | `e1107634c5847b825d6769e19e38cee7581f2faedd213c2457580aa6a7d7303c` | 5,257 | 80,829 |
| `fbalpha2012_cps1` | `fbalpha2012_cps1/fbalpha2012-cps1.dat` | 370,682 | `54a95deb406fefbf4522498ee55a4d644724c3abc017d744538678204015b789` | 227 | 5,355 |
| `fbalpha2012_cps2` | `fbalpha2012_cps2/fbalpha2012-cps2.dat` | 392,280 | `f59166dbd123d998ae874910275e5a0ab09b7aca83dde621f2c5a53af0407f99` | 284 | 5,047 |

最终 bytes 已在本次基线建立时从绑定源生成并完成统计；Git 不保存 DAT payload。`make prepare-deps` 按 manifest 的配方物化到被忽略的上表路径，`make deps-check` 验证最终 size/hash/stats。来源处理：

- `fbneo`：目标提交当时没有预提交的 Arcade DAT。权威审计路径是在该源码提交上执行 release SDL2 原生构建（`RELEASEBUILD=1`）并调用 `fbneo -dat`。日常物化无需重复编译：下载 libretro/FBNeo 固定提交 `7024859…` 的公开生成快照（13,330,371 bytes，SHA-256 `734fe466…40597`），再执行 manifest 中两项带 expected count 的字节替换；所得 13,330,353 bytes 与直接源码生成结果逐字节相同，最终 SHA-256 为 `99605be7…d73637`。任一计数或最终 hash 不同即失败。
- `mame2003`：直接保存目标提交的 [metadata/mame2003.xml](https://github.com/EmulatorJS/mame2003-libretro/blob/0ee1c0e4daf9fb7d96119ebf4627ee5d9af2c312/metadata/mame2003.xml)。本地计算的 Git blob SHA-1 与 GitHub 内容 API 的 `402017d7798acb5da9e89c446a4fa6914abe7491` 一致；验证覆盖提交 `a5c2098…` 中该文件的 SHA-256 也完全相同。
- `mame2003_plus`：直接保存目标提交的 [metadata/mame2003-plus.xml](https://github.com/EmulatorJS/mame2003-plus-libretro/blob/09e84fe55799031e225b9da5e526d82ee85b9cd8/metadata/mame2003-plus.xml)。本地 Git blob SHA-1 与上游 `0ee9922294fcf89ce10fca72b009bab6f9da0628` 一致。
- `fbalpha2012_cps1` / `fbalpha2012_cps2`：manifest 分别锁定源码 archive 的 URL、root、size 与 SHA-256。物化器安全展开后构建仓库内原生枚举器，调用 core 的生产 driver registry 生成 Logiqx XML，并在两个全新临时目录重复完整生成；仅当两次 bytes 相同且命中最终 size/hash/stats 才原子发布。CPS-1 的真实 `nBurnDrvCount` 是 227，不能从过时的 `gamelist.txt` 标题推断 244；CPS-2 只允许已锁定的 `mmancp2u -> megaman` 集合外 parent 规范化，漂移即失败。

## 5. 真实数据暴露出的解析要求

### 5.1 FBNeo

- 根元素是 `datafile`，machine 元素名是 `game`。
- 13 个 BIOS machine 通过 `isbios="yes"` 显式标记。
- 155,706 条 ROM 中 86,944 条带 `merge`，1,288 条为 `nodump`；其余 entry 未写 status，解析时规范为 GOOD。没有 MAME `biosset`/ROM `bios` 属性。
- 全部 155,706 条 ROM 都没有 SHA-1；恰好 1,288 条 NODUMP 同时没有 CRC32，所有非 NODUMP 条目都有 CRC32。parser 和表结构必须保留真实 NULL，不得为满足 NOT NULL 伪造 hash。
- 当前文件的 5,470 条 `cloneof` 与 5,841 条 `romof` 目标都可在同一 DAT 中解析。
- 文件声明外部 DTD；解析时必须禁用 DTD、外部实体和网络访问。

### 5.2 MAME 2003 与 MAME 2003-Plus

- 根元素是 `mame`，machine 元素名也是 `game`。
- 旧 List XML 没有 `isbios` 属性，`explicit_bios_machine_count = 0` 不表示没有 BIOS。
- MAME 2003 有 24,274 条 merge、1,408 条带 bios 的 ROM、1,427 个 biosset（213 个 default）、148 个 nodump 和 177 个 baddump；MAME 2003-Plus 对应为 29,580、3,435、3,462（268 个 default）、92 和 140。以上真实统计已进入 manifest，解析器不能把所有 bios option 同时当作必需 ROM。
- MAME 2003 缺 CRC32/SHA-1/两种 hash 的 ROM 计数分别为 148/154/148；MAME 2003-Plus 为 92/193/92。同时缺两种 hash 的都是 NODUMP，非 NODUMP 至少有一种 hash；两份各 30 条 disk 都有 SHA-1。这些计数也是当前 manifest schema V7 的机器基线。
- `cloneof` 表示 parent；当 `romof` 与 `cloneof` 不同，`romof` 目标是应加载的 BIOS/base archive。clone 还需沿 parent 链继续解析父项依赖。
- 两份真实 XML 都有 17 个 BIOS/base dependency target。
- 两份文件都包含 `brvblade`、`beastrzr` 指向未定义 `psarc95` 的关系。解析结果保留 unresolved warning；不得补 mock machine，也不得因两条悬空关系拒绝其余数千条有效记录。

### 5.3 FB Alpha 2012 CPS-1 与 CPS-2

- 两份文件都是仓库生成器输出的 Logiqx `datafile/game`，声明名分别精确为 `fbalpha2012_cps1`、`fbalpha2012_cps2`，版本均为 `0.2.97.29`。
- CPS-1 为 227 machines、5,355 ROM、1,554 merge、56 NODUMP、190 clone/romof；CPS-2 为 284 machines、5,047 ROM、3,275 merge、1 NODUMP、243 clone/romof。两者全部 ROM 都没有 SHA-1，非 NODUMP ROM 均有 CRC32。
- 两份 DAT 都没有 explicit BIOS machine、biosset、ROM bios 属性或 base dependency target，因此不会生成 `DAT_MACHINE` BIOS Requirement；这不允许把空依赖结论推广到其他 FBA/FBNeo core。
- 后端与前端使用同一封闭的五 family 集合。每份 FBA2012 DAT 只接受自己的声明名与目标 Provider Target；跨家族上传、导入或 Launch 必须拒绝，不能回退 FBNeo family。

因此 BIOS 管理页面的数据模型固定使用统一的 `dependency_kind = BIOS_OR_BASE_ARCHIVE`，并保留 `classification_source = EXPLICIT_ISBIOS | ROMOF_INFERENCE`。UI 可以展示为 BIOS 依赖，但诊断详情必须能说明推导依据。

## 6. 服务启动与运行时规则

1. Git 只跟踪 manifest/校验表；本地在 `make dev` 前执行 `make prepare-deps`，镜像在 dependency builder stage 执行同一固定来源配方。禁止浮动版本。
2. 应用同步启动阶段只验证 manifest schema、DAT size 和 SHA-256，不联网；再创建或复用 `PENDING/READY` 的 `dat_versions` 及唯一 `DAT_PARSE` Job，绝不把未解析版本标为 active。
3. 同时验证实际部署的 EmulatorJS version 及 core artifact SHA-256；未命中 manifest 的 core 不自动绑定内置 DAT。
4. Worker 以 `provider_id + target_id + bundle_sha256 + dat_sha256 + parser_version` 复用解析结果；冷库在后台解析期间 live 但不 ready，成功发布索引后由启动引导激活 manifest 指定的精确版本，并停用同 artifact 的其他 BUILTIN 版本。原始 DAT 不在每次启动游戏时重新解析；确定性失败使当前 enabled artifact 保持 `DEPENDENCY_DAT_PARSE_FAILED`，不得用空索引进入 ready。
5. `GameVariant` 保存当前 core、稳定 Provider/Target 与 DAT 选择；release manifest 的 DAT 选择变化只触发受影响 Variant 重校验并原子更新当前态。已创建 Launch 继续使用创建时冻结的 Bundle、DAT/依赖摘要与物化资源，不被后台改写。
6. 运行时只接受 manifest 固定并通过 SHA-256 校验的内置 DAT；没有用户上传、兼容性确认或候选版本分支。

## 7. 统一升级验收入口

基础完整性与安全解析执行 [一期项目验收规范](./project-acceptance.md) 的 `ACC-DAT-001`、`ACC-DAT-002` 和 `ACC-DAT-005`。EmulatorJS、core artifact 或预置 DAT 发生版本变化时，必须额外执行条件 Case `ACC-DAT-006` 及其引用的逐核心、依赖和存档兼容 Case；流程、通过标准和回滚证据只在统一文档维护。
