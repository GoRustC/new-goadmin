# gin-admin 作为通用底座的只读审计清单

日期：2026-09-26
范围：安全、配置与部署、代码结构、可扩展性、测试与工程化
环境：Go 1.27，依赖已升级（纯 Go sqlite、badger v4、jwt v5、stdlib errors/json）

## 总体结论

作为单项目脚手架合格，作为多个下游项目的共享底座**目前不合格**。硬伤集中在三处：

1. 默认密钥与默认口令让所有下游继承同一套可伪造凭证。
2. `internal/` 加全局 `config.C` 决定了下游只能 fork 改名，无法持续合并 upstream。
3. 集成测试包本身就跑不起来。

`pkg/util/gin.go:118` 的 vet 修复（`errors.InternalServerError("", "%s", err.Error())`）正确且完整，全仓库无同类问题，可直接提交。

已核对无问题：bcrypt 密码哈希与 `crypto/rand` 用法正确；jwt v5 keyFunc 拒绝 alg=none 与 RS 混淆；Casbin 路径匹配无绕过面，enforcer 为 nil 时 fail-closed；SQL 全部参数化，Order/Select 字段均为服务端常量；wire_gen 与 swagger 三份生成物与源码同步；`go build`、`go vet`、`gofmt`、`go mod verify`、`go mod tidy -diff` 全部干净；sqlite 已是纯 Go，`CGO_ENABLED=0` 构建通过；`.DS_Store` 未被跟踪。

---

## 修复进度（2026-10-13）

必须修 12 项中已完成 11 项，剩余第 8 项待定。

| 项 | 状态 | 提交 |
|---|---|---|
| JWT 默认密钥 | 已修：无内置密钥，非 debug 模式必须配置不少于 32 字节的密钥，拒绝旧公开值 | c011759 |
| token 与口令入日志 | 已修：不再记录请求体、响应体和签发的 token | c011759 |
| gzip 解压无上限 | 已修：解压后同样受 `MaxContentLen` 限制 | c011759 |
| 启动打印密钥 | 已修：打印配置时对密码、密钥、DSN 脱敏 | c011759 |
| pprof 经 Prometheus 端口暴露 | 已修：Prometheus 集成已删除，pprof 改用私有 mux | c011759 |
| 默认口令与 root 设计 | 已修：无内置默认口令；root 密码恒定时间比较、支持 bcrypt、为空即禁用；非 debug 模式拒绝公开默认值 | a973f3c |
| 配置无校验、无环境变量 | 已修：支持 `${NAME}` 与 `${NAME:-default}` 引用，启动时校验配置 | a973f3c |
| 模块路径与 `internal/` | **未修**：需要先决定下游是 fork 后改名，还是作为库引入 | — |
| 全局 `config.C` | 已修：schema 不再读配置，biz 与 rbac 通过 wire 注入配置 | 5fb9dd1 |
| Casbin 多实例与落盘 | 已修：策略只在内存加载，撤销权限立即生效，修复计时器竞态，内存缓存下给出多实例警告 | a973f3c |
| 集成测试 panic | 已修：使用临时工作目录并自动建表，Redis 测试未配置时跳过 | c011759 |
| README 过期 | 已处理：旧 README 已删除，尚未编写新 README | 540a33c |

修复过程中发现并修复的额外问题：日志 hook 与业务共用 SQLite 文件时并发写入会报 `database is locked`，已为 SQLite 连接加上忙等待和立即加锁的事务模式（f603faa）。

建议修中顺带完成的：dal 绕过事务上下文（5fb9dd1）、wire 生成工具版本已用 `go.mod` 的 `tool` 指令固定（5fb9dd1）。

---

## 必须修

- **[安全] JWT 签名密钥有内置默认值，可离线伪造 root**
  `internal/config/middleware.go:39` 默认 `XnEsT0S@`；`pkg/jwtx/jwt.go:22` 第二个兜底 key；`configs/dev/middleware.toml:31` 同值。伪造 `sub=root` 后经 `internal/bootstrap/http.go:172-175` 直接跳过 Casbin。
  修法：删掉两处默认值，密钥为空或等于已知值时启动失败。

- **[安全] 登录 token 与口令 MD5 写进日志，且可经 API 读回**
  `internal/mods/rbac/biz/login.biz.go:134` 以 Info 级打印完整 token；`pkg/middleware/logger.go:60-69,77-81` 记录 POST/PUT 请求体和响应体；`configs/dev/logging.toml:12-15` 把 Info 日志写入 `logger` 表；`internal/mods/rbac/main.go:105-108` 的 `/api/v1/loggers` 原样返回。
  修法：删除 :134 的 token 日志；Logger 中间件对 `/api/v1/login`、`/api/v1/current/password` 等路径跳过 body 或脱敏。

- **[安全] gzip 请求解压后大小无上限，未鉴权即可打爆内存**
  `pkg/middleware/copybody.go:43-48` 的 `MaxBytesReader` 只限压缩流，解压输出不受限；该中间件在 `http.go:136` 位于 Auth（`:142`）之前。
  修法：解压 reader 再套 `io.LimitReader`，默认 `MaxContentLen` 降到 1–4MB。

- **[安全/配置] 启动时全量打印配置，含 DSN、Redis 密码、JWT 密钥、root 密码**
  `internal/config/config.go:150-157`，由 `bootstrap.go:42` 调用，`DisablePrintConfig` 默认 false（`config.go:25`）。daemon 模式下落到 `cmd/start.go:67-76` 的 0666 日志文件。
  修法：默认关闭，或对 `Password/DSN/SigningKey` 脱敏。

- **[安全/部署] 开启 Prometheus 后 pprof 无鉴权暴露在 9100，且库代码 `log.Fatal` 可杀进程**
  `bootstrap.go:7` 匿名导入 pprof 注册到 DefaultServeMux；`pkg/promx/prom.go:134,147` 用 `nil` handler 监听所有网卡，Basic Auth 只包 `/metrics`；`config.go:101-102` 默认 `admin/admin`。
  修法：pprof 与 metrics 各用独立 `http.NewServeMux`；去掉 admin/admin 默认值。

- **[安全] 默认口令与 root 账号设计**
  `config.go:26` `DefaultLoginPwd` 是 MD5("abc-123")，`user.biz.go:97,202` 用它给新建和重置的用户；`config.go:39-44` 与 `configs/dev/server.toml:24` 的 root 密码是公开 MD5，`login.biz.go:152-155` 用 `!=` 直接比较（非常量时间）。
  修法：新建/重置随机口令并返回管理员，或加首次登录改密标记；root 密码存 bcrypt 并用 `CompareHashAndPassword`。

- **[配置部署] 配置无校验、无环境变量注入**
  `internal/config/parse.go:30-84` 只做 defaults 加 json/toml 合并，全仓 `Getenv/LookupEnv/ExpandEnv` 零命中。容器化部署下密钥只能写进 toml。

- **[结构] 模块路径 `github.com/LyricTian/gin-admin/v10` 加 `internal/` 封装，下游无法 import**
  `go.mod:1`；`internal/config`、`internal/wirex`、`internal/bootstrap`、`internal/mods` 全部不可复用，54/95 个文件写死路径。
  修法：把骨架移出 `internal/`，或明确 fork 模式并提供改名脚本与 upstream 合并流程。

- **[结构] schema 与 biz 直接读全局 `config.C`**
  `internal/mods/rbac/schema/user.go:34` 等 7 个 `TableName()`（menu.go:45、role.go:31、user_role.go:21、role_menu.go:20、menu_resource.go:21、logger.go:26）；`login.biz.go:34-35,152-157`、`user.biz.go:97,202`。任何单测都得先 `config.MustLoad`（`test/test.go:23-24`），wire 注入被架空，一进程只能一份配置。表前缀同时走 `pkg/gormx/gorm.go:61-63` NamingStrategy 与手写 TableName 两条机制。

- **[扩展] Casbin 多实例不刷新、策略落盘、配置为 0 时 panic**
  `internal/mods/rbac/casbin.go:189-215` 靠 cache key 轮询，默认 cache 是 memory（`config.go:49`），多实例永不刷新；`:113-121` 每次重载写 `configs/gen_rbac_policy.csv`，只读文件系统下启动失败；`:191` `AutoLoadInterval=0` 时 `NewTicker` 在 goroutine 里 panic。

- **[测试] 集成测试包在 `init()` 阶段 panic，`go test ./...` 整体是红的**
  `test/test.go:24` 只加载默认值，`config.go:73` `AutoMigrate` 默认 false，`casbin.go:57-64` 查 `role` 表报 no such table。`pkg/cachex/redis_test.go:14-16` 硬编码 `localhost:6379` 无 skip，失败前重试 35 秒。

- **[工程] README 指向不存在的工具链**
  `README.md:61,69,148,158` 的 `make` 目标、`:77,228-229` 的 Dockerfile/Makefile 均不存在；`:218,220` 仍列出已删除的 `pkg/mail`、`pkg/oss`；`:38` 写 Go 1.19+ 而 `go.mod:3` 要求 1.27。`README_EN.md` 同行号。

---

## 建议修

- **[结构] `pkg/errors` 设计与标准库冲突**
  `errors.go:254` 的 `As(err)` 签名与标准库不同，导入本包的文件写不了 `errors.As(err, &t)`；`:21-23` `WithStack` 已是空操作但全仓 53 处仍在调；`:246` `FromError`、`:222-223` `Equal` 用类型断言，经 `:40` `Wrapf` 包装过的 `*Error` 会被当纯字符串；`:291` `MultiError.Error()` 对 `[]error` 做 `json.Marshal` 丢内容。

- **[结构] `ResError` 原地修改共享错误对象**
  `pkg/util/gin.go:131,134` 改写 `ierr.Detail/Code`，而 `pkg/middleware/casbin.go:10` 的 `ErrCasbinDenied` 是包级单例，带 status 参数调用会永久污染并有数据竞争。

- **[结构] biz 依赖 gin、无接口、不可 mock**
  `login.biz.go:19,33,104`；`http.go:145` 穿透四层具体类型 `injector.M.RBAC.LoginAPI.LoginBIZ.ParseUserID`；`user.biz.go:18-22` 全是具体指针。仓库没有一个 biz 单测。

- **[扩展] 新增一个模块要改 6 处并重跑两个未固定版本的 CLI**
  `internal/mods/mods.go:18,22,26,43,51`、`internal/wirex/wire_gen.go`、`configs/menu.json`、swagger 三份生成物加 `internal/swagger/v3/openapi.json` 手动同步、`configs/dev/middleware.toml:29,72`。`go.mod` 无 `tool` 指令固定 wire/swag。

- **[配置] `/openapi.json` 路由指向不存在的文件**
  `http.go:60` 提供 `WorkDir/openapi.json`，`configs/` 下没有，线上必 404。

- **[结构] 两套 Redis 客户端并存**
  `pkg/middleware/ratelimiter.go:11-12` 用 `go-redis/redis/v8` 加 `redis_rate/v9`，`pkg/cachex/redis.go:9` 用 `redis/go-redis/v9`。升 `redis_rate/v10` 可统一。

- **[安全] 限流语义错误**
  `ratelimiter.go:103` `rate.NewLimiter(rate.Every(period), maxRequests)` 实际是"突发 N 后每周期只放 1 个"；`:139` 整除，`MaxRequestsPerIP < Period` 时 `PerSecond(0)` 全部拒绝。

- **[安全] 信任所有代理的 X-Forwarded-For**
  `http.go:32` 从未 `SetTrustedProxies`，gin 默认信任 0.0.0.0/0；`ratelimiter.go:59` 按 IP 限流可伪造，`logger.go:44` 审计 IP 可污染。

- **[安全] `?accessToken=` 接受 token**
  `pkg/util/gin.go:29-31`，token 进 `logger.go:49` 的 uri 字段、代理日志、Referer。

- **[安全] 分页无下限，`pageSize=0` 拖全表**
  `pkg/util/schema.go:27-28` 只有 `max=100`；`db.go:91-96` pageSize≤0 不加 LIMIT。

- **[配置] Swagger 默认开启且不经 Auth**
  `http.go:59-62`，`config.go:24` 默认 false；`/swagger/*` 在 `/api/` 前缀之外。

- **[安全] Casbin 陈旧放行与并发细节**
  `casbin.go:70-72,112-131` 无角色或无资源时不替换旧 enforcer，撤销的权限继续生效；`:76-95` `LoadThread=0` 时策略静默为空；`:191` 与 `:218` 的 `ticker` 读写有竞态。

- **[安全] 跳过列表是裸前缀匹配**
  `pkg/middleware/middleware.go:7-20`，`middleware.toml:29` 的 `/api/v1/login` 会放行下游新增的 `/api/v1/login-history`。

- **[安全] `pkg/crypto/aes` 不安全且会 panic**
  `aes.go:12` 导出固定密钥，`:35,56` 用 key 前 16 字节当 IV，CBC 无 MAC，`:21-25` 去填充不做边界检查，`:56-58` 非块倍数密文 panic。仓库内未用，但下游会拿来用。改 AES-GCM 或删除。

- **[结构] dal 绕过事务上下文**
  `dal/user_role.dal.go:30`、`logger.dal.go:30` 用 `a.DB.Table` 而非 `util.GetDB(ctx, a.DB)`，在 `Trans.Exec` 内会跑在事务外。

- **[扩展] N+1 查询**
  `menu.biz.go:200-210` 每菜单查一次资源（`MenuResourceDAL` 已支持 `MenuIDs` 批量），`:258-271` 每项一查；`casbin.go:96-98` 每角色两条查询。

- **[结构] `pkg/gormx` 细节**
  `gorm.go:97,103` 给 replica/source 建目录误用主 DSN；`:47` MySQL 每次启动 `CREATE DATABASE` 且不可关；`:65` 非 Debug 时 gorm 错误与慢查询完全不落日志。

- **[结构] 表约定**
  `schema/user.go:21` username、`role.go:20` code、`menu.go:28` 只有普通索引无唯一约束，唯一性仅靠 biz 的 `Exists`，并发可重复；无软删除；时间戳在 biz 手工赋值（`user.biz.go:93,146`）。

- **[结构] Logger 表两份定义与日志 hook 脆弱**
  `pkg/logging/gorm.go:11-21` 与 `schema/logger.go:11-23` 描述同一张表；`gorm.go:24-27` AutoMigrate 不受配置控制且 panic；`gorm.go:50,54` 无 ok 的类型断言会让 `hook.go:80-85` 的 worker 逐个死亡，之后日志静默丢弃（`hook.go:101-104`）。

- **[配置] `bootstrap/logger.go:36` 传的是 `cfg.Extra` 而非补了 appname 的 `extra`**
  `Extra` 为空时 appname 丢失。

- **[结构] Cache 层**
  `cachex/badger.go:31` 打开失败直接 panic，三个构造函数都不返回 error，redis 不 Ping；`jwtx/store.go:28-34` 复制了一份 `Cacher` 接口，`jwtx.NewMemoryCache` 0 引用；用户缓存 TTL 不一致（`login.biz.go:86` 无过期，`:192-193` 有过期）；`redis.go:104-111` Delete 两次往返。

- **[结构] 全局状态**
  `parse.go:17-26` 单例加 `sync.Once`，`MustLoad` 第二次静默不生效；`internal/utility/prom/prom.go:10-13` 包级变量绕过 wire；`logging/init.go:152` `ReplaceGlobals`；`pkg/util/command.go:43` 库函数里 `os.Exit`（前面 `time.Sleep(100ms)`），导致 `bootstrap.go:30-34` 的 `defer Sync` 永远不执行，`SIGHUP` 被吞但不 reload；user id / trace id 在 `util/context.go:11-17` 和 `logging/logger.go:20-26` 各存一份。

- **[结构] `pkg/util` 是大杂烩且不是叶子包**
  混装 gin（gin.go）、gorm（db.go）、信号退出（command.go）、context key、ID；本身依赖 `pkg/logging`、`pkg/errors`。导出但 0 引用的 API：`util.RandomizedIPAddr`、`util.MustNewUUID`、`util.NewRowLock/FromRowLock`、`util.GetBodyData`、`util.ParseForm`、`logging.LoadConfigFromToml/PrintLogger/NewLogger`、`jwtx.NewMemoryCache/WithCacheNS`、`pkg/crypto/aes`、`pkg/crypto/rand`、`toml.Marshal`、`middleware.Trace/Logger/CopyBody/Recovery` 默认构造、`errors.Parse/Equal/NewMultiError`。建议先裁剪再对外承诺。

- **[配置] 只有 dev 一套配置，Debug 默认全开**
  `configs/` 只有 `dev/`，`cmd/start.go:34-35` 默认 `-c dev`；`server.toml:4,46,59`、`logging.toml:2`；`logging/init.go:57-58` 中 `Debug=true` 会静默覆盖 `Level`。

- **[部署] HTTP server 与健康检查**
  `http.go:73-79` 无 `ReadHeaderTimeout`、`MaxHeaderBytes`；`:33-35` `/health` 不检查 DB 和缓存，对 readiness 无意义。

- **[部署] daemon/stop 模式不可靠**
  `cmd/start.go:67,89` 锁文件与日志写 cwd 且 0666，`:86,90` 在配置加载前读 `AppName` 得空串；`stop.go:25` 直接 `kill` 不校验 pid，`:31` kill 失败也删锁。建议删除，交给 systemd 或容器。

- **[工程] 版本号四处重复，构建不可复现**
  `main.go:11`、`config.go:21`、`server.toml:3`、`internal/swagger/docs.go:2006`；`-ldflags -X main.VERSION` 不影响 `bootstrap.go:53` 的启动日志；全仓无 `-trimpath`。

- **[工程] 无 Dockerfile、Makefile、CI、lint 配置**
  无 `.github/workflows`、`.golangci.yml`、pre-commit；vet 类问题只能靠人工发现。

- **[工程] `.gitignore` 缺失与残留**
  缺 `ginadmin.log`、`dist/`（daemon 与 start.sh 产物）；`:20` `/internal/test/data`、`:27-28` 指向不存在的路径。

- **[测试] 测试不 hermetic，覆盖面空白**
  `cachex/badger_test.go:15` 写包目录且不清理，`pkg/cachex/tmp/badger/` 已累积 11 个 sst；`test/test.go:20` 包级共享 app 用 `init()` 而非 `TestMain`，Casbin autoLoad goroutine 从不 Release；`:37-38` 在裸 `gin.New()` 上注册路由绕过全部中间件；`:52` 把含密码的请求体全量打印；`internal/*` 覆盖率 0%，`pkg/middleware`、`pkg/errors`、`pkg/logging`、`pkg/gormx`、`pkg/promx`、`pkg/encoding/json` 无测试，`pkg/util` 2%。

- **[部署] 日志目录 0777 且路径相对 cwd 而非 WorkDir**
  `logging/init.go:77`、`logging.toml:8`；`Badger.Path`（`server.toml:37`、`middleware.toml:43`）、`DB.DSN`（`server.toml:49`）同理。

---

## 可选

- 刷新 token 不吊销旧 token，默认 24 小时有效，黑名单默认 memory 重启即失效：`login.biz.go:203-220`、`config/middleware.go:41,43`。
- `DestroyToken` 对无 exp 的 token 空指针：`jwtx/jwt.go:156`，可加 `jwt.WithExpirationRequired()`。
- `Auth.Disable=true` 时所有请求都是 root：`login.biz.go:34-37`，文档应标明仅限本地调试。
- 静态目录无 index.html 时输出目录列表：`static.go:22-28`。路径穿越已被 `net/http` 的 `..` 检查挡住。
- LIKE 通配符未转义：`user.dal.go:31,34`、`role.dal.go:34`、`menu.dal.go:35`、`logger.dal.go:38,44`。
- `X-Request-Id` 无长度限制进日志：`trace.go:37`。Recovery 只遮 Authorization 不遮 Cookie：`recover.go:40-49`。
- dev CORS 模板带 `AllowFiles=true`、`AllowOrigins=["*"]`：`middleware.toml:6-14`；`cors.go:42-45` 的 `DefaultCORSConfig` 无人使用。
- 文件权限 0666/0777：`start.go:68,89`、`gorm.go:54`、`init.go:77`。
- `parse.go:36-39` 用 `strings.Contains` 判扩展名，`.tom` 也匹配；`:65-72` 目录按字典序合并无文档；不支持 yaml 配置（`pkg/encoding/yaml` 存在但未用）。
- Go 1.27 现代化汇总：33 处 `interface{}` 可换 `any`；7 处 `err == x` 应改 `errors.Is`（`util/db.go:108`、`cachex/badger.go:78,99`、`redis.go:87,151`、`login.biz.go:50,111`）；`cachex/badger.go:49-60` 伪造 string header 的 unsafe 写法应换 `unsafe.String/Slice`，且 Iterator 返回的 key 别名了 badger 内部缓冲；`atomic.AddInt32/LoadInt32` 可换 `atomic.Int32`；`wire.go:2`、`wire_gen.go:5` 的 `+build` 行冗余；`go.mod` 应加 `tool` 指令。
- `ResJSON` 序列化失败直接 panic：`gin.go:74`；`:101-104` 用反射判 nil。dal 用 `nil, nil` 表示未找到，biz 每处都要判空。
- 缓存与同步副作用写在事务闭包里，提交失败时缓存已改：`user.biz.go:171,191`、`role.biz.go:99,149,173`、`menu.biz.go:457,496`。
- 日志 gorm hook 另开一条连接（`bootstrap/logger.go:21-30`），SQLite 下有锁冲突风险；`logging.toml:19-26` 与 `server.toml:45-59` 重复配置同一库。
- 生成物入库但无 CI 校验同步；`go:generate` 只写在 `wire_gen.go:3` 生成文件里，swag 无 directive。
- `scripts/*.sh` 无 shebang、不可执行、全部相对 cwd，`dist` 目录不在仓库。
- `util/id_test.go:8-14` 零断言；`rand_test.go:23` `d > 10` 应为 `d > 9`。
- `main.go:31-34` 参数错误 `panic` 而非退出码。
- `go.mod` 中 `json-iterator/go` 仅为 gin 的间接依赖，非本仓库直接引用。
