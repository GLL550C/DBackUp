# DB Backup Tool（数据库备份工具）

一个基于 Go 开发的轻量级数据库自动备份工具，内置 Web 管理界面，支持 MySQL 和 PostgreSQL 的定时备份、远程传输、备份保留策略等功能。

## 背景

在日常运维工作中，数据库备份是最基础也是最关键的一环。市面上的备份方案要么过于笨重（需额外安装 Agent、配置复杂），要么功能过于单一（仅支持本地命令行脚本）。DB Backup Tool 的设计目标是提供一款**开箱即用、功能完善**的数据库备份工具，具备以下特性：

- **单文件部署**：Go 编译为单个可执行文件，无需依赖任何运行时环境
- **内嵌 Web 界面**：所有前端资源编译进二进制文件，无需额外的 Web 服务器
- **SQLite 数据存储**：不依赖外部数据库，开箱即用
- **中文界面**：管理后台使用简体中文，操作直观友好

## 核心功能

### 1. 多数据库支持
- **MySQL**：支持全库/指定库备份，可配置导出参数（存储过程、触发器、锁表策略、批量插入等）
- **PostgreSQL**：支持全库/指定库备份，可配置 schema 过滤、表过滤、DDL/CLEAN 选项、批量插入行数等

### 2. 灵活的存储策略
- **仅本地**：备份文件保存在本地指定目录
- **仅远程**：通过 SFTP 上传至远程服务器，本地不保留
- **本地 + 远程**：同时保存本地和远程两份备份

### 3. 定时调度
- 支持标准 5 字段 Cron 表达式
- 自动补全：输入 4 字段 Cron 时自动补齐为 5 字段
- 每次调度按顺序执行，避免重复运行
- 预设快捷选项：每 6 小时、每天凌晨 2 点、每周日凌晨 3 点

### 4. 备份保留策略
- 按数量保留：自动清理超出限制的旧备份文件（含远程文件）
- 按天数保留：按日志保留天数清理旧记录

### 5. 安全机制
- **登录认证**：基于 HMAC-SHA256 的 Session 机制，24 小时有效期
- **AES-256-GCM 加密**：数据库密码、SFTP 密码、SSH 密钥等敏感信息加密存储
- **SSH 密钥认证**：远程传输支持密码和 SSH 私钥两种认证方式
- **密码掩码**：API 返回时敏感字段自动替换为 `******`

### 6. 备份压缩
- 所有备份文件经过 Gzip 压缩，大幅节省存储空间
- 文件名格式：`{数据库类型}_{数据库名}_{时间戳}.sql.gz`

### 7. Web 管理界面
- **仪表盘**：任务总数、成功率、最近备份记录等概览统计
- **数据库连接**：增删改查、连接测试、数据库列表查询
- **备份任务**：创建/编辑/删除任务、立即执行、启用/禁用定时
- **备份历史**：分页查看、按任务筛选、文件下载、批量删除
- **系统设置**：服务端口、默认备份目录、日志保留天数

### 8. 批量操作
- 支持批量删除数据库连接（级联删除关联任务）
- 支持批量删除备份任务
- 支持批量删除备份记录及其文件

## 技术架构

```
├── main.go                  # 应用入口，编排各模块生命周期
├── embed.go                 # Go embed 指令，内嵌前端资源
├── config.yaml              # YAML 配置文件（自动生成）
├── Makefile                 # 跨平台构建脚本
│
├── internal/
│   ├── api/                 # HTTP API 层
│   │   ├── handlers.go      # 所有路由处理器（页面 + REST API）
│   │   └── middleware.go     # CORS、请求日志中间件
│   ├── auth/                # 认证模块
│   │   └── auth.go          # 登录/登出/Session/中间件
│   ├── backup/              # 备份引擎
│   │   ├── engine.go        # Engine 接口定义 + 参数校验
│   │   ├── manager.go       # 备份编排（创建引擎→导出→压缩→存储→清理）
│   │   ├── mysql.go         # MySQL 备份实现（mysqldump 逻辑）
│   │   ├── postgresql.go    # PostgreSQL 备份实现（pg_dump 逻辑）
│   │   └── sftp.go          # SFTP 客户端（上传/删除/递归创建目录）
│   ├── config/              # 配置管理
│   │   └── config.go        # YAML 配置读写，默认值
│   ├── crypto/              # 加密模块
│   │   └── crypto.go        # AES-256-GCM 加解密
│   ├── database/            # 数据持久层
│   │   └── database.go      # SQLite 初始化、版本化迁移、完整 CRUD
│   ├── models/              # 数据模型
│   │   └── models.go        # DBConnection, BackupTask, BackupRecord 等
│   └── scheduler/           # 定时调度器
│       └── scheduler.go     # Cron 任务管理、表达式校验和规范化
│
├── web/
│   ├── templates/           # Go 模板（服务端渲染）
│   │   ├── layout.html      # 主布局（侧边栏 + 顶栏）
│   │   ├── index.html       # 仪表盘页面
│   │   ├── connections.html # 数据库连接管理
│   │   ├── tasks.html       # 备份任务管理
│   │   ├── history.html     # 备份历史记录
│   │   ├── settings.html    # 系统设置
│   │   └── login.html       # 登录页（独立布局）
│   └── static/
│       ├── css/style.css    # 样式（CSS 变量、响应式布局）
│       └── js/app.js        # 前端逻辑（原生 JS，无框架依赖）
│
├── db/
│   ├── schema.sql           # 完整表结构文档
│   └── migrations.sql       # 迁移历史记录
│
└── data/                    # 运行时数据目录（自动创建）
    └── backup_tool.db       # SQLite 数据库文件
```

### 技术栈

| 组件 | 技术选型 | 说明 |
|------|---------|------|
| 语言 | Go 1.25 | 编译为单文件，跨平台 |
| Web 框架 | Gin v1.12 | 高性能 HTTP 路由 |
| 数据库 | SQLite (modernc.org/sqlite) | 纯 Go 实现，零 CGO 依赖 |
| MySQL 驱动 | go-sql-driver/mysql | 原生 Go MySQL 驱动 |
| PostgreSQL 驱动 | pgx v5 | 高性能纯 Go PG 驱动 |
| 定时调度 | robfig/cron v3 | 标准 Cron 调度器 |
| SSH/SFTP | golang.org/x/crypto + pkg/sftp | 远程文件传输 |
| 前端框架 | Bootstrap 5.3 + 原生 JS | 无框架依赖，极致轻量 |
| 配置格式 | YAML (gopkg.in/yaml.v3) | 人类可读 |

### 数据模型

```
connections  1 ──< N  backup_tasks  1 ──< N  backup_records
（数据库连接）      （备份任务）            （备份记录）
```

- 删除连接时级联删除关联的备份任务
- 删除任务时级联删除关联的备份记录

## 快速开始

### 环境要求

- Go 1.25+（仅编译时需要）
- 目标数据库：MySQL 5.7+ 或 PostgreSQL 12+

### 编译

```bash
# 编译当前平台
make build

# 编译所有平台（Windows / Linux / macOS）
make all

# 仅编译 Windows
make windows

# 仅编译 Linux
make linux

# ARM64 Linux
make arm64
```

编译产物：
- `db-backup-tool` — macOS / Linux（当前平台）
- `db-backup-tool.exe` — Windows
- `db-backup-tool-linux` — Linux x86_64
- `db-backup-tool-linux-arm64` — Linux ARM64
- `db-backup-tool-mac` — macOS x86_64

### 运行

```bash
# 直接运行（开发模式）
make run
# 或
go run .

# 使用编译好的二进制
./db-backup-tool-linux
```

首次运行会自动：
1. 在程序所在目录创建 `data/` 文件夹和 SQLite 数据库
2. 生成 `config.yaml` 配置文件
3. 生成 AES-256 加密密钥

启动后访问：**http://localhost:8080**

默认登录密码：**admin**（可在 config.yaml 中修改）

### 配置说明

配置文件 `config.yaml`（与可执行文件同目录）：

```yaml
server:
    port: 8080              # Web 服务端口

backup:
    default_dir: /opt/gll   # 默认备份目录（任务未指定时使用）

logging:
    retention_days: 30      # 日志保留天数

auth:
    password: admin         # 登录密码（明文）

security:
    encrypt_key: b643...    # AES-256 密钥（64 位十六进制，自动生成）
```

### 使用流程

1. **添加数据库连接** — 填写 MySQL/PostgreSQL 的主机、端口、用户名、密码，支持连接测试
2. **创建备份任务** — 选择数据库连接，指定备份范围（全库/指定库），配置存储方式（本地/远程/两者）、Cron 定时表达式
3. **手动或自动执行** — 可随时点击"立即执行"手动触发，或等待 Cron 定时自动运行
4. **查看备份历史** — 在历史页面查看执行状态、下载备份文件、清理过期记录
5. **管理远程备份** — 远程文件通过 SFTP 自动上传，清理策略同时覆盖本地和远程

## 备份参数说明

### MySQL 支持的自定义参数

| 参数 | 类型 | 说明 |
|------|------|------|
| `no_data` | bool | 仅导出表结构，不含数据 |
| `no_create_info` | bool | 仅导出数据，不含建表语句 |
| `skip_lock_tables` | bool | 跳过锁表操作 |
| `single_transaction` | bool | 使用事务一致性快照 |
| `routines` | bool | 导出存储过程和函数 |
| `triggers` | bool | 导出触发器 |
| `add_drop_table` | bool | 在 CREATE TABLE 前添加 DROP TABLE |
| `add_drop_database` | bool | 在 CREATE DATABASE 前添加 DROP DATABASE |
| `hex_blob` | bool | 二进制字段使用十六进制表示 |
| `complete_insert` | bool | INSERT 语句包含列名 |
| `extended_insert` | bool | 使用批量 INSERT 语句优化 |
| `ignore_tables` | array | 需要跳过的表名列表 |
| `where` | string | 导出数据的 WHERE 条件 |

### PostgreSQL 支持的自定义参数

| 参数 | 类型 | 说明 |
|------|------|------|
| `schema_only` | bool | 仅导出结构，不含数据 |
| `data_only` | bool | 仅导出数据，不含结构 |
| `clean` | bool | 在创建对象前添加 DROP 语句 |
| `if_exists` | bool | DROP 时使用 IF EXISTS |
| `create` | bool | 包含 CREATE DATABASE 语句 |
| `no_owner` | bool | 不导出对象所有者 |
| `no_comments` | bool | 不导出注释 |
| `no_tablespaces` | bool | 不导出表空间 |
| `rows_per_insert` | number | 批量插入行数 |
| `exclude_table` | array | 排除的表名列表 |
| `exclude_schema` | array | 排除的 Schema 列表 |
| `include_schema` | array | 仅包含的 Schema 列表 |

## 远程备份（SFTP）

当备份任务的存储配置为"远程"或"本地+远程"时，备份文件会通过 SFTP 上传到远程服务器。

**认证方式：**
1. **SSH 密钥**（推荐）：填写密钥文件的绝对路径，如 `/home/user/.ssh/id_rsa`
2. **密码认证**：填写 SFTP 用户的登录密码
3. **两者都填**：优先使用密钥，密码作为备用

**上传流程：**
1. 备份完成后生成 `.sql.gz` 压缩文件
2. 通过 SSH 连接到远程服务器
3. 自动创建远程目标目录（递归创建）
4. 流式上传备份文件
5. 保留策略同步清理远程旧文件

## 安全注意事项

1. **修改默认密码**：首次启动后请立即在 `config.yaml` 中修改 `auth.password` 并重启
2. **保护配置文件**：`config.yaml` 包含加密密钥，权限为 `0600`（仅所有者可读写）
3. **文件权限**：`data/` 目录下的 SQLite 数据库同样为 `0600` 权限
4. **密钥存储**：数据库密码和 SFTP 凭据使用 AES-256-GCM 加密存储，密钥保存在 `config.yaml` 中
5. **Session 安全**：登录 Session 使用 HttpOnly + SameSite Cookie，24 小时自动过期

## 项目结构特点

- **零外部依赖部署**：Web 资源通过 Go embed 内嵌，SQLite 为纯 Go 实现，编译后仅需一个可执行文件
- **版本化数据库迁移**：启动时自动检测并执行数据库迁移，支持无缝升级
- **优雅关闭**：捕获 SIGINT/SIGTERM 信号，安全停止调度器和关闭数据库连接
- **备份不阻塞**：备份任务在后台 Goroutine 中异步执行，Web 界面保持响应
- **引擎接口抽象**：`Engine` 接口统一 MySQL 和 PostgreSQL 的备份操作，便于扩展其他数据库

## License

MIT License
