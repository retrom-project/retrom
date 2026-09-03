# Arcade DAT 基线

本目录随代码维护五份真实 Arcade DAT 的小型来源 manifest、物化配方、SHA-256 和统计，不提交 DAT payload，也不包含 ROM/BIOS。每份 DAT 精确绑定 EmulatorJS Provider 的 `providerId/targetId/targetContractSha256`；升级 Provider 或改变 Target contract 时必须重新确认绑定关系，不能沿用“最新版 DAT”。

## 文件映射

| EmulatorJS | Core | DAT | 来源方式 |
| --- | --- | --- | --- |
| `v4.2.3` | `fbneo` | `emulatorjs/4.2.3/fbneo/fbneo-arcade.dat` | 权威来源是绑定提交的 release build + `fbneo -dat`；日常物化按 manifest 下载固定快照并执行两项计数替换，最终 bytes 与权威生成结果相同 |
| `v4.2.3` | `mame2003` | `emulatorjs/4.2.3/mame2003/mame2003.xml` | 构建时点对应的 EmulatorJS/mame2003-libretro 提交内置 XML |
| `v4.2.3` | `mame2003_plus` | `emulatorjs/4.2.3/mame2003_plus/mame2003-plus.xml` | 构建时点对应的 EmulatorJS/mame2003-plus-libretro 提交内置 XML |
| `v4.2.3` | `fbalpha2012_cps1` | `emulatorjs/4.2.3/fbalpha2012_cps1/fbalpha2012-cps1.dat` | 校验锁定源码 archive 后原生构建并枚举 227 个生产 driver；两次干净生成必须逐字节相同 |
| `v4.2.3` | `fbalpha2012_cps2` | `emulatorjs/4.2.3/fbalpha2012_cps2/fbalpha2012-cps2.dat` | 校验锁定源码 archive 后原生构建并枚举 284 个生产 driver；仅规范化 manifest 明列的一个集合外 parent |

每个 manifest 项只声明 DAT 来源、目标 `providerId/targetId/targetContractSha256`、parser 版本与确定性统计。Target 的入口、文件集合、能力和 checkpoint contract 只来自已激活 Provider Bundle；DAT 不复制这些字段。`data-check` 和 Go 启动校验都会独立确认目标存在、contract digest 完全匹配、DAT size/hash 与统计闭合。Provider 只向前升级；历史 Variant/Launch/Save 保留冻结的 Target contract，不允许把新字节覆盖到旧 identity。

```bash
make data-check     # 无 payload、无网络也可运行，只校验 Git 小文件
make prepare-deps   # 按固定来源物化/校验 payload
make deps-check     # 完全离线校验本地 bytes 与解析统计
```

应用同步启动预检只执行等价于 `deps-check` 的本地校验，不下载；缺少解析缓存时由服务 Worker 从这些已校验 bytes 建数据库索引。直接在未物化目录运行 `sha256sum --check SHA256SUMS` 会因缺文件失败，不是仓库健康检查入口。

## 重要解析差异

- FBNeo DAT 使用 Logiqx `datafile` 根结构，并显式提供 `isbios="yes"`。当前 release 基线有 13 个显式 BIOS machine。
- MAME 2003 与 MAME 2003-Plus 使用旧式 MAME List XML，根结构为 `mame`，没有 `isbios` 属性。不能把“没有 `isbios`”解释为“不需要 BIOS”。解析器应把 `cloneof` 作为 parent 关系，把 `romof != cloneof` 的目标作为 BIOS/base archive 依赖，并沿 parent 链继续解析。
- 两份 MAME XML 还真实包含 `biosset default` 与 ROM `bios` 属性；解析器只能把每个 machine 的唯一 default bios option 纳入必需闭包，不能要求所有地区/版本 BIOS。manifest 同时锁定 merge/biosset/bios/nodump/baddump 统计，避免只核对 machine 总数却漏掉依赖语义。
- 两份 MAME XML 都真实包含两个指向未定义 `psarc95` 的 `romof` 关系。这是上游数据特征，应记录为可诊断的 unresolved dependency，不能伪造一个 machine，也不应导致整份 DAT 导入失败。
- 三份既有文件声明 DOCTYPE：FBNeo 是外部 PUBLIC DTD，MAME 是内部结构声明；两份原生生成的 FBA2012 Logiqx XML 不依赖外部 DTD。运行时按 BIOS/DAT 专题的有界 scanner 跳过允许的声明，绝不解析 DTD、实体或访问网络；本目录校验也只读取 XML 元素，不获取任何 DTD URL。
- FBA2012 CPS-1/CPS-2 均没有显式 BIOS machine、biosset 或 base dependency target，解析统计分别为 227/284 machines、5,355/5,047 ROM entries；不能因为同属 FBA 系列而复用 FBNeo family 或 DAT。
- DAT 只用于 machine、ROM entry、parent 与 BIOS/base 依赖识别，不是游戏展示元信息刮削源。

## 更新规则

1. 先锁定更高版本的 EmulatorJS Provider Bundle，并取得目标 Arcade Target 的新 contract digest；不允许降级或同版本换字节。
2. 从 Provider Release provenance 和上游证据确认对应 DAT 来源，但不把 Provider 私有 adapter/core/asset 映射复制到 Retrom。
3. 定位 DAT 对应的上游源码提交。若发布证据未明示提交号，必须像当前 manifest 一样标记为“按构建时间推定”，不得写成官方明示值。
4. 优先使用该源码提交内置的 DAT；没有预生成 DAT 时，只能用同一提交的官方生成器生成。
5. 校验 XML 可解析、关系目标、统计值、文件大小与 SHA-256，再新增版本目录并更新 `manifest.json` 和 `SHA256SUMS`，不得覆盖旧目录。
6. DAT 是内置发布输入；管理后台、HTTP API 和数据库都不提供用户上传、切换、回滚或删除 DAT 的分支。

更完整的证据链和后端约束见 [`docs/arcade-dat-baseline.md`](../../docs/arcade-dat-baseline.md) 与 [`docs/dependency-management.md`](../../docs/dependency-management.md)。
