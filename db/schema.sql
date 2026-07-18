-- ============================================================
-- 数据库备份工具 - 数据库表结构
-- 适用于: SQLite 3
-- 最后更新: 2026-07-17
-- ============================================================

-- -----------------------------------------------------------
-- 迁移版本记录表
-- 用于跟踪已执行的数据库迁移版本
-- -----------------------------------------------------------
CREATE TABLE IF NOT EXISTS schema_version (
    version    INTEGER PRIMARY KEY,              -- 迁移版本号 (1,2,3...)
    applied_at DATETIME DEFAULT CURRENT_TIMESTAMP -- 执行时间
);

-- -----------------------------------------------------------
-- 数据库连接配置
-- 存储要备份的数据库服务器连接信息
-- 注意：此表不包含具体数据库名，库名在备份任务中配置
-- -----------------------------------------------------------
CREATE TABLE IF NOT EXISTS connections (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL,                   -- 连接名称（如：生产环境MySQL）
    db_type    TEXT    NOT NULL CHECK(db_type IN ('mysql', 'postgresql')),
    host       TEXT    NOT NULL,                   -- 主机地址
    port       INTEGER NOT NULL,                   -- 端口 (MySQL:3306, PG:5432)
    username   TEXT    NOT NULL,                   -- 用户名
    password   TEXT    NOT NULL DEFAULT '',        -- 密码（base64编码）
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- -----------------------------------------------------------
-- 备份任务配置
-- 每个任务定义了一个备份作业的完整参数
-- -----------------------------------------------------------
CREATE TABLE IF NOT EXISTS backup_tasks (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    name            TEXT    NOT NULL,              -- 任务名称
    connection_id   INTEGER NOT NULL,              -- 关联的数据库连接ID
    -- 备份范围
    databases       TEXT    NOT NULL DEFAULT '["*"]',  -- JSON数组: ["*"]全部 / ["db1","db2"]指定库
    backup_params   TEXT    NOT NULL DEFAULT '{}',     -- JSON对象: 备份参数 {key:value}
    -- 存储配置
    storage_type    TEXT    NOT NULL DEFAULT 'local',  -- 存储模式: local / remote / both
    local_path      TEXT    NOT NULL DEFAULT '',       -- 本地备份目录（留空使用默认）
    remote_host     TEXT    NOT NULL DEFAULT '',       -- SFTP远程主机
    remote_port     INTEGER NOT NULL DEFAULT 22,       -- SFTP端口
    remote_user     TEXT    NOT NULL DEFAULT '',       -- SFTP用户名
    remote_pass     TEXT    NOT NULL DEFAULT '',       -- SFTP密码（base64编码）
    remote_key      TEXT    NOT NULL DEFAULT '',       -- SSH私钥路径（免密登录）
    remote_path     TEXT    NOT NULL DEFAULT '',       -- 远程备份目录
    -- 保留策略
    max_backups     INTEGER NOT NULL DEFAULT 10,       -- 最多保留N份（0=不限制）
    retention_days  INTEGER NOT NULL DEFAULT 0,        -- 保留N天（0=不限制）
    -- 定时
    cron_expr       TEXT    NOT NULL DEFAULT '',       -- Cron表达式（空=手动触发）
    enabled         INTEGER NOT NULL DEFAULT 0,        -- 是否启用定时 (0/1)
    -- 时间戳
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP,

    FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE
);

-- -----------------------------------------------------------
-- 备份执行记录
-- 每次备份执行都会在此表创建一条记录
-- -----------------------------------------------------------
CREATE TABLE IF NOT EXISTS backup_records (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id     INTEGER NOT NULL,                  -- 关联的备份任务ID
    task_name   TEXT    NOT NULL DEFAULT '',       -- 任务名称（冗余，方便查询）
    db_type     TEXT    NOT NULL DEFAULT '',       -- 数据库类型 (mysql/postgresql)
    db_name     TEXT    NOT NULL DEFAULT '',       -- 备份的数据库名
    file_name   TEXT    NOT NULL DEFAULT '',       -- 备份文件名
    file_path   TEXT    NOT NULL DEFAULT '',       -- 文件完整路径
    file_size   INTEGER NOT NULL DEFAULT 0,        -- 文件大小（字节）
    status      TEXT    NOT NULL DEFAULT 'running' -- 状态: running / success / failed
                    CHECK(status IN ('running', 'success', 'failed')),
    message     TEXT    NOT NULL DEFAULT '',       -- 状态消息（成功或错误信息）
    started_at  DATETIME DEFAULT CURRENT_TIMESTAMP, -- 开始时间
    finished_at DATETIME,                          -- 完成时间
    duration    INTEGER NOT NULL DEFAULT 0,        -- 耗时（秒）

    FOREIGN KEY (task_id) REFERENCES backup_tasks(id) ON DELETE CASCADE
);

-- -----------------------------------------------------------
-- 应用设置（单行表，仅1条记录）
-- -----------------------------------------------------------
CREATE TABLE IF NOT EXISTS app_settings (
    id                 INTEGER PRIMARY KEY CHECK(id = 1),
    server_port        INTEGER NOT NULL DEFAULT 8080,
    default_backup_dir TEXT    NOT NULL DEFAULT '',
    log_retention_days INTEGER NOT NULL DEFAULT 30
);

INSERT OR IGNORE INTO app_settings (id, server_port, default_backup_dir, log_retention_days)
VALUES (1, 8080, '', 30);

-- ============================================================
-- 表关系说明
-- ============================================================
-- connections  1 ──< N  backup_tasks  1 ──< N  backup_records
--
-- 删除连接时级联删除关联的备份任务
-- 删除任务时级联删除关联的备份记录

-- ============================================================
-- 常用查询示例
-- ============================================================

-- 查看所有连接
-- SELECT * FROM connections ORDER BY created_at DESC;

-- 查看所有启用的定时任务
-- SELECT * FROM backup_tasks WHERE enabled = 1;

-- 查看最近10条备份记录
-- SELECT * FROM backup_records ORDER BY started_at DESC LIMIT 10;

-- 统计成功率
-- SELECT
--   COUNT(*) AS total,
--   SUM(CASE WHEN status='success' THEN 1 ELSE 0 END) AS success,
--   ROUND(SUM(CASE WHEN status='success' THEN 1 ELSE 0 END)*100.0/COUNT(*), 1) AS rate
-- FROM backup_records;

-- 清理30天前的记录
-- DELETE FROM backup_records WHERE started_at < datetime('now', '-30 days');
