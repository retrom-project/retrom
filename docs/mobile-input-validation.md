# 移动端数字摇杆验证

2026-10-14，`mobile-input-responsiveness` PFB 修复 runtime [#149](https://github.com/retrom-project/retrom-runtime/issues/149)。原始运行时代码提交为 `a02dd8b`；同步 master 后由 runtime PR [#152](https://github.com/retrom-project/retrom-runtime/pull/152) 合入 `d46102d`，发布正式版本 [v0.66.2](https://github.com/retrom-project/retrom-runtime/releases/tag/v0.66.2)。Retrom 固定同次发行的 host tool 和两个 Provider。

EmulatorJS 数字 zone 摇杆原先立即发送新方向，但延后 30ms 释放其他方向。连续右、左、右之后，旧释放任务仍能将最后保持的右方向清零；普通反向也会短暂同时保持相反方向。修复在 vendor 加载前接管数字 zone 的输入翻译，按最新状态同步先释放、再按下，只发送发生变化的方向。摇杆外观、原有映射和模拟轴处理保留；暂停、失焦、隐藏、结束与退出均释放数字方向。没有核心、游戏名或 ROM 特例，没有数据库、核心二进制、内容身份或存档格式变更。

该输入逻辑在 EmulatorJS 构造控件时就需要生效，不能等到 `EJS_ready`。回归同时覆盖 vendor 延后加载及 EmulatorJS 实例尚未赋值的构造顺序。

| 验证层 | 实际结果 |
| --- | --- |
| 定向回归 | 接入前 9 项数字输入断言失败；最终 11 项通过，包含 0/8/16ms 换向、重复移动、斜向、中心、结束、新触点、配置映射、模拟轴保留、暂停、清理及提前绑定 |
| runtime 工程检查 | npm ci、lint、typecheck、build、同步 master 后 313 文件 / 1834 项测试、package:check 通过 |
| 产品审核试玩 | 项目自有 MIT Z80 fixture 经正常扫描及 Parent 上传，在 FBNeo、MAME 2003、MAME 2003 Plus 三个实际核心中运行；28 项浏览器检查通过，无 pageerror |
| 数字触摸路径 | CDP 触摸长按、反向、斜向、中心、多指方向与面键独立释放、取消、CPU 6x 压力、暂停恢复、键盘与 Start、退出通过 |
| 高频输入 | 每个实际核心均验证 200 次同向移动只向 native 输入发送一次按下、一次释放；0/8/16ms 快换向保持最新方向，不再有迟到的清零 |
| 标准手柄 | 在三个实际核心中通过模拟标准 Gamepad API 验证 EJS 轮询、P1 方向和 Start 到 native 输入的交付；未连接实体手柄 |
| 两版控件集成 | 固定 4.2.3 与 4.3.0-pre 各验证默认数字 zone、N64 比例轴布局及自定义数字映射，共 6 组通过；此层使用输入记录器，不代表 N64 游戏运行验收 |
| PFB | `pfb-validate`、`pfb-verify` 通过，实例 healthy；最终 EmulatorJS 开发模块 SHA 为 `534dfadc01cd876029d41d8476f70bdd80b8e13822bf0e6e3b49fefa8fbd80c5` |

产品记录位于该 PFB 的 `.pfb/workspace/acceptance/mobile-input/`，包括原始事件、脚本、截图、测试日志、失败修正记录和摘要清单。三个测试游戏保留为待审核状态。MAME 原创测试程序与商业 DAT 存在预期 CRC 差异，依然通过正常审核试玩入口加载原始 ZIP。

浏览器为 Linux Chrome，Android 触摸与视口模拟，软件 GPU。native 输入调用和核心帧推进已观测，但未测量红米真机、核心读取按键的时刻或屏幕实际呈现延迟。此前压力实验由约 60 FPS / 24ms 事件排队中位数下降至约 26 FPS / 85ms，说明主线程繁忙可以延迟输入，不能直接代替用户手机的归因。默认渲染策略未变；主线程负载及 `preserveDrawingBuffer` 的真机对比继续由 [#150](https://github.com/retrom-project/retrom-runtime/issues/150) 跟踪。

本次按输入改动范围验证，没有重跑存档恢复、全部核心或 T01–T47 全量验收。基线 checkout 和线上保持原状；只新增该 PFB 的 workspace、标准 registry/gateway 注册与 Docker 资源，未选择裸 localhost 的默认 PFB。

## 正式发行复验

Retrom 使用 `runtime-provider-pin-release`、`runtime-provider-prepare` 验证全部正式归档、工具闭包和 Provider 文件；依赖检查及配套输入摘要检查通过。runtime 的 Provider input/build/check、release build 和 package check 均通过，PR 与 tag 流水线通过。

正式 `v0.66.2` 的自足工具与 Provider 在独立 PostgreSQL/Redis 环境启动，未启用开发覆盖层。项目 MIT fixture 重新通过正常扫描与 Parent 上传导入；三个实际核心再次通过 28 项触摸/键盘/暂停检查及标准 Gamepad API 的方向、Start 按下与释放。每个游戏的浏览器实际加载模块 SHA 均核对为 `8718d7323153bd82263adb6bcc6c2b1e094f3b1a1aee74864fd862cea4699662`，与正式 pin 一致。正式记录位于 PFB 验收目录的 `formal/`，临时测试容器和数据库已销毁。

复验脚本显式等待登录后的审核路由、全屏稳定和手柄 P1 绑定；适配当前点击游戏画面即恢复的行为，仍断言暂停时释放全部数字方向。发布不扩大上述输入范围或红米真机性能结论。
