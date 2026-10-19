# 运行产品验证边界

平台／核心对应关系、Provider Target 和公开存档能力由 runtime 的[支持清单](https://github.com/retrom-project/retrom-runtime/blob/master/docs/supported-platforms-and-cores.md)维护，内容格式与运行前提见 [Target 指南](https://github.com/retrom-project/retrom-runtime/blob/master/docs/targets/README.md)。已部署环境以配套版本为准；声明清单与逐核心实际验收是不同的记录。

Retrom 验证必须通过正常来源扫描、审核／发布、Run、受限资源和浏览器输入／checkpoint 链。一个 Target 存在、module 加载、ready 200 或出现标题图标都不能当作产品 PASS。

记录实际 coreId/providerId/targetId、源码／工具／Provider 摘要、核心指纹、合法素材的受控标识、画面或状态变化、输入、payload 保存与新实例恢复，以及故障、不适用或未执行的原因。支持 checkpoint 的 Target 必须验证退出后新 Run 恢复；GAME_SAVE 还需确认游戏内实际保存与读取。隔离 Web 项目应覆盖 SW／MessagePort／Range 及原生导航、import、Worker、media。

通用浏览器回归入口见[质量与测试](engineering-quality-and-testing.md)，完整追踪标准见[项目验收](project-acceptance.md)。结果随对应 CI、PR、Release 或本地报告提供，并绑定实际输入与覆盖范围。素材不足不能替换为假 ROM、假 HTTP payload 或假成功；局部通过不表示全部核心或作品兼容。
