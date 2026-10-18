# Siglus 原生存档修复验证

核心统一解释普通、Quick 和 End 的物理槽位，让它们共享一份头部缓存。没有按作品名、路径或脚本增加分支，游戏原始资源保持不变。

普通槽位数为 N、Quick 数量为 Q 时，通用接口可访问 `[0, N+Q+E)` 的物理文件；Quick 相对下标映射为 `N+i`，启用的 End 位于 `N+Q`。保存、复制、交换、删除和注释更新均失效相应物理缓存。原生退出被确认且已有 SAVEPOINT 时，先保存启用的 End，再结束运行；取消退出、没有 SAVEPOINT 和关闭 End 的配置均不生成新的进度。

原生字节布局、公共单层 gzip `GAME_SAVE` 封包、内容身份和 Content I/O 边界均不变。核心文件变化继续使用原有恢复警告和来源存档保护，不能绕过当前内容身份或覆盖来源旧档。

## 候选验收

六项原生槽位／缓存回归在旧 r2 上必红，原生退出 End 回归也先验证失败。修复后 Rust lib 277 项通过、7 项原有忽略，desktop engine check、JS bridge 11 项、发布包装测试通过。runtime 346 个测试文件、2,028 项通过。

| 实际样本 | 已完成的产品行为 |
| --- | --- |
| 纯白之夏，N=170 / Q=10 / End=1 | 普通保存和新 Run 读取；170→171→172 Quick 轮转、菜单时间与缩略图刷新；Quick 读取后鼠标／Enter／标准 Gamepad API 推进；原生退出自动写入 180，新空浏览器实例从 Last 读取、继续输入并重新 Quick 保存 |
| 初恋 1/1，N=101 / Q=1 | 普通第六槽和 101 Quick 读取；中文对白与语音继续；Quick 覆写及缓存刷新；退出确认 NO 不写 End，YES 自动写入 102 并同步。菜单不提供 Last 读取入口，未声称完成该菜单的 End 读取 |

六个此前云存档的版本和 payload 摘要保持不变，实际恢复使用新 Run 并另存。浏览器使用 Chrome for Testing 149、SwiftShader，手柄使用标准 Gamepad API 注入，未验证实体手柄、Brave 真机或完整通关。按需读取保持有界；一个真实故事 Run 最大 Content I/O 读取为 262,144 字节，没有读取 EXE。

完整候选证据保存在 PFB 的忽略验收目录，含红绿测试、原生文件头、Run 身份、逐步截图和网络记录。源码不包含游戏、私有存档或凭据。

## 正式发行与复验

正式发行经过核心维护分支 PR、不可移动 annotated core tag、runtime Provider 发行和 Retrom 标准 pin。运行／许可资产及完整对应源码均逐项核对字节数与 SHA-256。正式身份已通过标准 pin 和完整准备链验证：

| 输入 | 正式身份 |
| --- | --- |
| 核心 | [Siglus r3](https://github.com/retrom-project/siglus_rs/releases/tag/retrom-core-g5f358ef1360b-r3)，`e312024961156a848106d02d3a21477c20f22a5b` |
| 核心源码树 | `574d84bbaffb9e1f84633c54d0788005bf566605dabc17c8715f30d26fd95b83`，与已验收候选相同 |
| MJS / WASM | `a35b1e05eb76c2a9c20afd16cf8736ee58e478f79a06582247cc044777acebe7` / `be1347b81713f4d9f7bbef88ef26bc4b6dcfe39d493b1149454762dd25665ff2` |
| runtime | [v0.72.1](https://github.com/retrom-project/retrom-runtime/releases/tag/v0.72.1)，`b1d98e6cdb4c57d0660c420e18e522de51c40c93` |
| runtime 源码树 | `22866017d403fa42b006f513886e8da129b02e6c1691f923323025aea5089d50` |
| retrom-runtime Bundle / Module | `52d6a1f19dd834954e302ce860c3b455f6bed35a5e59a7a3a9f39e97d04ff7cb` / `a166ebf6cf784c5ab61f4b67c882d2601714a38f0c4678cd83aa7e4dbb4dbb05` |
| Siglus 核心文件指纹 | `31fa2691887d253a23ccdb1c9c94788c866b1480b65831e8ef7001165eedf31b` |

正式归档的 112 个 Target 与前一正式版逐项对比，仅 Siglus 指纹变化，其余 111 个不变。正式产品复验使用上述实际资产，Run、存档文件头、截图、自动同步和来源旧档完整性与候选证据分别保存于发布验收记录；不能用相同源码树代替对正式 WASM 的运行验证。未提升全部 Siglus 作品、完整通关或 T01–T47 的整体通过状态。

## 额外发现的兼容边界

升级前线上 r2 的《初恋 1/1》全新开局并按 Ctrl 快进时出现 `unhandled form command chain [38, 2, 3]`。这与从真实故事存档开始的存档验收不同，不能将本次结果解释为该作品全流程兼容。新版复现及后续追踪在发布报告中单独记录。
