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
- 生产模式下 root 密码必须是 bcrypt 哈希（`ginadmin hash-password` 生成），不再接受 MD5；启动命令必须指定 `-c`。
- 配置项变化：新增 `MaxSessionHours`、`TrustedProxies`、`LoginLock`、`ReadHeaderTimeout`、`MaxHeaderBytes`、`LoggerRetentionDays`；删除 `GenPolicyFile`、日志请求体长度相关配置和 Prometheus 配置；`DisableSwagger` 默认改为 true。

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

以下步骤已在 Ubuntu 26.04 上按 `deploy/` 目录的文件实际验证。

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

### 容量与多实例

- 每个实例占用的数据库连接数为 `Storage.DB.MaxOpenConns` 加日志 hook 的 `MaxOpenConns`（样例为 25 + 4）。所有实例合计要小于数据库的 `max_connections`（MySQL 默认 151，PostgreSQL 默认 100）。
- 多实例部署需要所有实例共用同一个 Redis：权限变更、登出、验证码和限流都依赖它。生产样例已全部使用 Redis。
- 首次部署请先启动一个实例完成建表和菜单导入，再启动其余实例，避免并发初始化。

## 安全机制

- **认证**：JWT（HS512）。登出、改密码、管理员设置或重置密码都会使相关 token 失效；刷新 token 时旧 token 作废；会话自登录起最长 7 天，刷新不能延长。
- **登录防护**：图形验证码；同一账号名连续失败 5 次锁定 15 分钟（`General.LoginLock`），不存在的账号名同样计数；错误提示和响应时间不暴露账号是否存在。
- **授权**：Casbin 按"角色 → 菜单 → 接口"控制访问，权限变更无需重启即生效。非 root 管理员只能授予和管理不超过自身菜单范围的角色与用户，不能通过用户管理修改自己。
- **root 账号**：来自配置而非数据库，不受权限控制。生产环境应使用强密码，必要时把 `GINADMIN_ROOT_PASSWORD` 设为空以禁用。
- **其他**：限流（按 IP 与按用户）、请求体与 gzip 解压大小限制、安全响应头、日志中的密码与 token 脱敏、操作日志保留 90 天。

已知限制：`configs/menu.json` 中没有任何菜单包含重置密码接口，目前只有 root 可以重置他人密码。如需开放，在对应菜单的 `resources` 中加入 `PATCH /api/v1/users/{id}/reset-pwd`。

## 测试

```
go test ./...                 # 单元测试与端到端测试（SQLite、内存缓存）
go test -race ./...           # 同上，开启竞态检测
```

针对真实数据库和 Redis 运行端到端测试：

```
E2E_DB_TYPE=mysql \
E2E_DB_DSN='user:pass@tcp(127.0.0.1:3306)/ginadmin?charset=utf8mb4&parseTime=True&loc=Local' \
E2E_REDIS_ADDR=127.0.0.1:6379 \
go test -count=1 ./test/e2e
```

GitHub Actions（`.github/workflows/ci.yml`）会在每次推送和 PR 时运行格式检查、vet、竞态测试、漏洞扫描，以及针对 MySQL、PostgreSQL 和 Redis 的端到端测试。

## 代码生成

```
go generate ./internal/wirex    # 依赖注入代码（wire 版本由 go.mod 的 tool 指令固定）
go install github.com/swaggo/swag/cmd/swag@latest   # API 文档生成工具
swag init --parseDependency --generalInfo ./main.go --output ./internal/swagger
```

## 目录结构

```
cmd/                 命令行入口（start、stop、version）
configs/             配置：dev、prod 样例，菜单定义，Casbin 模型
deploy/              systemd 服务、环境变量模板、nginx 示例
internal/bootstrap/  启动流程与 HTTP 服务
internal/config/     配置结构、加载、环境变量展开、校验
internal/mods/rbac/  RBAC 模块：api、biz、dal、schema
internal/wirex/      依赖注入
pkg/                 可复用组件：缓存、验证码存储、JWT、日志、中间件、数据库等
test/e2e/            端到端测试
```
