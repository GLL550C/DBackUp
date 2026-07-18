package models

// DBConnection represents a source database to back up
type DBConnection struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	DBType    string `json:"db_type"` // "mysql" | "postgresql"
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Username  string `json:"username"`
	Password  string `json:"password"` // stored as base64 (simple obfuscation)
	Database  string `json:"database"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// BackupTask defines a backup job configuration
type BackupTask struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	ConnectionID  int64  `json:"connection_id"`
	StorageType   string `json:"storage_type"` // "local" | "remote"
	LocalPath     string `json:"local_path"`
	RemoteHost    string `json:"remote_host"`
	RemotePort    int    `json:"remote_port"` // default 22
	RemoteUser    string `json:"remote_user"`
	RemotePass    string `json:"remote_pass"`
	RemotePath    string `json:"remote_path"`
	MaxBackups    int    `json:"max_backups"`    // keep N latest files (0 = unlimited)
	RetentionDays int    `json:"retention_days"` // keep files for N days (0 = unlimited)
	CronExpr      string `json:"cron_expr"`      // e.g. "0 2 * * *" for daily at 2am
	Enabled       bool   `json:"enabled"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

// BackupRecord tracks each backup execution
type BackupRecord struct {
	ID         int64  `json:"id"`
	TaskID     int64  `json:"task_id"`
	TaskName   string `json:"task_name"`
	DBType     string `json:"db_type"`
	DBName     string `json:"db_name"`
	FileName   string `json:"file_name"`
	FilePath   string `json:"file_path"`
	FileSize   int64  `json:"file_size"` // bytes
	Status     string `json:"status"`    // "running" | "success" | "failed"
	Message    string `json:"message"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at"`
	Duration   int64  `json:"duration"` // seconds
}

// AppSettings holds global application configuration
type AppSettings struct {
	ID              int64  `json:"id"`
	ServerPort      int    `json:"server_port"`
	DefaultBackupDir string `json:"default_backup_dir"`
	LogRetentionDays int   `json:"log_retention_days"`
}

// DashboardStats holds summary data for the dashboard
type DashboardStats struct {
	TotalTasks      int64   `json:"total_tasks"`
	EnabledTasks    int64   `json:"enabled_tasks"`
	TotalBackups    int64   `json:"total_backups"`
	SuccessBackups  int64   `json:"success_backups"`
	FailedBackups   int64   `json:"failed_backups"`
	SuccessRate     float64 `json:"success_rate"`
	TotalSize       int64   `json:"total_size"`
	LastBackupTime  string  `json:"last_backup_time"`
}

// RecentRecord is a simplified backup record for dashboard display
type RecentRecord struct {
	ID         int64  `json:"id"`
	TaskName   string `json:"task_name"`
	DBType     string `json:"db_type"`
	FileName   string `json:"file_name"`
	FileSize   int64  `json:"file_size"`
	Status     string `json:"status"`
	StartedAt  string `json:"started_at"`
	Duration   int64  `json:"duration"`
}
