# 配套运行输入

`workspace/manifest.yaml`拥有源码仓库、维护 branch 和依赖边。构建、默认依赖准备和 CI 的唯一配套权威是版本控制的 `data/runtime-inputs.json`，包括实际 runtime 源码树摘要、两个完整 Provider 构建记录和自足 host-tool 的归档名、字节数、SHA256。Provider 和工具必须来自同一次真实源码捕获，不能用版本字段推断配套关系。

## 当前 RC

当前 pin 使用 [retrom-runtime v0.60.0-rc.1](https://github.com/retrom-project/retrom-runtime/releases/tag/v0.60.0-rc.1)。这是不可移动标签上的预发布，配套 Retrom v0.0.111-rc.1；不兼容旧版数据库、存档和接口。所有 core 输入继续固定各自已发布的 tag/commit，Retrom 不自行选择或编译核心。

`make runtime-provider-pin-release TAG=v0.60.0-rc.1` 先下载该发行的 `runtime-inputs.json`，验证同一发行的两份 Provider 和一份 host-tool 原始归档，再更新仓库 pin。默认下载地址由描述中的真实 repository/tag 派生，无需配置临时运输地址。后续依赖发布同样通过该入口固定，不手工替换摘要或覆盖旧标签。

```sh
make prepare-deps runtime-provider-prepare
```

准备结果位于 `.cache/runtime-inputs/<描述摘要>/`。`make build-images` 在缺少已准备路径时执行同一准备链；显式 context 也必须属于该 pin 认证的完整集合。前后端使用同一 Retrom 标签。PR 验证两张分支镜像及独立 PostgreSQL/Redis 部署；tag 流水线构建并验证发行镜像，RC 不更新 `latest`。

## 身份与校验

源码摘要复用 runtime 的 `scripts/provider-release.mjs:sourceTreeSha256`：Git 列出 tracked 与非 ignored untracked 文件，跳过实际已删除项，按 UTF8 路径排序；每项记录 path、Git mode 和实际内容 SHA256（symlink 记录链接目标字节），再对 canonical JSON 计算 SHA256。ignored 构建产物、node_modules 和验收缓存不进入该边界。构建描述、tool 的 host-tool.json、两个 Provider provenance 都必须与同次捕获相等。

准备先校验三个原始归档的大小和 SHA256，再解包无链接的自足工具和 Provider。工具实际路径、内容、大小及可执行位逐项匹配认证归档；Provider 本地 integrity 必须逐字节匹配认证归档中的 integrity，再校验全部文件、manifest、闭包、module、来源和 Target 指纹。完整集合原子发布，重复准备仍重新校验。共享下载按单文件锁复用，环境安装与 active 不共享。损坏或来源错配不会发布集合。

`make runtime-provider-prepare` 与 `scripts/prepare_image_inputs.py` 执行同一准备链。镜像构建前后计算配套输入摘要；维护 branch、源码摘要、归档摘要、实际 module 和 Target 指纹分别记录，不能互相冒充。DAT 文件的发行身份只描述事实素材来源，不选择当前运行 Provider；依赖导出不再携带另一份 tag-only 运行 pin。

## 开发候选

日常 PFB 使用标准 watcher 与显式已验证基座。未发布的候选描述明确 `release:null`，使用 `RETROM_RUNTIME_INPUT_ARCHIVE_ROOT` 指向本地归档目录，或 `RETROM_RUNTIME_INPUT_BASE_URL` 指向 HTTPS 运输根。其下固定为两个 Provider 和一个 host-tool 的三个 basename；Provider 记录中的 `providerId/archive` 是 BuildRecord 标识。运输地址不决定身份，所有摘要仍由选定描述决定。

候选材料只用于对应开发验收，不冒充已发布依赖。没有输入时明确失败，不下载历史版本作为默认替代。初次认证后，完整准备结果及认证缓存支持无运输输入的重复准备，不依赖原 PFB、runtime 源码路径或其 node_modules。

实际逐核心验证范围与已知问题见 [项目验收](project-acceptance.md)。发布 RC 不表示全部 ROM 或存档场景已经验证。
