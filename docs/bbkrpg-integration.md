# 步步高 RPG（GAM4980）集成

## 产品边界

- 平台 `bbkrpg`，目录「步步高 RPG 游戏」，默认产品核心 `gam4980`。
- Provider `emulatorjs` 的独立 Target `gam4980`；单线程软件核心，内容为单个 `.gam`。
- 原始 GAM 和现有单文件导入流程共用 `EMULATORJS_SINGLE_FILE`；多碟不适用。
- 默认标准手柄方向对应原生方向，底部确认键对应原生 A/Enter。
  键盘 WASD 与 K/J 保持独立映射。Host B 返回契约不变。
- 上游没有实现声音；不声明音量能力。BIOS 与游戏不随 Provider 分发。

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

公共写格式为 `gam4980-state-v1-storage-v1`，由 Provider 统一添加一次 gzip。
核心使用带版本和 CRC 的 `BBKST001`，保存 CPU/RAM、全部 flash、
银行映射、计时器、周期余量、RTC、输入重发与 LCD 状态，并校验当前游戏和 BIOS 身份。
它不兼容上游不完整状态或其他 EmulatorJS 核心的公共存档格式。
原生回归使用项目自有合成 BIOS/GAM，在新实例恢复后继续运行并比较完整机器状态。
真实产品验证入口见 [ACC-BBKRPG-001](project-acceptance.md#acc-bbkrpg-001步步高-gam标准输入与新会话即时恢复)。

## PFB 验证和发布边界

源码、核心构建、测试输入及运行状态保留在命名 PFB 内。
先运行 `pfb-core-build CORE=gam4980`，再以真实候选元数据构建完整 Provider，
校验并显式导入 Provider base，最后启动 PFB。新增 Target 不能通过 loose core override 添加。
日常 `pfb-up`/`pfb-restart` 不构建核心或 Provider archive。

本分支的运行时输入是显式 PFB candidate。正式聚合前需要先发布经过验收的不可变核心
release，再把运行时目录更新为该 release 的真实坐标并发布 Provider，
最后更新 Retrom 的生产 Provider lock。不得把本地 candidate 哈希填进生产 release 坐标。
