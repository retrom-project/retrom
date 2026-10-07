# PFB 开发

命名工作树按workspace/manifest.yaml的最新远端defaultBranch创建，Retrom和runtime保持同树，branch使用codex/前缀。基线project/只作参考，不通过旧路径参与构建；其他PFB不得停止/清理。

当前PFB每个环境独立拥有.pfb/workspace/data、postgres、providers、缓存及验收来源。server-data.json配置仅本PFB的只读素材挂载；管理员从 `/` 浏览容器实际可见目录，以绝对路径选择来源。up/restart只复用dev image、依赖和不可变runtime工具；显式pfb-build才npm构建并原子发布runtime-tools/<sha>，运行进程固定resolved路径，不因runtime源码clean丢失CLI。

Provider基座必须显式pfb-provider-import，核验完整bytes/size/integrity/proof；来源只读，不执行另一PFB源码。候选同版本重建的例外仅限显式PFB candidate导入，正式production升级门禁保留。watcher生成scope内loose overrides和实际Target指纹，restart加载事实。日常命令不自动构建Core或Provider archive。

reset使用exact ID停止本PFB，归档data与postgres到workspace/reset-backups/<timestamp>，保留Provider、依赖、构建缓存、ID与URL。完整清理只能用工作区pfb-remove的clean检查和交互边界，不能rm其他worktree/共享缓存。状态先make pfb-list，不能从目录或本地registry猜。

隔离桥唯一/__retrom/runtime-isolation/传输路径复用共享网关允许的前缀。此规则保全其他运行环境，不增加本Go旧接口别名，也不改动全局网关契约。

联合验收可在明确pfb-build时传RETROM_RUNTIME_TOOL_INPUT=<本PFB中的自足工具包目录>；它复制全部production依赖到不可变SHA快照，避免读取另一个owner正在构建的dist。未传时显式build才编译本PFB runtime并发布开发快照；每日up/restart只读已发布工具。工具输入不允许越出本命名PFB。

正式pfb-build拒绝正在运行的当前app，先协调停止该app再构建；不得手写toolchain marker绕过检查。标准pfb-up运行源码go run、Next dev和正常Provider watcher，并非固定生产binary或standalone。默认watcher仅覆盖retrom-runtime；dev/provider-id显式选择emulatorjs时才覆盖该Provider。开发client的assetIndex与完整基座可不同，需比较实际Target指纹及资产，不能按client整包SHA推断存档失效。生产镜像与日常开发来源分别记录。切换保留本PFB PG/Redis及workspace；Redis临时键可正常TTL到期，前后对照必须按实际变化记录。
