# 配套运行输入

`workspace/manifest.yaml`拥有源码仓库、维护branch和依赖边。构建、默认依赖准备和CI的唯一配套权威是版本控制的`data/runtime-inputs.json`，包括实际runtime源码树摘要、两个完整Provider构建记录和自足host-tool的归档名、字节数、SHA256。Provider和工具必须来自同一次真实源码捕获，不能用版本字段推断配套关系。

源码摘要复用runtime的`scripts/provider-release.mjs:sourceTreeSha256`：Git列出tracked与非ignored untracked文件，跳过实际已删除项，按UTF8路径排序；每项记录path、Git mode和实际内容SHA256（symlink记录链接目标字节），再对canonical JSON计算SHA256。ignored构建产物、node_modules和验收缓存不进入该边界。构建描述、tool host-tool.json、两个Provider provenance都必须与同次捕获相等。

`make runtime-provider-prepare`及`scripts/prepare_image_inputs.py`执行同一条准备链。未发布的描述明确`release:null`，使用`RETROM_RUNTIME_INPUT_ARCHIVE_ROOT`指定本地归档目录，或`RETROM_RUNTIME_INPUT_BASE_URL`指定HTTPS运输根。目录/地址下固定为两个Provider与一个host-tool的三个basename；Provider记录中的`providerId/archive`是正式BuildRecord标识，不是另一套查找路径。运输地址不决定身份，CI只有运输根变量，全部摘要仍由仓库描述决定。未提供输入时明确失败，不下载历史版本作为默认替代。

准备先校验三个原始归档的大小和SHA256，再解包无链接的自足npm工具和已验证Provider。工具实际路径、内容、大小和可执行位逐项匹配认证归档；Provider本地integrity必须逐字节匹配认证归档中的integrity，再校验所有文件、manifest、闭包、module、来源和Target指纹。完整集合原子发布到`.cache/runtime-inputs/<描述摘要>/`，重复准备仍重新校验。共享下载按单文件锁复用，环境安装与active不共享。损坏或来源错配不会发布集合。

本次 R12 描述来自runtime本地提交 `94f4d8627ae8f267682c145245ce47935e955bff` 的干净源码，实际源码树摘要 `2a8080b11683f2e4e1b708815e71e944319e66252a87af79aad60794b0f71903`。本次重建补齐J2ME手柄手机键映射，并交付只读内容BIOS要求和DOS包内程序候选查询；两个Provider的671项`assets/`文件与原核心输入逐项保持相同。相对R11仅J2ME执行指纹改变，其余109个Target保持原指纹，DOS查询属于host-tool能力而非浏览器执行变化。ONS输入在该仓库的Provider来源描述中独立固定；Retrom不隐式下载旧ONS或自行选择核心构建。候选版本字段仍为0.59.1，`release:null` 与归档摘要明确区分它和历史正式v0.59.1。

拿到描述对应的三份归档后，可以在全新checkout执行：

```sh
export RETROM_RUNTIME_INPUT_ARCHIVE_ROOT=/path/to/three-pinned-archives
make prepare-deps runtime-provider-prepare
make build-images
```

归档可复制到任意目录；无需原PFB、runtime源码路径或其node_modules。初次认证完成后，完整准备结果和认证缓存支持无运输输入的重复准备。描述本身不含本机路径或未经发布的虚构下载URL。

`make build-images`在缺少已准备路径时执行同一准备链；显式context也必须属于该pin认证的完整集合。镜像前后计算配套输入摘要，独立Postgres/Redis启动验证先于发布。日常PFB仍使用标准watcher与显式已验证基座，维护branch、本次源码摘要、归档摘要、实际module和Target指纹分别记录，不能互相冒充。

DAT文件的原始发行身份只描述事实素材的来源，不选择当前运行Provider；依赖导出不再携带另一份tag-only运行pin。

未来正式发行仍需要真实immutable core发行、runtime annotated tag和同一release产出的`runtime-inputs.json`及三份归档。`make runtime-provider-pin-release TAG=<真实新tag>`下载并核验完整集合后才更新唯一pin；正式描述的默认运输根由其真实repository/tag派生。本次没有创建tag、上传归档或发布release，也不宣称未配置运输根的远端CI已经通过。历史v0.59.1归档不会被覆盖。
