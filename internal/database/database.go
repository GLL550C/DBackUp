package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite"
)

var db *sql.DB
var once sync.Once

// Init opens the SQLite database and runs migrations
func Init(dataDir string) error {
	var initErr error
	once.Do(func() {
		os.MkdirAll(dataDir, 0755)
		dbPath := filepath.Join(dataDir, "backup_tool.db")
		d, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_foreign_keys=on")
		if err != nil {
			initErr = fmt.Errorf("打开数据库失败: %w", err)
			return
		}
		d.SetMaxOpenConns(1)
		db = d
		initErr = runMigrations()
	})
	return initErr
}

func GetDB() *sql.DB  { return db }
func Close()          { if db != nil { db.Close() } }

// ==================== Versioned Migrations ====================

type migration struct {
	version int
	name    string
	sql     string
}

var migrations = []migration{
	{1, "schema_version", `
		CREATE TABLE IF NOT EXISTS schema_version (
			version INTEGER PRIMARY KEY,
			applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`},
	{2, "connections", `
		CREATE TABLE IF NOT EXISTS connections (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			db_type TEXT NOT NULL CHECK(db_type IN ('mysql','postgresql')),
			host TEXT NOT NULL,
			port INTEGER NOT NULL,
			username TEXT NOT NULL,
			password TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`},
	{3, "backup_tasks", `
		CREATE TABLE IF NOT EXISTS backup_tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			connection_id INTEGER NOT NULL,
			databases TEXT NOT NULL DEFAULT '["*"]',
			backup_params TEXT NOT NULL DEFAULT '{}',
			storage_type TEXT NOT NULL DEFAULT 'local',
			local_path TEXT NOT NULL DEFAULT '',
			remote_host TEXT NOT NULL DEFAULT '',
			remote_port INTEGER NOT NULL DEFAULT 22,
			remote_user TEXT NOT NULL DEFAULT '',
			remote_pass TEXT NOT NULL DEFAULT '',
			remote_key TEXT NOT NULL DEFAULT '',
			remote_path TEXT NOT NULL DEFAULT '',
			max_backups INTEGER NOT NULL DEFAULT 10,
			retention_days INTEGER NOT NULL DEFAULT 0,
			cron_expr TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE
		)
	`},
	{4, "backup_records", `
		CREATE TABLE IF NOT EXISTS backup_records (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id INTEGER NOT NULL,
			task_name TEXT NOT NULL DEFAULT '',
			db_type TEXT NOT NULL DEFAULT '',
			db_name TEXT NOT NULL DEFAULT '',
			file_name TEXT NOT NULL DEFAULT '',
			file_path TEXT NOT NULL DEFAULT '',
			file_size INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'running' CHECK(status IN ('running','success','failed')),
			message TEXT NOT NULL DEFAULT '',
			started_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			finished_at DATETIME,
			duration INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY (task_id) REFERENCES backup_tasks(id) ON DELETE CASCADE
		)
	`},
	{5, "app_settings", `
		CREATE TABLE IF NOT EXISTS app_settings (
			id INTEGER PRIMARY KEY CHECK(id = 1),
			server_port INTEGER NOT NULL DEFAULT 8080,
			default_backup_dir TEXT NOT NULL DEFAULT '',
			log_retention_days INTEGER NOT NULL DEFAULT 30
		);
		INSERT OR IGNORE INTO app_settings (id, server_port, default_backup_dir, log_retention_days)
		VALUES (1, 8080, '', 30)
	`},
}

func runMigrations() error {
	// Get current version
	var version int
	db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&version)

	for _, m := range migrations {
		if m.version <= version {
			continue
		}
		if _, err := db.Exec(m.sql); err != nil {
			return fmt.Errorf("迁移 %d (%s) 失败: %w", m.version, m.name, err)
		}
		if _, err := db.Exec("INSERT INTO schema_version (version) VALUES (?)", m.version); err != nil {
			return fmt.Errorf("记录迁移 %d 失败: %w", m.version, err)
		}
	}
	return nil
}

// ==================== Connection CRUD ====================

func CreateConnection(c ConnectionInput) (int64, error) {
	res, err := db.Exec(
		`INSERT INTO connections (name, db_type, host, port, username, password)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		c.Name, c.DBType, c.Host, c.Port, c.Username, c.Password,
	)
	if err != nil { return 0, err }
	return res.LastInsertId()
}

func UpdateConnection(id int64, c ConnectionInput) error {
	_, err := db.Exec(
		`UPDATE connections SET name=?, db_type=?, host=?, port=?, username=?, password=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		c.Name, c.DBType, c.Host, c.Port, c.Username, c.Password, id,
	)
	return err
}

func DeleteConnection(id int64) error {
	_, err := db.Exec(`DELETE FROM connections WHERE id=?`, id)
	return err
}

func GetConnection(id int64) (*ConnectionRow, error) {
	row := db.QueryRow(
		`SELECT id, name, db_type, host, port, username, password, created_at, updated_at FROM connections WHERE id=?`, id,
	)
	return scanConnection(row)
}

func ListConnections() ([]ConnectionRow, error) {
	rows, err := db.Query(
		`SELECT id, name, db_type, host, port, username, password, created_at, updated_at FROM connections ORDER BY created_at DESC`,
	)
	if err != nil { return nil, err }
	defer rows.Close()
	return scanConnections(rows)
}

// ==================== Backup Task CRUD ====================

func CreateTask(t TaskInput) (int64, error) {
	res, err := db.Exec(
		`INSERT INTO backup_tasks (name, connection_id, databases, backup_params,
		 storage_type, local_path, remote_host, remote_port, remote_user, remote_pass, remote_key, remote_path,
		 max_backups, retention_days, cron_expr, enabled)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.Name, t.ConnectionID, t.Databases, t.BackupParams,
		t.StorageType, t.LocalPath, t.RemoteHost, t.RemotePort, t.RemoteUser, t.RemotePass, t.RemoteKey, t.RemotePath,
		t.MaxBackups, t.RetentionDays, t.CronExpr, t.Enabled,
	)
	if err != nil { return 0, err }
	return res.LastInsertId()
}

func UpdateTask(id int64, t TaskInput) error {
	_, err := db.Exec(
		`UPDATE backup_tasks SET name=?, connection_id=?, databases=?, backup_params=?,
		 storage_type=?, local_path=?, remote_host=?, remote_port=?, remote_user=?, remote_pass=?, remote_key=?, remote_path=?,
		 max_backups=?, retention_days=?, cron_expr=?, enabled=?, updated_at=CURRENT_TIMESTAMP
		 WHERE id=?`,
		t.Name, t.ConnectionID, t.Databases, t.BackupParams,
		t.StorageType, t.LocalPath, t.RemoteHost, t.RemotePort, t.RemoteUser, t.RemotePass, t.RemoteKey, t.RemotePath,
		t.MaxBackups, t.RetentionDays, t.CronExpr, t.Enabled, id,
	)
	return err
}

func DeleteTask(id int64) error {
	_, err := db.Exec(`DELETE FROM backup_tasks WHERE id=?`, id)
	return err
}

func GetTask(id int64) (*TaskRow, error) {
	row := db.QueryRow(
		`SELECT id, name, connection_id, databases, backup_params,
		 storage_type, local_path, remote_host, remote_port, remote_user, remote_pass, remote_key, remote_path,
		 max_backups, retention_days, cron_expr, enabled, created_at, updated_at
		 FROM backup_tasks WHERE id=?`, id,
	)
	return scanTask(row)
}

func ListTasks() ([]TaskRow, error) {
	rows, err := db.Query(
		`SELECT id, name, connection_id, databases, backup_params,
		 storage_type, local_path, remote_host, remote_port, remote_user, remote_pass, remote_key, remote_path,
		 max_backups, retention_days, cron_expr, enabled, created_at, updated_at
		 FROM backup_tasks ORDER BY created_at DESC`,
	)
	if err != nil { return nil, err }
	defer rows.Close()
	return scanTasks(rows)
}

// ParseDatabases unmarshals the databases JSON field
func ParseDatabases(raw string) []string {
	if raw == "" { return []string{"*"} }
	var arr []string
	if err := json.Unmarshal([]byte(raw), &arr); err != nil || len(arr) == 0 {
		return []string{"*"}
	}
	return arr
}

// ParseBackupParams unmarshals the backup_params JSON field
func ParseBackupParams(raw string) map[string]interface{} {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return make(map[string]interface{})
	}
	return m
}

// ==================== Backup Record CRUD ====================

func CreateRecord(r RecordInput) (int64, error) {
	res, err := db.Exec(
		`INSERT INTO backup_records (task_id, task_name, db_type, db_name, file_name, file_path, file_size, status, message, started_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.TaskID, r.TaskName, r.DBType, r.DBName, r.FileName, r.FilePath, r.FileSize, r.Status, r.Message, r.StartedAt,
	)
	if err != nil { return 0, err }
	return res.LastInsertId()
}

func UpdateRecordStatus(id int64, status, message, finishedAt string, fileSize, duration int64) error {
	_, err := db.Exec(
		`UPDATE backup_records SET status=?, message=?, finished_at=?, file_size=?, duration=? WHERE id=?`,
		status, message, finishedAt, fileSize, duration, id,
	)
	return err
}

func GetRecord(id int64) (*RecordRow, error) {
	row := db.QueryRow(
		`SELECT id, task_id, task_name, db_type, db_name, file_name, file_path, file_size, status, message, started_at, finished_at, duration
		 FROM backup_records WHERE id=?`, id,
	)
	return scanRecord(row)
}

func ListRecords(taskID int64, limit, offset int) ([]RecordRow, error) {
	var rows *sql.Rows
	var err error
	if taskID > 0 {
		rows, err = db.Query(
			`SELECT id, task_id, task_name, db_type, db_name, file_name, file_path, file_size, status, message, started_at, finished_at, duration
			 FROM backup_records WHERE task_id=? ORDER BY started_at DESC LIMIT ? OFFSET ?`,
			taskID, limit, offset)
	} else {
		rows, err = db.Query(
			`SELECT id, task_id, task_name, db_type, db_name, file_name, file_path, file_size, status, message, started_at, finished_at, duration
			 FROM backup_records ORDER BY started_at DESC LIMIT ? OFFSET ?`,
			limit, offset)
	}
	if err != nil { return nil, err }
	defer rows.Close()
	return scanRecords(rows)
}

func CountRecords(taskID int64) (int64, error) {
	var count int64
	var err error
	if taskID > 0 {
		err = db.QueryRow(`SELECT COUNT(*) FROM backup_records WHERE task_id=?`, taskID).Scan(&count)
	} else {
		err = db.QueryRow(`SELECT COUNT(*) FROM backup_records`).Scan(&count)
	}
	return count, err
}

func DeleteRecord(id int64) error {
	_, err := db.Exec(`DELETE FROM backup_records WHERE id=?`, id)
	return err
}

func GetDashboardStats() (*DashboardStatsRow, error) {
	stats := &DashboardStatsRow{}
	db.QueryRow(`SELECT COUNT(*) FROM backup_tasks`).Scan(&stats.TotalTasks)
	db.QueryRow(`SELECT COUNT(*) FROM backup_tasks WHERE enabled=1`).Scan(&stats.EnabledTasks)
	db.QueryRow(`SELECT COUNT(*) FROM backup_records`).Scan(&stats.TotalRecords)
	db.QueryRow(`SELECT COUNT(*) FROM backup_records WHERE status='success'`).Scan(&stats.SuccessRecords)
	db.QueryRow(`SELECT COUNT(*) FROM backup_records WHERE status='failed'`).Scan(&stats.FailedRecords)
	db.QueryRow(`SELECT COALESCE(SUM(file_size), 0) FROM backup_records WHERE status='success'`).Scan(&stats.TotalSize)
	db.QueryRow(`SELECT COALESCE(started_at, '') FROM backup_records ORDER BY started_at DESC LIMIT 1`).Scan(&stats.LastBackupTime)
	return stats, nil
}

func GetRecentRecords(limit int) ([]RecordRow, error) {
	rows, err := db.Query(
		`SELECT id, task_id, task_name, db_type, db_name, file_name, file_path, file_size, status, message, started_at, finished_at, duration
		 FROM backup_records ORDER BY started_at DESC LIMIT ?`, limit)
	if err != nil { return nil, err }
	defer rows.Close()
	return scanRecords(rows)
}

func GetBackupFilesForTask(taskID int64) ([]FileRow, error) {
	rows, err := db.Query(
		`SELECT id, file_path FROM backup_records WHERE task_id=? AND status='success' ORDER BY started_at DESC`, taskID)
	if err != nil { return nil, err }
	defer rows.Close()
	var files []FileRow
	for rows.Next() {
		var f FileRow
		if err := rows.Scan(&f.ID, &f.FilePath); err != nil { return nil, err }
		files = append(files, f)
	}
	return files, rows.Err()
}

// ==================== Row Types ====================

type ConnectionInput struct {
	Name, DBType, Host, Username, Password string
	Port int
}

type TaskInput struct {
	Name          string
	ConnectionID  int64
	Databases     string
	BackupParams  string
	StorageType   string
	LocalPath     string
	RemoteHost    string
	RemotePort    int
	RemoteUser    string
	RemotePass    string
	RemoteKey     string
	RemotePath    string
	MaxBackups    int
	RetentionDays int
	CronExpr      string
	Enabled       bool
}

type RecordInput struct {
	TaskID, FileSize int64
	TaskName, DBType, DBName, FileName, FilePath, Status, Message, StartedAt string
}

type ConnectionRow struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	DBType    string `json:"db_type"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type TaskRow struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	ConnectionID  int64  `json:"connection_id"`
	Databases     string `json:"databases"`
	BackupParams  string `json:"backup_params"`
	StorageType   string `json:"storage_type"`
	LocalPath     string `json:"local_path"`
	RemoteHost    string `json:"remote_host"`
	RemotePort    int    `json:"remote_port"`
	RemoteUser    string `json:"remote_user"`
	RemotePass    string `json:"remote_pass"`
	RemoteKey     string `json:"remote_key"`
	RemotePath    string `json:"remote_path"`
	MaxBackups    int    `json:"max_backups"`
	RetentionDays int    `json:"retention_days"`
	CronExpr      string `json:"cron_expr"`
	Enabled       bool   `json:"enabled"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

type RecordRow struct {
	ID         int64  `json:"id"`
	TaskID     int64  `json:"task_id"`
	TaskName   string `json:"task_name"`
	DBType     string `json:"db_type"`
	DBName     string `json:"db_name"`
	FileName   string `json:"file_name"`
	FilePath   string `json:"file_path"`
	FileSize   int64  `json:"file_size"`
	Status     string `json:"status"`
	Message    string `json:"message"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at"`
	Duration   int64  `json:"duration"`
}

type DashboardStatsRow struct {
	TotalTasks, EnabledTasks, TotalRecords, SuccessRecords, FailedRecords, TotalSize int64
	LastBackupTime string
}

type FileRow struct {
	ID       int64
	FilePath string
}

// ==================== Scanners ====================

func scanConnection(row *sql.Row) (*ConnectionRow, error) {
	var c ConnectionRow
	err := row.Scan(&c.ID, &c.Name, &c.DBType, &c.Host, &c.Port, &c.Username, &c.Password, &c.CreatedAt, &c.UpdatedAt)
	if err != nil { return nil, err }
	return &c, nil
}

func scanConnections(rows *sql.Rows) ([]ConnectionRow, error) {
	var results []ConnectionRow
	for rows.Next() {
		var c ConnectionRow
		if err := rows.Scan(&c.ID, &c.Name, &c.DBType, &c.Host, &c.Port, &c.Username, &c.Password, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		results = append(results, c)
	}
	return results, rows.Err()
}

func scanTask(row *sql.Row) (*TaskRow, error) {
	var t TaskRow
	err := row.Scan(&t.ID, &t.Name, &t.ConnectionID, &t.Databases, &t.BackupParams,
		&t.StorageType, &t.LocalPath, &t.RemoteHost, &t.RemotePort, &t.RemoteUser, &t.RemotePass, &t.RemoteKey, &t.RemotePath,
		&t.MaxBackups, &t.RetentionDays, &t.CronExpr, &t.Enabled, &t.CreatedAt, &t.UpdatedAt)
	if err != nil { return nil, err }
	return &t, nil
}

func scanTasks(rows *sql.Rows) ([]TaskRow, error) {
	var results []TaskRow
	for rows.Next() {
		var t TaskRow
		if err := rows.Scan(&t.ID, &t.Name, &t.ConnectionID, &t.Databases, &t.BackupParams,
			&t.StorageType, &t.LocalPath, &t.RemoteHost, &t.RemotePort, &t.RemoteUser, &t.RemotePass, &t.RemoteKey, &t.RemotePath,
			&t.MaxBackups, &t.RetentionDays, &t.CronExpr, &t.Enabled, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		results = append(results, t)
	}
	return results, rows.Err()
}

func scanRecord(row *sql.Row) (*RecordRow, error) {
	var r RecordRow
	var finishedAt sql.NullString
	err := row.Scan(&r.ID, &r.TaskID, &r.TaskName, &r.DBType, &r.DBName, &r.FileName, &r.FilePath, &r.FileSize,
		&r.Status, &r.Message, &r.StartedAt, &finishedAt, &r.Duration)
	if err != nil { return nil, err }
	r.FinishedAt = finishedAt.String
	return &r, nil
}

func scanRecords(rows *sql.Rows) ([]RecordRow, error) {
	var results []RecordRow
	for rows.Next() {
		var r RecordRow
		var finishedAt sql.NullString
		if err := rows.Scan(&r.ID, &r.TaskID, &r.TaskName, &r.DBType, &r.DBName, &r.FileName, &r.FilePath, &r.FileSize,
			&r.Status, &r.Message, &r.StartedAt, &finishedAt, &r.Duration); err != nil {
			return nil, err
		}
		r.FinishedAt = finishedAt.String
		results = append(results, r)
	}
	return results, rows.Err()
}
