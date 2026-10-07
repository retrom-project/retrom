# 账号、配置与部署

入口cmd/retrom创建PostgreSQL、Redis、受管Storage、runtime工具和HTTP服务。应用启动应用19表DDL及索引迁移，加载已验证Provider；空release实例只可在/setup一次初始化管理员。RETROM_MODE=test仅供隔离开发，自动创建明确测试管理员；生产不要启用测试模式。

账号密码使用已锁定Argon2组件及密码blocklist，验证后短事务再次围栏确认同一credential/active User，防密码变化后的旧验证创建新会话。密码变化增加session_version并撤销旧会话，selfchange生成新cookie。邀请/重置hash token一次消费、可撤销；重新创建reset撤销旧记录并增加其版本。账号停用/删除撤销有效会话和待用链接。Redis限流按IP/账号hash作用域原子TTL执行，存储不可用失败关闭。

| 配置 | 含义 |
| --- | --- |
| RETROM_DATABASE_URL | PostgreSQL连接URL，凭据通过私有环境注入 |
| RETROM_REDIS_ADDR | Redis地址或TLS/认证URL |
| RETROM_DATA_DIR | 可写持久根 |
| RETROM_DEPENDENCY_ROOT | 密码blocklist的已验证只读依赖根 |
| RETROM_RUNTIME_ROOT / RETROM_NODE | 自足runtime工具根与Node24入口 |
| RETROM_PROVIDER_ROOT | 含active.json/installed的只读Provider根 |
| RETROM_PUBLIC_ORIGIN | 精确宿主origin |
| RETROM_RPG_RUNTIME_ORIGIN_TEMPLATE | 独立隔离origin，使用{runId} |
| RETROM_SOURCE_ROOTS | 显式{id,name,path}数组，源挂载只读 |
| RETROM_TRUSTED_PROXY_CIDRS | 可解释forwarded链的直接代理CIDR |
| RETROM_HTTP_ADDR | 内部监听地址 |
| RETROM_WEB_ROOT | 含public/runtime-isolation的固定Web资产根，镜像已打包该目录 |

Node工具和Provider必须与版本控制的 `data/runtime-inputs.json` 配套。默认准备和镜像构建共同认证其中三个归档、实际解包字节和源码身份；显式build context也必须属于该认证集合。运输输入与未来正式发行流程见[配套依赖输入](dependency-management.md)。当前描述为未发布候选，候选验收不代表tag/release完成。生产镜像只放离线工具、已验证Provider、可由服务UID读取的公共blocklist和Go binary，运行不依赖源码或PFB。

生产同时运行PostgreSQL、Redis、Go、Web及可信反向代理。数据库和Redis不暴露公网；持久根仅服务UID可写。代理把/api/v1和/runtime交给Go，隔离host只代理/__retrom/runtime-isolation/，其他路径404；TLS、wildcard DNS和证书由部署者配置。健康ready端点仅内部验证依赖可用，不能将200当全部产品通过。
