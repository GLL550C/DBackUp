-- ============================================================
-- DB Backup Tool - 数据库初始化脚本（唯一 SQL 维护文件）
--
-- 用法:   sqlite3 data/main.db < db/init.sql
--         ./scripts/init_db.sh
--
-- 表关系: connections 1 ─< N backup_tasks 1 ─< N backup_records
--         删除连接 → 级联删除关联的备份任务
--         删除任务 → 级联删除关联的备份记录
-- ============================================================

PRAGMA journal_mode=WAL;
PRAGMA foreign_keys=ON;

-- -----------------------------------------------------------
-- 1. schema_version — 迁移版本记录
--    程序启动时根据此表判断是否需要执行 Go 内置迁移
--    init.sql 预设版本 1~5，避免程序重复执行迁移
-- -----------------------------------------------------------
CREATE TABLE IF NOT EXISTS schema_version (
    version    INTEGER PRIMARY KEY,
    applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- -----------------------------------------------------------
-- 2. connections — 数据库连接配置
--    存储 MySQL / PostgreSQL 服务器的连接信息
--    密码字段使用 AES-256-GCM 加密存储
-- -----------------------------------------------------------
CREATE TABLE IF NOT EXISTS connections (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL,                             -- 连接名称（如：生产环境MySQL）
    db_type    TEXT    NOT NULL CHECK(db_type IN ('mysql','postgresql')),
    host       TEXT    NOT NULL,                             -- 主机地址
    port       INTEGER NOT NULL,                             -- 端口 (MySQL:3306, PG:5432)
    username   TEXT    NOT NULL,                             -- 用户名
    password   TEXT    NOT NULL DEFAULT '',                  -- 密码（AES-256-GCM 加密）
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- -----------------------------------------------------------
-- 3. backup_tasks — 备份任务配置
--    每个任务定义了一个完整的备份作业参数
-- -----------------------------------------------------------
CREATE TABLE IF NOT EXISTS backup_tasks (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    name            TEXT    NOT NULL,                        -- 任务名称
    connection_id   INTEGER NOT NULL,                        -- 关联的数据库连接
    -- 备份范围
    databases       TEXT    NOT NULL DEFAULT '["*"]',        -- JSON数组: ["*"]全部 / ["db1","db2"]指定库
    backup_params   TEXT    NOT NULL DEFAULT '{}',           -- JSON对象: 备份参数 {key:value}
    -- 存储配置
    storage_type    TEXT    NOT NULL DEFAULT 'local',        -- 存储模式: local / remote / both
    local_path      TEXT    NOT NULL DEFAULT '',             -- 本地备份目录（留空使用默认）
    remote_host     TEXT    NOT NULL DEFAULT '',             -- SFTP 远程主机
    remote_port     INTEGER NOT NULL DEFAULT 22,             -- SFTP 端口
    remote_user     TEXT    NOT NULL DEFAULT '',             -- SFTP 用户名
    remote_pass     TEXT    NOT NULL DEFAULT '',             -- SFTP 密码（AES-256-GCM 加密）
    remote_key      TEXT    NOT NULL DEFAULT '',             -- SSH 私钥路径（免密登录优先）
    remote_path     TEXT    NOT NULL DEFAULT '',             -- 远程备份目录
    -- 保留策略
    max_backups     INTEGER NOT NULL DEFAULT 10,             -- 最多保留 N 份（0=不限制）
    retention_days  INTEGER NOT NULL DEFAULT 0,              -- 保留 N 天（0=不限制）
    -- 定时
    cron_expr       TEXT    NOT NULL DEFAULT '',             -- Cron 表达式（空=手动触发）
    enabled         INTEGER NOT NULL DEFAULT 0,              -- 是否启用定时 (0/1)
    -- 时间戳
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP,

    FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE
);

-- -----------------------------------------------------------
-- 4. backup_records — 备份执行记录
--    每次备份执行都会在此表创建一条记录
-- -----------------------------------------------------------
CREATE TABLE IF NOT EXISTS backup_records (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id     INTEGER NOT NULL,                            -- 关联的备份任务
    task_name   TEXT    NOT NULL DEFAULT '',                 -- 任务名称（冗余，方便查询）
    db_type     TEXT    NOT NULL DEFAULT '',                 -- 数据库类型 (mysql/postgresql)
    db_name     TEXT    NOT NULL DEFAULT '',                 -- 备份的数据库名/标签
    file_name   TEXT    NOT NULL DEFAULT '',                 -- 备份文件名
    file_path   TEXT    NOT NULL DEFAULT '',                 -- 文件完整路径
    file_size   INTEGER NOT NULL DEFAULT 0,                  -- 文件大小（字节）
    status      TEXT    NOT NULL DEFAULT 'running'           -- 状态
                    CHECK(status IN ('running','success','failed')),
    message     TEXT    NOT NULL DEFAULT '',                 -- 成功/错误消息
    started_at  DATETIME DEFAULT CURRENT_TIMESTAMP,          -- 开始时间
    finished_at DATETIME,                                    -- 完成时间
    duration    INTEGER NOT NULL DEFAULT 0,                  -- 耗时（秒）

    FOREIGN KEY (task_id) REFERENCES backup_tasks(id) ON DELETE CASCADE
);

-- -----------------------------------------------------------
-- 5. app_settings — 应用设置（单行表，仅 id=1 一条记录）
-- -----------------------------------------------------------
CREATE TABLE IF NOT EXISTS app_settings (
    id                 INTEGER PRIMARY KEY CHECK(id = 1),
    server_port        INTEGER NOT NULL DEFAULT 8080,
    default_backup_dir TEXT    NOT NULL DEFAULT '',
    log_retention_days INTEGER NOT NULL DEFAULT 30
);

-- ============================================================
-- 初始数据
-- ============================================================
INSERT OR IGNORE INTO app_settings (id, server_port, default_backup_dir, log_retention_days)
VALUES (1, 8080, '', 30);

INSERT OR IGNORE INTO schema_version (version) VALUES (1);
INSERT OR IGNORE INTO schema_version (version) VALUES (2);
INSERT OR IGNORE INTO schema_version (version) VALUES (3);
INSERT OR IGNORE INTO schema_version (version) VALUES (4);
INSERT OR IGNORE INTO schema_version (version) VALUES (5);

-- ============================================================
-- 常用查询
-- ============================================================
-- 查看所有连接:       SELECT * FROM connections ORDER BY created_at DESC;
-- 查看启用的定时任务:   SELECT * FROM backup_tasks WHERE enabled = 1;
-- 最近10条备份记录:    SELECT * FROM backup_records ORDER BY started_at DESC LIMIT 10;
-- 统计成功率:
--   SELECT COUNT(*) AS total,
--     SUM(CASE WHEN status='success' THEN 1 ELSE 0 END) AS success,
--     ROUND(SUM(CASE WHEN status='success' THEN 1 ELSE 0 END)*100.0/COUNT(*),1) AS rate
--   FROM backup_records;
