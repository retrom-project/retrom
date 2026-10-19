# 实施与交付

本文说明变更从实施到评审、验证和发布的交付流程。

1. 从[文档索引](README.md)确认模块职责、API 与 runtime 的事实源，在命名 [PFB](pfb-development.md) 中实施。
2. 按影响范围更新实现、契约、生成产物和正式文档。平台、核心及运行规则由 runtime 维护；Retrom 维护 Host 调用、受管存储、权限和产品流程。
3. 执行[质量与测试](engineering-quality-and-testing.md)要求的检查；涉及真实运行、输入或存档时补充受影响的产品链证据。
4. 按[项目验收标准](project-acceptance.md)记录实际覆盖、失败、未执行及不适用项，关联最终源码、工具和 Provider 身份。候选、正式资产和镜像分别验证。
5. 提交评审前确认改动范围、依赖 pin、文档和验证输入一致。发布流程见[配套运行输入](dependency-management.md)，部署与升级见[部署指南](docker-deployment.md)。

完整交付以实际要求和证据为准。工程门禁、一个样本或一份候选包通过，都不能替代全项目或全核心的验收结论。执行结果随对应 CI、PR、Release 或本地报告提供，正式文档维护当前规范和方法。
