# BBK RPG 运行接入

BBK RPG 沿用统一 content/config、资源计划和 Target 适配。入口、格式识别、输入及存档语义归 runtime，Host 不增加引擎专属 Go 装配或数据库字段。

平台／核心映射及公开存档能力从 runtime 的[支持清单](https://github.com/retrom-project/retrom-runtime/blob/master/docs/supported-platforms-and-cores.md)查询，内容前提从 [Target 指南](https://github.com/retrom-project/retrom-runtime/blob/master/docs/targets/README.md)进入。接入变更按[统一集成流程](platform-cores-integration.md)处理，不在本文复制核心固定版本或机器清单。

实际可用性应通过[正常来源导入](import-source-examples.md)、审核、真实浏览器输入和所声明的保存／新实例恢复链验证，要求见[运行产品验证](core-runtime-validation.md)。
