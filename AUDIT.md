# gin-admin 底座审计报告（第二轮）

日期：2026-10-13
范围：安全、配置与部署、代码结构、可扩展性、测试与工程化，外加对第一轮之后全部改动的逐行审查
环境：Go 1.27.1；测试机 Ubuntu 26.04（VMware），MySQL 8.4、PostgreSQL 18、Redis 8
第一轮审计的完整清单见 git 历史（提交 540a33c 中的本文件）。

## 结论

后端已按"对外开放的生产环境"加固完毕：第二轮必须修的前 9 项和上线前清单中的代码类条目均已修复，并在 MySQL、PostgreSQL 加 Redis 上验证，包括按 `deploy/` 中的 systemd 服务实际部署。

上线前仍需完成两件需要人工参与的事：与前端联调一遍（后端有若干行为变化，见 README 的"安全机制"），以及决定重置密码接口开放给哪个菜单。作为多项目共享底座，还需决定模块路径方案（必须修第 10 项）。

## 测试与验证

| 测试 | 环境 | 结果 |
|---|---|---|
| 全部单元测试与集成测试 | 本机 | 通过 |
| 竞态检测 `go test -race ./...` | 本机 | 通过，无竞态报告 |
| 端到端测试，覆盖全部 26 个接口：验证码、登录、按角色放行与拒绝、授权与撤权实时生效、冻结与删除用户终止会话、改密、刷新 token、登出黑名单跨实例生效、错误格式、请求体与 gzip 炸弹限制、并发验证码攻击 | SQLite（日常 `go test`）、MySQL + Redis、PostgreSQL + Redis | 全部通过；各连续运行 3 遍均通过 |
| 按生产配置启动、重启 | 测试机，MySQL 与 PostgreSQL | 正常，日志无错误、无密钥泄露，Swagger 关闭 |
| 运维故障场景 | 测试机 | 见下表 |

| 故障场景 | 实测结果 |
|---|---|
| SIGTERM 停止 | 0.1 秒内退出，退出码 0 |
| 停止时有请求进行中 | 已接收的请求均返回 200 |
| 启动时数据库或 Redis 不可用 | 立即失败退出 |
| 运行中 MySQL 停止后再停服务 | 0.1 秒内正常退出，日志写入失败但不卡住（网络黑洞时可能不同，未测） |
| 端口被占用 | **进程不退出也不监听**，见必须修第 1 项 |
| MySQL 使用 unix socket | **启动失败**，见必须修第 2 项 |
| 环境变量含双引号 | **配置解析失败**，见必须修第 7 项 |
| 同一验证码 16 路并发登录 | 修复前第 25 轮后验证码接口永久卡死；修复后 30 轮无异常 |

## 本轮已修复

| 问题 | 来源 | 提交 |
|---|---|---|
| 验证码库内存存储并发时死锁，未登录即可让所有人无法登录，健康检查仍为 200 | 原项目依赖 | 2bcabf7 |
| `Util.Captcha.CacheType = "redis"` 从未生效，多实例必须会话保持 | 原项目 | 2bcabf7 |
| Casbin 重载信号精度为秒，同一秒内的第二次权限变更会丢失 | 原项目 | 5065bdc |
| 验证码图片的禁止缓存响应头在写出图片后才设置，从未生效 | 原项目 | e92767a |
| Casbin 策略行末尾多一个空格，所有非 root 请求被拒绝 | 第一轮修复引入的回归 | abc9cf1 |
| PostgreSQL 下日志查询 `join user` 未加引号，日志页面 500 | 原项目 | 23bc11a |
| 日志表只由日志 hook 创建，关闭 hook 后日志页面 500 | 原项目 | 07b6fa2 |

第一轮必须修项的修复进度：除第 8 项（模块路径与 `internal/`）外全部完成，见 c011759、a973f3c、5fb9dd1。

---

## 必须修

| # | 问题 | 状态 | 提交 |
|---|---|---|---|
| 1 | 端口绑定失败时进程假活 | 已修，测试机验证：第二个实例 0.1 秒内以退出码 2 退出 | a4dce37 |
| 2 | MySQL 建库时重新拼接连接串，丢掉 socket、TLS、超时等参数 | 已修，测试机验证：unix socket 可正常启动 | 240a006 |
| 3 | 被冻结或被撤权的用户可经缓存绕过 | 已修：缓存加过期时间，提交后删除并延迟二次删除 | 6c37d60 |
| 4 | Casbin 重载信号在事务提交前写出 | 已修：提交成功后再发信号 | 9556d3a |
| 5 | 查询串中的 token 写入数据库日志 | 已修：记录日志时遮蔽 `accessToken`、`access_token`、`token` | 0001a4a |
| 6 | 用户与角色管理权限没有上限 | 已修：非 root 只能授予、管理不超过自身菜单范围的角色和用户，不能通过用户管理修改自己 | 608e98b |
| 7 | 环境变量按原文替换进 TOML | 已修：解析后再展开，测试机验证含引号和反斜杠的值可正常启动 | 57d81b7 |
| 8 | 标准库漏洞 | 已修：要求 Go 1.27.2，升级 x/net 与 opentelemetry；govulncheck 无可达漏洞 | 2d63306 |
| 9 | Casbin 单个角色加载失败被当作成功 | 已修：整次加载失败，保留旧策略，下个周期重试 | 921702b |
| 10 | 模块路径加 `internal/`，下游无法作为库引入 | **部分完成**：模块路径已改为 `github.com/GoRustC/new-goadmin`；骨架仍在 `internal/`，是否移出取决于下游 fork 还是作为库引入 | e0c2570 |

修复中新发现：`configs/menu.json` 没有任何菜单包含重置密码接口 `PATCH /api/v1/users/{id}/reset-pwd`，因此除 root 外无人能使用该功能。是否开放给某个按钮，属于产品决定，暂未改动。

---

## 对外开放加固（第三轮）

| 问题 | 处理 | 提交 |
|---|---|---|
| 并发 403 修改共享错误对象（数据竞争） | `ResError` 改为处理副本 | b30dddc |
| 停机时日志 hook 可能 panic，队列满时阻塞调用方 | 加锁保护关闭，满时丢弃，Flush 最多等待 10 秒 | f2545ac |
| 超长 `X-Request-Id` 使请求不进审计日志 | 只接受 64 字符以内的简单 ID，日志字段按列宽截断；测试机 MySQL 验证 | 783785e |
| 日志 caller 偏一层 | 按配置使用，默认 0 | fc9d0cf |
| 非 debug 模式不记录 SQL 错误和慢查询 | 经 zap 记录错误和超过 200ms 的查询 | affde78 |
| 缺少安全响应头、请求头超时；健康检查不检查依赖 | 安全响应头、`ReadHeaderTimeout`、`MaxHeaderBytes`，新增 `/health/ready`；测试机验证 MySQL 停止时返回 503 | 5294ed4 |
| 登录可暴力破解、可枚举账号 | 连续失败锁定账号名，先验密码再提示冻结，未知账号做等时比较 | 1cb5202 |
| 改密码、重置密码不吊销旧 token；刷新可无限续期 | 改密或设置密码后吊销该用户所有旧 token，刷新时作废旧 token；root 可刷新 | 1cb5202 |
| 日志表无限增长 | `Dictionary.LoggerRetentionDays`，生产样例保留 90 天 | 04ea7a3 |
| 生产样例连接池过大、无超时、未开限流 | 连接池 25、连接串超时示例、开启限流、说明 `TrustedProxies` | 88073ba |
| 没有 CI | GitHub Actions：格式、vet、竞态测试、漏洞扫描、MySQL 与 PostgreSQL 端到端测试 | 400127c |
| 没有 README 和部署文件 | README、systemd 服务、环境变量模板、nginx 示例；测试机按此部署验证 | 6276817 |

## 底座优化（第四轮）

| 问题 | 处理 | 提交 |
|---|---|---|
| 跳过鉴权的路径是裸前缀匹配 | 按路径段匹配，`/api/v1/login` 不再覆盖 `/api/v1/login-history` | 110dd23 |
| 会话可通过刷新无限续期 | token 带 `auth_time`，自密码登录起最长 7 天（`MaxSessionHours`） | 9c101d2 |
| 生产模式接受明文 MD5 root 密码；`OldSigningKey` 不校验长度；可关闭 Casbin；pprof 可绑公网 | 生产模式要求 bcrypt（新增 `ginadmin hash-password`），旧密钥至少 32 字节，拒绝关闭 Casbin 和非回环 pprof | 9ccd0c0 |
| 启动失败以 panic 退出；`start` 默认 dev 配置；`stop` 不校验进程号 | 输出一行错误并以 1 退出，失败时清理并刷出日志；`-c` 必填；`stop` 校验 pid、发信号成功后才删锁文件 | 3b4f54b |
| Redis 键没有应用前缀 | `Storage.Cache.KeyPrefix`，覆盖缓存、token 存储、限流和验证码；测试机验证所有键均带前缀 | 0468e12 |
| 菜单按 code 路径查询从第二级起查错；菜单可移到自己的子孙下形成环 | 逐级按上一级菜单查找；拒绝成环的移动 | dff6f09 |
| PostgreSQL 搜索区分大小写；搜索框中的 `%`、`_` 被当作通配符 | 统一 `LOWER(...) LIKE LOWER(...)` 并转义通配符，三种数据库验证 | 0bddd1a |

## 仍未处理

**需要决定或配合**
- 与前端联调。
- 重置密码接口开放给哪个菜单（`configs/menu.json`）。
- 下游采用 fork 还是作为库引入（决定骨架是否移出 `internal/`）。

**安全**
- 菜单资源可注入 Casbin 策略（仅当 `DenyOperateMenu=false`，生产样例为 true）。

**运维与结构**
- 多实例首次同时启动的建表与菜单导入冲突（README 要求先启动一个实例）。
- 依赖注入只完成一半：`BuildInjector` 不接收配置参数，bootstrap 和 wirex 仍读全局 `config.C`。
- 新增一个模块需要修改多处共享文件；`http.go` 写死了 rbac 模块。
- `pkg/errors` 的 `As` 签名与标准库冲突，"已存在"类错误返回 400 而非 409。
- N+1 查询（菜单树、Casbin 加载）；Casbin 的 autoLoad goroutine 在 Release 后泄漏。
- gzip 解析失败时的降级读取会截断请求体。
- 测试工程：旧的 `test/` 集成测试包绕过中间件，可删除；`pkg/cachex/tmp` 测试目录持续增长。

---

## 建议修

**安全**
- 菜单资源可注入 Casbin 策略：`casbin.go:111` 直接拼接 `res.Path`，`schema/menu.go:149` 不校验。path 含换行可给任意角色加权限，含逗号可让鉴权整体 500。前提是 `DenyOperateMenu=false`（结构体默认值）。
- 改密码、重置密码不吊销已有 token；刷新 token 可无限续期，没有绝对会话上限：`login.biz.go:205,309`、`user.biz.go:198-221`。
- `OldSigningKey` 没有长度校验，可被离线爆破后伪造 root：`internal/wirex/injector.go:39-41`。
- 非 debug 模式仍接受明文 MD5 的 root 密码；`cmd/start.go:35` 默认 `-c dev`，忘加 `-c prod` 就以公开口令运行。
- 跳过列表是裸前缀匹配，下游新增 `/api/v1/login-xxx` 会绕过鉴权：`pkg/middleware/middleware.go:12-20`。
- 登录可枚举账号（冻结提示先于密码校验、响应时间差），无失败次数限制，生产样例未开启限流：`login.biz.go:175`。
- 超长 `X-Request-Id` 让该请求的数据库日志整条写入失败，审计日志可被规避：`trace.go:37`、`schema/logger.go:13`。
- resolver 的 DSN 明文写入日志：`pkg/gormx/gorm.go:109-110`。
- 缺少安全响应头（`X-Content-Type-Options`、`X-Frame-Options`、HSTS）；主服务器未设 `ReadHeaderTimeout`。
- 生产校验缺口：非 debug 模式未拒绝 `Casbin.Disable=true`、非回环的 `PprofAddr`、开启限流但上限为 0。

**配置与运维**
- 日志 caller 偏一层且无法设为正确的 0：`configs/prod/logging.toml:4`、`pkg/logging/init.go:104-107`。
- 连接池 `MaxOpenConns=100` 加 hook 连接，两个实例即超过 MySQL 默认上限，单实例即可能超过 PostgreSQL 默认上限：`configs/prod/server.toml`。
- 示例连接串没有超时参数，数据库网络中断时请求和停机都可能长时间挂起。
- 多实例同时首次启动，迁移与菜单导入会冲突，菜单可能重复：`rbac/main.go`、`menu.biz.go:57-82`。建议拆出单独的迁移命令。
- logger 表无限增长，没有清理机制；超长 message 在 MySQL 严格模式和 PostgreSQL 下写入失败。
- 启动失败路径不清理日志 hook，退出方式为 panic：`bootstrap.go:87-94`、`cmd/start.go:100`。
- `TrustedProxies` 默认为空：部署在 nginx 后面而未配置时，所有未登录请求共用一个限流桶。需写进部署说明。
- 新增的配置校验会拒绝部分旧配置：`RateLimiter.Store.Type` 为空、`Storage.DB.Type` 大小写不一致。
- Redis 键没有应用级前缀，多套环境共用一个 Redis 库时会互相覆盖。

**代码与结构**
- 菜单按 `code` 路径查询从第二级开始查错，应传 `lastMenu.ID`：`menu.biz.go:227`。
- `ResError` 原地修改包级单例错误对象，并发 403 必然产生数据竞争：`pkg/util/gin.go:134`、`pkg/middleware/casbin.go:10`。
- 日志 hook 的 `Write` 与 `Flush` 有先检查后发送的竞态，停机时可能 panic：`pkg/logging/hook.go:97-120`。
- Casbin 的 autoLoad goroutine 在 Release 后泄漏：`casbin.go:219,238`。
- gzip 解析失败时降级读取会截断请求体，所有读错误都报 413：`pkg/middleware/copybody.go:44-60`。
- 依赖注入只完成一半：`BuildInjector` 没有配置参数，`InitDB`、`InitCacher`、`InitAuth` 和 bootstrap 仍直接读 `config.C`。
- 新增一个模块要改 7 处共享文件并重新生成两类代码；`http.go` 写死了 `injector.M.RBAC`。建议定义 `Module` 接口。
- 数据库差异：PostgreSQL 的 LIKE 区分大小写；日志时间范围按字符串比较；`OrderByParams.ToSQL` 不给字段名加引号。
- 菜单可移动到自己的子孙节点下形成环：`menu.biz.go:382-405`。
- N+1 查询：`menu.biz.go:201,258,427,490`，`casbin.go:103-126`。
- 错误 ID 与状态码不统一，"已存在"返回 400 而非 409；`pkg/errors` 的 `As` 签名与标准库冲突。

**测试与工程化**
- 没有 CI、lint 配置和 README。
- 测试临时目录不清理：`test/e2e` 的 `os.Exit` 跳过了 defer，`test/test.go` 从不删除；`pkg/cachex/tmp/badger` 持续增长，并发运行时 badger 目录锁会 panic。
- `test/` 旧集成测试包绕过全部中间件，已被 `test/e2e` 覆盖，建议删除。
- `prod_test` 受开发者 shell 中已导出的 `GINADMIN_*` 变量影响。
- 端到端测试不经过 `bootstrap.Run`，日志初始化、gorm 日志 hook、SQLite 忙等待设置没有测试覆盖。
- swag 未在 `go.mod` 中固定版本；`.gitignore` 中 `gen_rbac_policy.csv`、`/test/data` 等条目已过时。
- 并发攻击类缺陷依赖时序，相关测试为概率性检测。

---

## 可选

- `Casbin.Disable=true` 时调用 `GetEnforcer` 会空指针，目前靠中间件 Skipper 挡住。
- root 无法刷新 token（数据库中没有 root 用户）。
- 改密码接口的新密码没有长度上限，超过 72 字节时 bcrypt 报错返回 500。
- `UserStatusFreezed = "freezed"` 拼写错误且已写入数据库，越晚改成本越高。
- `gormx` 同时链接三种数据库驱动，下游无法裁剪。
- 日志输出到 stderr 而非注释所写的 stdout；启动时打印的多行配置会干扰按行解析的日志采集。
- `IdleTimeout=10` 小于 nginx upstream keepalive 默认值，可能偶发 502。
- Go 1.27 现代化：`interface{}` 换 `any`、`err ==` 换 `errors.Is`、`atomic.Int32`、`wg.Go`、`strings.SplitSeq`。
- `pkg/` 中仍有 0 引用的导出 API（`errors.Parse`、`errors.Equal`、`toml.Marshal`、`crypto/aes` 的固定密钥）。
