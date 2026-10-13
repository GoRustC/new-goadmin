# gin-admin 底座审计报告（第二轮）

日期：2026-10-13
范围：安全、配置与部署、代码结构、可扩展性、测试与工程化，外加对第一轮之后全部改动的逐行审查
环境：Go 1.27.1；测试机 Ubuntu 26.04（VMware），MySQL 8.4、PostgreSQL 18、Redis 8
第一轮审计的完整清单见 git 历史（提交 540a33c 中的本文件）。

## 结论

后端在 MySQL 或 PostgreSQL 加 Redis 上，按 `configs/prod` 跑通了全部接口和主要异常场景。第一轮的必须修项除第 8 项外均已完成，本轮测试又发现并修复了 7 个缺陷，其中 1 个是未登录即可永久打挂登录的拒绝服务。

但还不能直接作为生产底座：下面"必须修"里还有 10 项，其中 3 项已在测试机上实测复现（进程假活、MySQL socket 连不上、环境变量带引号启动失败），另有一个可以让被冻结用户继续使用系统的缓存绕过。

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

1. **端口绑定失败时进程假活**（实测）
   `internal/bootstrap/http.go:40-52` 在 goroutine 里 `ListenAndServe`，失败只记一条日志，进程继续运行。systemd 认为服务正常，不会重启，但没有任何端口在监听。修法：先 `net.Listen`，失败时同步返回错误。

2. **MySQL 建库时重新拼接连接串，丢掉原有参数**（实测）
   `pkg/gormx/gorm.go:185` 写死 `tcp(...)`。unix socket 地址被当成主机名，启动失败；`tls`、`timeout` 等参数也会丢失。修法：`cfg.Clone()` 后清空库名再 `FormatDSN()`，或改为可关闭的开关。

3. **被冻结或被撤权的用户可以绕过**
   `internal/mods/rbac/biz/login.biz.go:89` 写入的用户缓存没有过期时间；`:62-68` 缓存命中时不再检查用户状态；`user.biz.go:174,194` 在事务提交前删除缓存。被冻结的用户在"删缓存后、提交前"发出的请求，会读到旧状态并写回一份永不过期的缓存，此后冻结、删除、撤权对其 token 全部失效。修法：缓存加过期时间，提交后再删缓存，缓存中带上用户状态。

4. **Casbin 重载信号在事务提交前写出**
   `internal/mods/rbac/biz/role.biz.go:98,148,172`、`menu.biz.go:457,496`。轮询若恰好落在写信号与提交之间，会加载提交前的旧数据并把信号标记为已处理，撤销的权限一直有效。修法：在 `Trans.Exec` 成功返回后再写信号。

5. **`?accessToken=` 携带的 token 会写进数据库日志，可经接口读回**
   `pkg/util/gin.go:29-31` 接受查询串 token；`pkg/middleware/logger.go:43` 记录完整 `RequestURI`；生产配置的日志 hook 把 Info 级请求日志写入 logger 表；有 `/api/v1/loggers` 权限的用户可读到他人（含 root）的 token。修法：去掉查询串 token，或日志只记 `URL.Path`。

6. **用户与角色管理权限没有上限，等同超级管理员**
   `internal/mods/rbac/biz/user.biz.go:132-175`：有编辑用户权限的人可以给自己加任意角色、改任意非 root 用户的密码。`role.biz.go:110-151`：有编辑角色权限的人可以把任意菜单挂到自己的角色上。修法：只能授予自己已有的角色和菜单，禁止修改自己的角色，修改他人密码单独授权。

7. **环境变量按原文替换进 TOML，不做转义**（实测）
   `internal/config/validate.go:22-41`，由 `parse.go:48` 在解析前调用。值含 `"` 时整个文件解析失败，报错看不出与环境变量有关；值含 `\t`、`\n` 时会被悄悄改成控制字符。注释里写 `${X}` 也会被要求设置该变量。修法：解析后再展开字符串值，或按 TOML 规则转义。

8. **标准库有 10 个代码可达的漏洞**
   Go 1.27.1 的 net/http、HTTP/2、crypto/tls、html/template 等，1.27.2 已修复；`golang.org/x/net` 需升到 v0.60.0。修法：升级 Go 与 x/net 后重新构建。

9. **Casbin 加载时单个角色查询失败会被当作成功**
   `internal/mods/rbac/casbin.go:123-124` 只记日志后 `continue`，残缺的策略照样替换上去并标记已处理。一次短暂的数据库故障，会让这些角色的用户在下一次权限变更前一直被拒绝。修法：任一角色失败即返回错误，保留旧策略，下个周期重试。

10. **模块路径加 `internal/`，下游无法作为库引入**（第一轮第 8 项，未修）
    `go.mod:1` 仍为 `github.com/LyricTian/gin-admin/v10`。需要先决定下游是 fork 后改名，还是作为库引入。

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
