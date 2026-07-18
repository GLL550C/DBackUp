-- ============================================================
-- 数据库备份工具 - 迁移历史
-- 按版本号顺序记录所有数据库变更
-- ============================================================

-- 版本 1: 迁移版本管理表
-- 记录已执行的迁移版本号
CREATE TABLE IF NOT EXISTS schema_version (
    version    INTEGER PRIMARY KEY,
    applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 版本 2: 数据库连接表
-- 存储数据库服务器连接信息
CREATE TABLE IF NOT EXISTS connections (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL,
    db_type    TEXT    NOT NULL CHECK(db_type IN ('mysql','postgresql')),
    host       TEXT    NOT NULL,
    port       INTEGER NOT NULL,
    username   TEXT    NOT NULL,
    password   TEXT    NOT NULL DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 版本 3: 备份任务表
-- 存储备份任务的完整配置
-- 注意: storage_type 支持 'local', 'remote', 'both' 三种模式
CREATE TABLE IF NOT EXISTS backup_tasks (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    name            TEXT    NOT NULL,
    connection_id   INTEGER NOT NULL,
    databases       TEXT    NOT NULL DEFAULT '["*"]',
    backup_params   TEXT    NOT NULL DEFAULT '{}',
    storage_type    TEXT    NOT NULL DEFAULT 'local',
    local_path      TEXT    NOT NULL DEFAULT '',
    remote_host     TEXT    NOT NULL DEFAULT '',
    remote_port     INTEGER NOT NULL DEFAULT 22,
    remote_user     TEXT    NOT NULL DEFAULT '',
    remote_pass     TEXT    NOT NULL DEFAULT '',
    remote_key      TEXT    NOT NULL DEFAULT '',
    remote_path     TEXT    NOT NULL DEFAULT '',
    max_backups     INTEGER NOT NULL DEFAULT 10,
    retention_days  INTEGER NOT NULL DEFAULT 0,
    cron_expr       TEXT    NOT NULL DEFAULT '',
    enabled         INTEGER NOT NULL DEFAULT 0,
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE
);

-- 版本 4: 备份记录表
-- 记录每次备份执行的结果
CREATE TABLE IF NOT EXISTS backup_records (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id     INTEGER NOT NULL,
    task_name   TEXT    NOT NULL DEFAULT '',
    db_type     TEXT    NOT NULL DEFAULT '',
    db_name     TEXT    NOT NULL DEFAULT '',
    file_name   TEXT    NOT NULL DEFAULT '',
    file_path   TEXT    NOT NULL DEFAULT '',
    file_size   INTEGER NOT NULL DEFAULT 0,
    status      TEXT    NOT NULL DEFAULT 'running' CHECK(status IN ('running','success','failed')),
    message     TEXT    NOT NULL DEFAULT '',
    started_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    finished_at DATETIME,
    duration    INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (task_id) REFERENCES backup_tasks(id) ON DELETE CASCADE
);

-- 版本 5: 应用设置表（当前最新版本）
CREATE TABLE IF NOT EXISTS app_settings (
    id                 INTEGER PRIMARY KEY CHECK(id = 1),
    server_port        INTEGER NOT NULL DEFAULT 8080,
    default_backup_dir TEXT    NOT NULL DEFAULT '',
    log_retention_days INTEGER NOT NULL DEFAULT 30
);

INSERT OR IGNORE INTO app_settings (id, server_port, default_backup_dir, log_retention_days)
VALUES (1, 8080, '', 30);
