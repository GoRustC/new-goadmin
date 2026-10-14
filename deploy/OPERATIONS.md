# 运维手册

本文档面向按 README "生产部署"一节部署的单实例服务：systemd 运行 `/opt/ginadmin/ginadmin`，MySQL 或 PostgreSQL，Redis，前面是 nginx。文中的备份与恢复命令已在 Ubuntu 26.04、MySQL 8.4、PostgreSQL 18 上实际执行并核对过行数。

## 1. 备份

### 要备份什么

| 数据 | 重要性 | 丢失的后果 |
|---|---|---|
| 数据库 | 必须备份 | 用户、角色、菜单和操作日志全部丢失 |
| `/etc/ginadmin/ginadmin.env` | 必须备份，单独加密保存 | 丢失 JWT 密钥后所有 token 失效；丢失 root 密码哈希需重新生成 |
| `/opt/ginadmin/configs/` | 建议纳入版本管理 | 可从代码仓库恢复 |
| Redis | 开启持久化即可，无需单独备份 | 丢失后：已登出或已吊销的 token 在过期前（最长 24 小时）重新有效，登录锁定计数清零，验证码失效 |

### 数据库备份

MySQL（`--single-transaction` 不锁表，可在服务运行时执行；`--no-tablespaces` 让普通账号无需 PROCESS 权限）：

```
# 凭据放在 600 权限的文件中，不要写在命令行里
cat > /root/.ginadmin-my.cnf <<'EOF'
[client]
user=ginadmin
password=数据库密码
host=127.0.0.1
EOF
chmod 600 /root/.ginadmin-my.cnf

mysqldump --defaults-extra-file=/root/.ginadmin-my.cnf --single-transaction --no-tablespaces --routines ginadmin \
  | gzip > /var/backups/ginadmin/ginadmin-$(date +%F).sql.gz
```

PostgreSQL（自定义格式，压缩且支持选择性恢复；密码放在 `~/.pgpass`）：

```
pg_dump -h 127.0.0.1 -U ginadmin -Fc ginadmin > /var/backups/ginadmin/ginadmin-$(date +%F).dump
```

建议用 cron 或 systemd timer 每天执行一次，保留最近 14 天，并把备份复制到另一台机器或对象存储。只放在同一台服务器上的备份，在磁盘故障时一起丢失。

操作日志表（`logger`）通常占数据库的绝大部分。生产样例保留 90 天（`Dictionary.LoggerRetentionDays`），备份大小随之稳定。

### Redis 持久化

Ubuntu 默认只有 RDB 定时快照，宕机时最多丢失数分钟的数据。建议开启 AOF，最多丢失 1 秒：

```
# /etc/redis/redis.conf
appendonly yes
appendfsync everysec
```

修改后执行 `sudo systemctl restart redis-server`。

## 2. 恢复

先恢复到一个新库核对，确认无误后再切换，不要直接覆盖正在使用的库。

MySQL：

```
mysql -e "CREATE DATABASE ginadmin_restore CHARACTER SET utf8mb4"
gunzip -c ginadmin-2026-10-14.sql.gz | mysql --defaults-extra-file=/root/.ginadmin-my.cnf ginadmin_restore
```

PostgreSQL：

```
createdb -O ginadmin ginadmin_restore
pg_restore -h 127.0.0.1 -U ginadmin -d ginadmin_restore --no-owner ginadmin-2026-10-14.dump
```

核对各表行数后，二选一：

- 把 `GINADMIN_DB_DSN` 指向 `ginadmin_restore`，执行 `sudo systemctl restart ginadmin`。
- 或停止服务，删除原库，把备份恢复到同名库，再启动服务。

**至少每季度做一次恢复演练**：没有恢复过的备份，不能算作备份。

## 3. 监控与告警

| 检查 | 方法 | 告警条件 |
|---|---|---|
| 服务可用 | 每 30 秒请求 `/health/ready` | 连续 2 次非 200 |
| 进程状态 | `systemctl is-active ginadmin` | 不是 `active` |
| 错误日志 | `journalctl -u ginadmin -o cat \| grep '"level":"error"'` | 5 分钟内出现即通知 |
| 慢查询 | 日志中带 `"tag":"sql"` 的 warn | 持续出现说明需要加索引或扩容 |
| 磁盘 | 数据库和备份所在分区 | 使用率超过 80% |
| 数据库连接 | MySQL `SHOW STATUS LIKE 'Threads_connected'` | 接近 `max_connections` |
| 证书 | nginx 使用的 HTTPS 证书 | 30 天内到期 |

`/health/ready` 在数据库或 Redis 不可用时返回 503，而 `/health` 仍返回 200。外部监控应检查前者。

服务崩溃时 systemd 会在 5 秒后自动重启（`Restart=on-failure`）。若希望重启时收到通知，可在 unit 中加 `OnFailure=` 指向一个发送通知的服务。

## 4. 升级与回滚

```
# 1. 备份数据库（见第 1 节）
# 2. 保留当前版本，便于回滚
sudo cp /opt/ginadmin/ginadmin /opt/ginadmin/ginadmin.prev
# 3. 替换程序与配置，然后重启
sudo install -m 755 ginadmin /opt/ginadmin/ginadmin
sudo systemctl restart ginadmin
# 4. 确认
curl -s http://127.0.0.1:8040/health/ready
journalctl -u ginadmin -n 50 --no-pager
```

- 启动时自动建表（`Storage.DB.AutoMigrate`），只新增表和列，不删除列，因此回滚到旧版本通常可以直接运行。
- 回滚：`sudo cp /opt/ginadmin/ginadmin.prev /opt/ginadmin/ginadmin && sudo systemctl restart ginadmin`。如果新版本修改过数据格式，需同时恢复升级前的数据库备份。
- 重启期间（通常 1 秒内）请求会失败，建议在低峰时段操作。
- 升级前阅读 README 的"行为变化"一节，配置项或环境变量可能需要同步调整。

## 5. 密钥与密码轮换

### JWT 签名密钥

1. 生成新密钥：`openssl rand -hex 32`。
2. 在 `ginadmin.env` 中把旧值移到配置的 `Middleware.Auth.OldSigningKey`（可写成 `${GINADMIN_OLD_JWT_KEY}` 并新增该变量），`GINADMIN_JWT_KEY` 改为新值，重启。此时新旧密钥签发的 token 都有效。
3. 等待一个 token 有效期（默认 24 小时）后，去掉 `OldSigningKey` 再重启。

怀疑密钥泄露时跳过第 2 步的过渡期，直接更换并重启：所有用户需要重新登录。

### root 密码

```
printf '%s' '新密码' | /opt/ginadmin/ginadmin hash-password
```

把输出写入 `GINADMIN_ROOT_PASSWORD`，然后 `sudo systemctl restart ginadmin`。设为空即禁用 root 登录。

### 普通用户

用户自己改密码，或由 root 重置/设置新密码。两种方式都会使该用户已签发的 token 立即失效。

## 6. 容量参考

在 4 核、约 7GB 内存的虚拟机上，服务与 MySQL、Redis 同机部署，使用生产配置（含数据库操作日志）实测：

| 并发客户端 | 吞吐量 | 中位延迟 | 95% 延迟 | 99% 延迟 | 错误 |
|---|---|---|---|---|---|
| 50 | 约 1450 请求/秒 | 29ms | 74ms | 101ms | 0 |

测试混合了用户信息、菜单、用户列表、角色列表、菜单树和操作日志 6 个已鉴权接口，所有请求均写入操作日志表，无一丢失。瓶颈在网络与数据库 I/O，而非业务代码。

一千人左右的后台系统，高峰期通常在每秒几十到一两百个请求，单实例有一个数量级的余量。若吞吐量接近上限，优先把数据库迁到独立服务器，再考虑增加 CPU。
