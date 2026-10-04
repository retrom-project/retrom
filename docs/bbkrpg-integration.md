# 步步高 RPG（GAM4980）集成

## 产品边界

- 平台 `bbkrpg`，目录「步步高 RPG 游戏」，默认产品核心 `gam4980`。
- Provider `emulatorjs` 的独立 Target `gam4980`；单线程软件核心，内容为单个 `.gam`。
- 原始 GAM 和现有单文件导入流程共用 `EMULATORJS_SINGLE_FILE`；多碟不适用。
- 默认标准手柄方向对应原生方向，底部确认键对应原生 A/Enter。
  键盘 WASD 与 K/J 保持独立映射。Host B 返回契约不变。
- 维护 fork 已实现双通道旋律、计时器中断和 libretro 音频输出，Target 声明音量能力。
  外部语音 ROM/DAC 接口仍未连接，不支持词典语音；时钟与音色保真度仍有待硬件校准。
  BIOS 与游戏不随 Provider 分发。

## 源码及 BIOS

核心来源为 [ThisBoringWorld/gam4980](https://github.com/ThisBoringWorld/gam4980)
固定提交 `eeaa531b55e7127ab4b5e0bdc5ceba686df59c6a`，
目标维护 fork 为 [retrom-project/gam4980](https://github.com/retrom-project/gam4980)，
维护分支 `retrom/geeaa531b55e7`。Retrom 的 `workspace/manifest.yaml` 登记这条依赖边；
运行时只聚合核心仓库生成并验证的产物。

| BIOS | 大小 | 核心中的路径 | SHA-256 |
| --- | ---: | --- | --- |
| `8.BIN` | 2097152 | `/gam4980/8.BIN` | `7663735609c416025b2738c80cedaf11528ff8cb5a7c74c7c31c1f46e43e9caf` |
| `E.BIN` | 2097152 | `/gam4980/E.BIN` | `3e12d40948fd50710cef8c6d14acea26ad69f47312125a61bd1003912893fcad` |

二者是 REQUIRED 的 EXTERNAL_FILE。通过普通 BIOS 上传、校验和安装流程提供。
GAM 头、入口、大小、银行范围以及 BIOS 完整性由原生核心再次校验；
无效输入不能作为可运行游戏继续启动。

## 即时存档

公共写格式为 `gam4980-state-v2-storage-v1`，由 Provider 统一添加一次 gzip。
核心使用带版本和 CRC 的 `BBKST002`，保存 CPU/RAM、全部 flash、
银行映射、计时器、周期余量、RTC、输入重发、LCD 状态和旋律相位/计数器，并校验当前游戏和 BIOS 身份。
Provider 继续读取已有 v1 公共格式；原生 `BBKST001` 恢复时初始化旧格式未保存的音频相位。
它不兼容上游不完整状态或其他 EmulatorJS 核心的公共存档格式。
原生回归使用项目自有合成 BIOS/GAM，在新实例恢复后继续运行并比较完整机器状态。
真实产品验证入口见 [ACC-BBKRPG-001](project-acceptance.md#acc-bbkrpg-001步步高-gam-音频标准输入与新会话即时恢复)。

## PFB 验证和发布边界

源码、核心构建、测试输入及运行状态保留在命名 PFB 内。
先运行 `pfb-core-build CORE=gam4980`，再以真实候选元数据构建完整 Provider，
校验并显式导入 Provider base，最后启动 PFB。新增 Target 不能通过 loose core override 添加。
日常 `pfb-up`/`pfb-restart` 不构建核心或 Provider archive。

当前正式 Provider 已聚合维护 fork 的 `retrom-core-geeaa531b55e7-r1`，精确 commit、
资产摘要和 checkpoint 声明以 runtime 来源清单及 Provider declaration 为准。
后续更新仍先在 PFB 验证候选，再按核心 release → Provider release → Retrom Provider lock
顺序发布。不得把本地 candidate 哈希填进生产 release 坐标。
