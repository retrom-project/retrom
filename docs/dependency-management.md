# 配套运行输入

`workspace/manifest.yaml`拥有源码仓库、维护 branch 和依赖边。构建、默认依赖准备和 CI 的唯一配套权威是版本控制的 `data/runtime-inputs.json`，包括实际 runtime 源码树摘要、两个 Provider 的 bundle 身份与分块清单，以及自足 host-tool 的归档名、字节数、SHA256。Provider 和工具必须来自同一次真实源码捕获，不能用版本字段推断配套关系。

## 正式依赖准备与升级

当前固定的 runtime Release、提交和摘要直接查阅 [data/runtime-inputs.json](../data/runtime-inputs.json)。完整平台／核心映射及公开存档能力由 runtime 的[支持清单](https://github.com/retrom-project/retrom-runtime/blob/master/docs/supported-platforms-and-cores.md)维护，具体格式与运行前提见 [Target 指南](https://github.com/retrom-project/retrom-runtime/blob/master/docs/targets/README.md)。已部署环境应核对自身配套版本。

runtime Release 只以内容寻址的分块发布 Provider：每个 Provider 一份分块清单 `<providerId>-chunks-<版本>.json` 和若干 `<providerId>-chunk-<chunkId>.tar.gz`，不发布 Provider 整包。`bundleSha256` 是 bundle 中 `integrity.json` 的 SHA256；`chunkId` 只由分块内文件的路径、大小和 SHA256 决定，内容不变的核心在不同版本中保持同一分块。

`make runtime-provider-pin-release TAG=<runtime-release-tag>` 先下载该发行的 `runtime-inputs.json`，验证 host-tool 归档和两个 Provider 的全部分块，再更新仓库 pin；验证过的分块留在共享缓存中。默认下载地址由描述中的真实 repository/tag 派生；`RETROM_RUNTIME_INPUT_BASE_URL` 可指定镜像源，只改变运输地址。后续依赖发布同样通过该入口固定，不手工替换摘要或覆盖旧标签。

```sh
make prepare-deps runtime-provider-prepare
```

准备结果位于 `.cache/runtime-inputs/<描述摘要>/`。分块统一存放在共享下载缓存 `.cache/runtime-providers/chunks/`（基线与所有 PFB 共用），已准备集合中的 Provider 文件是这些分块的硬链接，新的 pin 只下载内容变化的分块。`make build-images` 在缺少已准备路径时执行同一准备链。前后端使用同一 Retrom 标签。PR 验证两张分支镜像及独立 PostgreSQL/Redis 部署；tag 流水线构建并验证发行镜像；稳定版更新 `latest`，RC 不更新。

## 镜像启动时安装 Provider

后端镜像不含 Provider，只携带固定的 `/opt/retrom/runtime-inputs.json`（`RETROM_PROVIDER_INPUTS`）。后端在对外服务前把对应分块安装到数据卷 `/var/lib/retrom/providers/`（`RETROM_PROVIDER_ROOT`）：

- 首次启动或升级到新镜像时，只下载本地缺少的分块，逐块校验归档摘要并逐文件校验 SHA256，用硬链接组装 bundle，原子切换 `active.json`，最后写入完成标记 `ready.json`。进度写入日志。中断不会写出标记，再次启动从已完成的分块继续。
- 标记与镜像固定的 pin 一致时，只按 `integrity.json` 核对文件存在与大小，不下载、不逐文件计算哈希；发现缺失或截断只重新获取受影响的分块。`RETROM_PROVIDER_VERIFY=full` 强制逐文件计算哈希并修复。
- 镜像固定的 Provider 版本低于已安装版本时拒绝启动（`RUNTIME_PROVIDER_DOWNGRADE_FORBIDDEN`）；同版本不同 bundle 同样拒绝。不保留旧版本。
- 新版本生效后删除不再引用的 bundle 与分块；未变化的核心原样保留。
- 启动与安装不读取、不检查存档；存档能否恢复仍只在列表和恢复时按现有规则判断。

`providers/` 可再生，可不纳入备份。默认从 GitHub Release 下载；`RETROM_RUNTIME_INPUT_BASE_URL` 可指向镜像源或本地目录。无网络环境可先用 `go run ./cmd/retrom-providers sync --inputs <pin> --root <数据卷>/providers --mode production --seed <已验证的分块缓存或 Provider 根>`（或镜像内的 `retrom providers sync`）在主机准备数据卷，启动时检测到一致的标记即不再下载。

## 身份与校验

源码摘要复用 runtime 的 `scripts/provider-release.mjs:sourceTreeSha256`：Git 列出 tracked 与非 ignored untracked 文件，跳过实际已删除项，按 UTF8 路径排序；每项记录 path、Git mode 和实际内容 SHA256（symlink 记录链接目标字节），再对 canonical JSON 计算 SHA256。ignored 构建产物、node_modules 和验收缓存不进入该边界。构建描述、tool 的 host-tool.json、两个 Provider provenance 都必须与同次捕获相等。

分块的下载、校验、组装、标记与清理只有一套实现：`internal/providersync`，命令行为 `cmd/retrom-providers sync|verify|gc`（镜像内为 `retrom providers …`，同一代码）。后端启动直接调用它；准备脚本、PFB 候选安装、缓存导入和镜像验证通过同一命令完成，Python 不重复实现。分块清单按 pin 中的摘要校验，可来自镜像源、本地目录或已验证的本地副本；本地种子只有在逐文件 SHA256 与清单一致时才会被链接。

准备先校验 host-tool 归档的大小和 SHA256，再解包无链接的自足工具，并由上述命令安装 Provider。工具实际路径、内容、大小及可执行位逐项匹配认证归档；Provider 的 `integrity.json` 摘要必须等于 pin 中的 `bundleSha256`，再校验全部文件、manifest、闭包、module、来源和 Target 指纹。完整集合原子发布，重复准备仍重新校验。损坏或来源错配不会发布集合。

`make runtime-provider-prepare` 与 `scripts/prepare_image_inputs.py` 执行同一准备链。镜像构建前后计算配套输入摘要；维护 branch、源码摘要、pin 摘要、实际 module 和 Target 指纹分别记录，不能互相冒充。DAT 文件的发行身份只描述事实素材来源，不选择当前运行 Provider；依赖导出不再携带另一份 tag-only 运行 pin。

## 开发候选

日常 PFB 使用标准 watcher 与显式已验证基座。未发布的候选描述明确 `release:null`，使用 `RETROM_RUNTIME_INPUT_ARCHIVE_ROOT` 指向 runtime 候选构建的 `release/` 目录，或 `RETROM_RUNTIME_INPUT_BASE_URL` 指向 HTTPS（或本机回环 HTTP）运输根；其下是 host-tool 归档、两份分块清单与全部分块的 basename。`make runtime-provider-prepare-candidate RETROM_PROVIDER_CANDIDATE_ROOT=<release 目录>` 在暂存目录组装并通过升级规则后才替换当前开发 Provider，被拒绝的候选不改变现有安装。运输地址不决定身份，所有摘要仍由选定描述决定。

候选材料只用于对应开发验收，不冒充已发布依赖。没有输入时明确失败，不下载历史版本作为默认替代。初次认证后，完整准备结果及共享分块缓存支持无运输输入的重复准备，不依赖原 PFB、runtime 源码路径或其 node_modules。`make runtime-provider-cache-import SOURCE_ROOT=<已准备集合>` 把其中的 host-tool 归档与全部分块离线导入共享缓存。

逐核心产品验证要求见[运行产品验证边界](core-runtime-validation.md)，完整检查标准见[项目验收](project-acceptance.md)。执行结果随对应 CI、PR、Release 或本地报告提供，发布版本不表示全部 ROM 或存档场景已经验证。
