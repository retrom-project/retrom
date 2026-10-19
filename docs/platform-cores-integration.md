# 平台与核心接入

平台、公共 coreId、Provider Target、内容准备、BIOS／DAT、指纹和 checkpoint 能力由 retrom-runtime 拥有。Retrom 消费同一配套 catalog，不维护另一份机器清单、引擎分支或核心别名。

完整映射见 runtime 的[支持清单](https://github.com/retrom-project/retrom-runtime/blob/master/docs/supported-platforms-and-cores.md)，格式与运行前提见 [Target 指南](https://github.com/retrom-project/retrom-runtime/blob/master/docs/targets/README.md)，核心 fork 与候选流程见 [runtime 维护文档](https://github.com/retrom-project/retrom-runtime/blob/master/docs/maintaining-forks.md)。已部署环境的范围以其固定版本为准。

## Retrom 集成步骤

1. 在同一 [PFB](pfb-development.md) 准备 runtime 及必要核心 worktree；源码依赖新增或维护分支变化只修改本 PFB 的 `workspace/manifest.yaml`。
2. 在 runtime 完成声明、适配、公共资产及对应 Target 文档，生成同源工具和完整候选 Provider。新增核心不追加目录登记 migration，也不自动改动已有目录的允许集。
3. 在 Retrom 通过正常 API 创建或调整[游戏目录](platform-instance.md)，经来源扫描、审核／发布和真实输入验证产品链；支持存档的能力还需覆盖公开保存和新 Run 恢复，具体边界见[运行产品验证](core-runtime-validation.md)。缺失核心明确失败，不能静默回退。
4. 正式 runtime 发行后，经[标准 pin 与准备链](dependency-management.md)固定同次 host-tool 和两个 Provider，分别记录正式资产与镜像验证。候选相同源码不能替代正式资产运行证明。

API 及存储契约仍由 Retrom 拥有；仅增加运行能力时不应凭引擎名称增加宿主专属表或字段。完整检查标准见[项目验收](project-acceptance.md)。
