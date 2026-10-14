# gin-admin

基于 Gin、GORM、Casbin 和 Wire 的 RBAC 后台管理服务端，作为多个项目的通用底座。提供登录与验证码、用户、角色、菜单与接口权限、操作日志等功能。

## 来源与许可

本项目基于 [LyricTian/gin-admin](https://github.com/LyricTian/gin-admin) v10（Copyright 2023 LyricTian）二次开发，沿用其 [Apache License 2.0](LICENSE)。感谢原作者的工作。

相对上游的改动概要见下一节，每项改动的原因与验证方式见 [AUDIT.md](AUDIT.md) 和 git 提交记录（基线提交为 `5ce7269`）。

## 相对上游的改动

**依赖与构建**
- 要求 Go 1.27.2；依赖升级到当时的最新次版本，badger 升至 v4、golang-jwt 升至 v5；SQLite 驱动改为纯 Go 实现，不再需要 cgo。
- 用标准库替换 `pkg/errors` 和 json-iterator；Redis 客户端统一为 go-redis v9；wire 版本由 `go.mod` 固定。
- 移除 Prometheus 集成，以及未使用的 oss、mail 包。

**安全**
- 去掉内置的 JWT 密钥和默认口令；生产模式下拒绝已公开的默认值。
- 登录连续失败锁定账号名；错误提示与响应时间不暴露账号是否存在。
- 改密码、管理员设置或重置密码后吊销该用户的旧 token；刷新 token 时作废旧 token。
- 非 root 管理员只能授予、管理不超过自身权限范围的角色和用户。
- 被冻结、删除或撤权的用户不再能通过缓存继续访问。
- 替换验证码库自带的内存存储，修复未登录即可让所有人无法登录的死锁；验证码可存入 Redis。
- 日志不再记录请求体、响应体和 token；打印配置时遮蔽密码与密钥。
- gzip 请求解压后同样受大小限制；默认不信任代理转发的客户端 IP；pprof 改用独立路由；Swagger 默认关闭。
- 安全响应头、请求头超时；更新到无已知可达漏洞的依赖版本。

**权限与数据**
- Casbin 策略改为在内存中加载，不再写文件；撤权立即生效，同一秒内的多次变更不再丢失，加载失败时保留原策略。
- 修复所有非 root 用户被拒绝的策略格式问题、PostgreSQL 下日志查询报错、关闭日志 hook 后日志页面报错。
- 分页参数增加下限，`pageSize=0` 不再返回整张表；日志表支持按天数自动清理。

**配置与运维**
- 新增生产配置样例 `configs/prod`；配置值可引用环境变量（`${NAME}`），启动时校验配置并一次列出所有问题。
- 端口占用、证书错误时启动失败而非进程假活；MySQL 建库保留原连接串参数（支持 unix socket、TLS）。
- 新增就绪检查 `/health/ready`；记录 SQL 错误与慢查询；日志 caller 位置修正；日志 hook 停机安全。
- 新增 systemd 服务、环境变量模板与 nginx 示例。

**测试与工程化**
- 新增覆盖全部接口的端到端测试，可针对 SQLite、MySQL、PostgreSQL 运行；新增 GitHub Actions CI。

**行为变化（升级时注意）**
- 刷新 token 后旧 token 立即失效；改密码后当前会话也需重新登录。
- 会话自密码登录起最长 7 天（`Middleware.Auth.MaxSessionHours`），之后刷新返回 401（`com.session.expired`），需重新登录。
- 登录锁定返回 429（`com.login.locked`），越权的管理操作返回 403（`com.rbac.privilege-escalation`）。
- 未配置 `DefaultLoginPwd` 时，新建用户必须填写密码，重置密码不可用。
- 只有 root 能重置他人密码或在编辑用户时设置新密码，其他管理员返回 403（`com.rbac.root-only`）。
- 用户名、角色编码、同一父菜单下的菜单编码在数据库中唯一；重复时返回 409（原为 400）。已有数据若存在重复，升级后首次启动会报错退出，需先清理重复行。
- 用户冻结状态的值由 `freezed` 改为 `frozen`，前端提交和显示需同步修改。旧数据中的 `freezed` 用户仍无法登录（只有 `activated` 可以登录），编辑时需改为新值。
- 生产模式下 root 密码必须是 bcrypt 哈希（`ginadmin hash-password` 生成），不再接受 MD5；启动命令必须指定 `-c`。
- 配置项变化：新增 `Storage.Cache.KeyPrefix`（生产样例为 `ginadmin:`）、`MaxSessionHours`、`TrustedProxies`、`LoginLock`、`ReadHeaderTimeout`、`MaxHeaderBytes`、`LoggerRetentionDays`；删除 `GenPolicyFile`、Casbin 的 `AutoLoadInterval` 和 `LoadThread`、日志请求体长度相关配置和 Prometheus 配置；`DisableSwagger` 默认改为 true。

## 环境要求

| 组件 | 版本 | 说明 |
|---|---|---|
| Go | 1.27.2 及以上 | `go.mod` 已指定工具链，较旧的 Go 会自动下载 |
| MySQL 或 PostgreSQL | MySQL 8.0+ / PostgreSQL 14+ | 生产环境不要使用 SQLite |
| Redis | 6.2+ | 验证码使用 `GETDEL`，需要 6.2 及以上 |

程序为纯 Go 实现，不依赖 cgo，可以直接交叉编译。

## 本地开发

```
go run . start -d configs -c dev
```

dev 配置使用 SQLite 和内存缓存，开箱即用：

- 监听 `:8040`，root 账号为 `admin`，密码为 `abc-123`（前端发送它的 MD5）。
- JWT 密钥为空时每次启动随机生成，重启后需要重新登录。
- Swagger 在 `/swagger/index.html`。

托管前端静态文件：`go run . start -d configs -c dev -s /path/to/dist`。

## 生产部署

以下步骤已在 Ubuntu 26.04 上按 `deploy/` 目录的文件实际验证。部署之后的备份、恢复、监控、升级和密钥轮换见 [deploy/OPERATIONS.md](deploy/OPERATIONS.md)。

### 1. 构建

```
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w -X main.VERSION=$(git rev-parse --short HEAD)" -o ginadmin .
```

把 `ginadmin` 和 `configs/` 目录复制到服务器的 `/opt/ginadmin/`，并创建 `/opt/ginadmin/data/`。

### 2. 准备数据库与 Redis

- 为应用创建独立的数据库和账号，账号需要在该库上建表的权限（首次启动会自动建表）。
- Redis 只监听内网或本机地址。
- 数据库与 Redis 都不要对公网开放。

### 3. 配置环境变量

`configs/prod` 中的密钥和地址全部来自环境变量，缺少必填变量时程序会列出缺失的变量和对应配置项并拒绝启动。

```
sudo mkdir -p /etc/ginadmin
sudo cp deploy/ginadmin.env.example /etc/ginadmin/ginadmin.env
sudo chmod 600 /etc/ginadmin/ginadmin.env
```

| 变量 | 必填 | 说明 |
|---|---|---|
| `GINADMIN_DB_DSN` | 是 | 数据库连接串，建议带超时参数，示例见 `ginadmin.env.example` |
| `GINADMIN_REDIS_ADDR` | 是 | Redis 地址 |
| `GINADMIN_JWT_KEY` | 是 | 至少 32 字节，可用 `openssl rand -hex 32` 生成 |
| `GINADMIN_ROOT_PASSWORD` | 是 | root 密码的 bcrypt 哈希，用 `printf '%s' '密码' \| ./ginadmin hash-password` 生成；为空则禁用 root 登录 |
| `GINADMIN_DB_TYPE` | 否 | `mysql`（默认）或 `postgres` |
| `GINADMIN_HTTP_ADDR` | 否 | 监听地址，默认 `:8040`；放在 nginx 后面时建议 `127.0.0.1:8040` |
| `GINADMIN_REDIS_USERNAME`、`GINADMIN_REDIS_PASSWORD` | 否 | Redis 认证 |
| `GINADMIN_DEFAULT_LOGIN_PWD` | 否 | 新建用户未填密码和重置密码时使用的密码（MD5）；为空则这两项功能不可用 |
| `GINADMIN_METRICS_ADDR` | 否 | Prometheus 指标地址，默认 `127.0.0.1:8041`（`/metrics`）；为空则关闭。不要暴露到公网 |

配置文件中任意字符串值都可以写成 `${NAME}` 或 `${NAME:-默认值}` 来引用环境变量。

### 4. 安装 systemd 服务

```
sudo useradd --system --no-create-home --shell /usr/sbin/nologin ginadmin
sudo chown ginadmin:ginadmin /opt/ginadmin/data
sudo cp deploy/ginadmin.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now ginadmin
```

日志以 JSON 输出到 journal：`journalctl -u ginadmin -f`。

### 5. 反向代理

参考 `deploy/nginx.conf.example`。**放在代理后面时，必须在 `configs/prod/server.toml` 中把代理地址加入 `General.HTTP.TrustedProxies`**，否则所有客户端都会被识别为代理的 IP，共用同一个限流额度。

### 健康检查

| 路径 | 用途 |
|---|---|
| `/health` | 存活检查：进程正常即返回 200 |
| `/health/ready` | 就绪检查：数据库和 Redis 均可用时返回 200，否则 503；负载均衡应使用这个 |

### 部署模型与容量

- 按每台服务器一个实例设计和测试，不需要也不支持同时启动多个实例初始化同一个数据库。
- 即使只有一个实例，也应使用 Redis：登出黑名单、改密码后的 token 吊销、登录失败计数和验证码都存在缓存中，Redis 在重启后保留它们；改用内存缓存则重启后已吊销的 token 会重新有效。
- 一个实例占用的数据库连接数为 `Storage.DB.MaxOpenConns` 加日志 hook 的 `MaxOpenConns`（样例为 25 + 4），要小于数据库的 `max_connections`（MySQL 默认 151，PostgreSQL 默认 100）。
- 多个应用或环境共用一个 Redis 库时，为每个设置不同的 `Storage.Cache.KeyPrefix`，避免键冲突。

## 安全机制

- **认证**：JWT（HS512）。登出、改密码、管理员设置或重置密码都会使相关 token 失效；刷新 token 时旧 token 作废；会话自登录起最长 7 天，刷新不能延长。
- **登录防护**：图形验证码；同一账号名连续失败 5 次锁定 15 分钟（`General.LoginLock`），不存在的账号名同样计数；错误提示和响应时间不暴露账号是否存在。
- **授权**：Casbin 按"角色 → 菜单 → 接口"控制访问；角色或菜单变更提交后立即在进程内重建策略，下一个请求即生效。非 root 管理员只能授予和管理不超过自身菜单范围的角色与用户，不能通过用户管理修改自己。
- **root 账号**：来自配置而非数据库，不受权限控制。生产环境应使用强密码，必要时把 `GINADMIN_ROOT_PASSWORD` 设为空以禁用。
- **其他**：限流（按 IP 与按用户）、请求体与 gzip 解压大小限制、安全响应头、日志中的密码与 token 脱敏、操作日志保留 90 天。

- **修改他人密码**：只有 root 可以重置密码或在编辑用户时设置新密码，由代码保证，与 `menu.json` 的配置无关（返回 403 `com.rbac.root-only`）。其他管理员创建用户时仍可设置初始密码；用户可以修改自己的密码。

## 测试

```
go test ./...                 # 单元测试与端到端测试（SQLite、内存缓存）
go test -race ./...           # 同上，开启竞态检测
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run ./...   # 静态检查，规则见 .golangci.yml
```

`internal/mods/` 之外的导出标识符必须有文档注释；新模块的 api、biz、dal、schema 层不作要求。

针对真实数据库和 Redis 运行端到端测试：

```
E2E_DB_TYPE=mysql \
E2E_DB_DSN='user:pass@tcp(127.0.0.1:3306)/ginadmin?charset=utf8mb4&parseTime=True&loc=Local' \
E2E_REDIS_ADDR=127.0.0.1:6379 \
go test -count=1 ./test/e2e
```

GitHub Actions（`.github/workflows/ci.yml`）会在每次推送和 PR 时运行格式检查、vet、golangci-lint、竞态测试、漏洞扫描，以及针对 MySQL、PostgreSQL 和 Redis 的端到端测试。

## 代码生成

```
go generate ./internal/wirex    # 依赖注入代码（wire 版本由 go.mod 的 tool 指令固定）
go install github.com/swaggo/swag/cmd/swag@latest   # API 文档生成工具
swag init --parseDependency --generalInfo ./main.go --output ./internal/swagger
```

## 目录结构

```
cmd/                 命令行入口（start、stop、version、hash-password）
configs/             配置：dev、prod 样例，菜单定义，Casbin 模型
deploy/              systemd 服务、环境变量模板、nginx 示例、运维手册
internal/bootstrap/  启动流程与 HTTP 服务
internal/config/     配置结构、加载、环境变量展开、校验
internal/mods/rbac/  RBAC 模块：api、biz、dal、schema
internal/wirex/      依赖注入
pkg/                 可复用组件：缓存、验证码存储、JWT、日志、中间件、数据库等
test/e2e/            端到端测试
```
