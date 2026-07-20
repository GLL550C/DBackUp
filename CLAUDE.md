# CLAUDE.md

此文件为 Claude Code（claude.ai/code）在此仓库中工作时提供指导。

## 常用命令

```bash
# 当前平台构建（去除调试符号，注入 git 版本号）
make build          # → services
go build -ldflags "-s -w -X main.version=$(git describe --tags --always)" -o services .

# 交叉编译
make linux          # → services-linux (amd64)
make arm64          # → services-linux-arm64
make mac            # → services-mac (amd64)
make windows        # → services.exe (amd64)
make all            # → 三个桌面平台

# 开发运行
make run            # 或 `go run .`

# 清理构建产物
make clean
```

项目中**没有任何测试**（不存在 `_test.go` 文件）。

## 架构

**模块路径：** `db-backup-tool`（Go 1.25）。单文件部署：Go 后端 + 内嵌 Web 界面 + SQLite，无外部运行时依赖。

### 包结构

```
main.go              → 组装一切，启动服务器，处理优雅关闭
embed.go             → //go:embed 指令，嵌入 web/templates/* 和 web/static/*
internal/api/        → Gin HTTP 路由，页面处理器 + REST API 处理器，CORS 中间件
internal/auth/       → HMAC-SHA256 会话令牌，内存会话存储，登录中间件
internal/backup/     → Engine 接口，MySQL/PostgreSQL 引擎，备份编排，SFTP 上传
internal/config/     → YAML 配置加载/保存，默认值，首次运行时自动创建 config.yaml
internal/crypto/     → AES-256-GCM 加密/解密，用于存储凭据
internal/database/   → 通过 modernc.org/sqlite 访问 SQLite（无 CGO），版本化迁移，完整 CRUD
internal/models/     → 独立的 model 结构体（与 database 包中的 row 类型有冗余）
internal/scheduler/  → robfig/cron v3 封装，Cron 表达式规范化，任务到 cron entry 的映射
```

### 启动流程

1. 确定 `data/` 目录（优先可执行文件同级，回退到 `./data`）
2. `config.Load()` — 读取 `config.yaml`，不存在则用默认值自动创建
3. `crypto.Init()` — 若配置中没有密钥，生成随机 AES-256 密钥并回存
4. `auth.Init()` — 设置密码，若为空或看起来像 hex 密钥则防御性重置为 `"admin"`
5. `database.Init()` — 以 WAL 模式 + 外键约束打开 SQLite，执行版本化迁移
6. `backup.NewManager()` — 创建备份编排器
7. `scheduler.Start()` — 从数据库加载所有已启用任务，注册 cron 条目
8. Gin 路由在 `:8080` 启动（可配置），监听 SIGINT/SIGTERM 实现优雅关闭

### Engine 接口（添加新数据库类型）

定义在 [internal/backup/engine.go](internal/backup/engine.go)。要添加新数据库支持：

```go
type Engine interface {
    Dump(w io.Writer) error
    DumpDatabase(w io.Writer, dbName string) error
    TestConnection() error
    ListDatabases() ([]string, error)
    Close() error
    DBType() string
    ValidateParams(params map[string]interface{}) error
}
```

实现全部 7 个方法，然后在 `manager.go` 的 `ExecuteBackup()` switch 语句中注册新类型。参数会通过 `engine.go` 中的已知参数映射表（`validMySQLParams`、`validPGParams`）进行校验。

### 备份执行流程

1. `Manager.ExecuteBackup()` 从 SQLite 加载任务和连接，解密凭据
2. 在 `backup_records` 表中创建一条状态为 `"running"` 的记录
3. 在 goroutine 中启动 `doBackup()`（非阻塞）：
   - 创建 Engine → 通过 gzip.Writer 导出到临时文件
   - 根据任务配置复制到本地目录 和/或 通过 SFTP 上传
   - 调用 `enforceRetention()` — 保留最近 N 个成功的备份，删除旧文件（本地 + 远程）
   - 将记录状态更新为 `"success"` 或 `"failed"`
4. Cron 调度器调用的是同一个 `ExecuteBackup()`

### 数据库层

- SQLite，通过 `modernc.org/sqlite` 驱动（纯 Go，无 CGO）。单连接（`SetMaxOpenConns(1)`）。
- 单例模式：`Init()` 使用 `sync.Once`，`GetDB()` 返回全局的 `*sql.DB`。
- 版本化迁移：`database.go` 中按序号排列的迁移步骤，仅在 `schema_version < migration.version` 时执行。
- 级联删除：`connections → backup_tasks → backup_records`。

### 安全模型（关键——不可破坏）

- **AES-256-GCM** 加密所有敏感存储字段（数据库密码、SFTP 密码、SSH 密钥）。加密密钥以 hex 格式存储在 `config.yaml` 中。
- **密码掩码**：API 响应中对敏感字段返回 `"******"`。`crypto.MustEncrypt()` 对空值或已掩码值静默返回明文。
- **HMAC-SHA256 会话**：24 小时有效期，内存存储（重启 = 所有会话失效），HttpOnly + SameSite Cookie。
- **认证中间件** 跳过 `/login`、`/api/login` 和 `/static/*`。API 调用返回 401 JSON；页面请求重定向到 `/login`。
- **SSH 主机密钥校验**：当前使用 `ssh.InsecureIgnoreHostKey()` — 这是一个已知的安全取舍。

### Web 前端

所有前端资源位于 `web/` 目录，通过 `embed.go` 中的 `//go:embed` 嵌入二进制文件。模板使用 Go 的 `html/template`，采用布局注入模式：内容模板渲染为 `{{.contentHTML}}`，然后注入到 `layout.html` 中。不使用 JavaScript 框架——`web/static/js/app.js` 使用原生 JS（UI 使用 Bootstrap 5.3）。

### 配置

`config.yaml` 首次运行时在可执行文件同级的 `data/` 目录下自动创建。关键字段：`server.port`、`backup.default_dir`、`logging.retention_days`、`auth.password`、`security.encrypt_key`。文件权限设为 `0600`。

### Cron 表达式

4 字段表达式自动规范化为 5 字段（追加 `*` 作为 day-of-week）。调度器使用 5 字段解析器：分钟、小时、日、月、星期。任务通过数据库 `id` 在 `map[int64]cron.EntryID` 映射中标识。
