package backup

import (
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"db-backup-tool/internal/crypto"
	"db-backup-tool/internal/database"
)

// Manager orchestrates backup operations
type Manager struct {
	dataDir string
}

// NewManager creates a new backup manager
func NewManager(dataDir string) *Manager {
	return &Manager{dataDir: dataDir}
}

// ExecuteBackup runs a backup for the given task and returns the record ID
func (m *Manager) ExecuteBackup(taskID int64) (int64, error) {
	// Load task
	task, err := database.GetTask(taskID)
	if err != nil {
		return 0, fmt.Errorf("failed to load task: %w", err)
	}

	// Load connection
	conn, err := database.GetConnection(task.ConnectionID)
	if err != nil {
		return 0, fmt.Errorf("failed to load connection: %w", err)
	}

	// Parse databases and params
	databases := database.ParseDatabases(task.Databases)
	backupParams := database.ParseBackupParams(task.BackupParams)

	// Decrypt password
	plainPass, _ := crypto.Decrypt(conn.Password)

	// Build engine config
	engineCfg := Config{
		Type:      conn.DBType,
		Host:      conn.Host,
		Port:      conn.Port,
		Username:  conn.Username,
		Password:  plainPass,
		Databases: databases,
		Params:    backupParams,
	}

	// Validate params
	if err := ValidateParams(conn.DBType, backupParams); err != nil {
		return 0, fmt.Errorf("备份参数校验失败: %w", err)
	}

	// Generate backup filename
	timestamp := time.Now().Format("20060102_150405")
	var dbLabel string
	if len(databases) == 0 || (len(databases) == 1 && databases[0] == "*") {
		dbLabel = "all"
	} else if len(databases) == 1 {
		dbLabel = databases[0]
	} else if len(databases) <= 3 {
		dbLabel = strings.Join(databases, "_")
	} else {
		dbLabel = databases[0] + "_等" + fmt.Sprintf("%d", len(databases)) + "库"
	}
	fileName := fmt.Sprintf("%s_%s_%s.sql.gz", conn.DBType, dbLabel, timestamp)

	// Determine local path
	localDir := task.LocalPath
	if localDir == "" {
		localDir = filepath.Join(m.dataDir, "backups")
	}
	os.MkdirAll(localDir, 0755)
	localFilePath := filepath.Join(localDir, fileName)

	// Create backup record
	startedAt := time.Now().Format("2006-01-02 15:04:05")
	recordID, err := database.CreateRecord(database.RecordInput{
		TaskID:    taskID,
		TaskName:  task.Name,
		DBType:    conn.DBType,
		DBName:    dbLabel,
		FileName:  fileName,
		FilePath:  localFilePath,
		FileSize:  0,
		Status:    "running",
		Message:   "Backup started",
		StartedAt: startedAt,
	})
	if err != nil {
		return 0, fmt.Errorf("failed to create backup record: %w", err)
	}

	// Run backup in background
	// Decrypt remote credentials
	remotePass, _ := crypto.Decrypt(task.RemotePass)
	remoteKey, _ := crypto.Decrypt(task.RemoteKey)

	go m.doBackup(recordID, taskID, engineCfg, localFilePath, fileName, task.StorageType,
		task.RemoteHost, task.RemotePort, task.RemoteUser, remotePass, remoteKey, task.RemotePath,
		task.MaxBackups, task.RetentionDays)

	return recordID, nil
}

func (m *Manager) doBackup(recordID, taskID int64, engineCfg Config, localFilePath, fileName,
	storageType, remoteHost string, remotePort int, remoteUser, remotePass, remoteKey, remotePath string,
	maxBackups, retentionDays int) {

	startTime := time.Now()

	// Create the engine
	var engine Engine
	var err error

	switch engineCfg.Type {
	case "mysql":
		engine, err = NewMySQLEngine(engineCfg)
	case "postgresql":
		engine, err = NewPostgreSQLEngine(engineCfg)
	default:
		err = fmt.Errorf("unsupported database type: %s", engineCfg.Type)
	}

	if err != nil {
		m.failRecord(recordID, fmt.Sprintf("Failed to create engine: %v", err), startTime)
		return
	}
	defer engine.Close()

	// Create temp file for gzip compression
	tmpFile, err := os.CreateTemp("", "db-backup-*.sql.gz")
	if err != nil {
		m.failRecord(recordID, fmt.Sprintf("Failed to create temp file: %v", err), startTime)
		return
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	// Create gzip writer
	gzipWriter := gzip.NewWriter(tmpFile)

	// Dump database through gzip
	log.Printf("Starting backup: %v (%s)", engineCfg.Databases, engineCfg.Type)
	if err := engine.Dump(gzipWriter); err != nil {
		gzipWriter.Close()
		tmpFile.Close()
		m.failRecord(recordID, fmt.Sprintf("Backup failed: %v", err), startTime)
		return
	}

	// Close gzip and get file size
	if err := gzipWriter.Close(); err != nil {
		tmpFile.Close()
		m.failRecord(recordID, fmt.Sprintf("Failed to finalize gzip: %v", err), startTime)
		return
	}
	tmpFile.Close()

	// Get file size
	fileInfo, err := os.Stat(tmpPath)
	if err != nil {
		m.failRecord(recordID, fmt.Sprintf("Failed to stat temp file: %v", err), startTime)
		return
	}
	fileSize := fileInfo.Size()

	// Handle storage based on mode: local / remote / both
	keepLocal := storageType == "local" || storageType == "both"
	doRemote := (storageType == "remote" || storageType == "both") && remoteHost != ""

	// Save local copy if needed
	if keepLocal {
		if err := copyFile(tmpPath, localFilePath); err != nil {
			m.failRecord(recordID, fmt.Sprintf("本地保存失败: %v", err), startTime)
			return
		}
	}

	// Upload to remote if configured
	if doRemote {
		remoteStorage, err := NewSFTPStorage(remoteHost, remotePort, remoteUser, remotePass, remoteKey)
		if err != nil {
			m.failRecord(recordID, fmt.Sprintf("远程连接失败: %v", err), startTime)
			return
		}
		// Build remote path using Unix-style separators
		remoteFullPath := remotePath
		if !strings.HasSuffix(remotePath, "/") {
			remoteFullPath += "/"
		}
		remoteFullPath += fileName
		uploadedSize, err := remoteStorage.Upload(tmpPath, remoteFullPath)
		remoteStorage.Close()
		if err != nil {
			m.failRecord(recordID, fmt.Sprintf("远程上传失败: %v", err), startTime)
			return
		}
		log.Printf("已上传 %d 字节到 %s:%s", uploadedSize, remoteHost, remoteFullPath)
		// Update file path to remote path
		if storageType == "remote" {
				localFilePath = remoteFullPath
			}
	}

	// Cleanup old backups
	if maxBackups > 0 {
		m.enforceRetention(taskID, localFilePath, maxBackups, storageType, remoteHost, remotePort, remoteUser, remotePass, remoteKey, remotePath)
	}

	duration := int64(time.Since(startTime).Seconds())
	database.GetDB().Exec(`UPDATE backup_records SET file_path=? WHERE id=?`, localFilePath, recordID)
	database.UpdateRecordStatus(recordID, "success",
		fmt.Sprintf("备份完成（%d 字节）", fileSize),
		time.Now().Format("2006-01-02 15:04:05"), fileSize, duration)

	log.Printf("Backup completed: %s (%d bytes, %ds)", fileName, fileSize, duration)
}

func (m *Manager) failRecord(recordID int64, message string, startTime time.Time) {
	duration := int64(time.Since(startTime).Seconds())
	database.UpdateRecordStatus(recordID, "failed", message,
		time.Now().Format("2006-01-02 15:04:05"), 0, duration)
	log.Printf("Backup failed: %s", message)
}

// enforceRetention removes old backup files keeping only maxBackups latest
func (m *Manager) enforceRetention(taskID int64, currentFile string, maxBackups int,
	storageType, remoteHost string, remotePort int, remoteUser, remotePass, remoteKey, remotePath string) {

	files, err := database.GetBackupFilesForTask(taskID)
	if err != nil {
		log.Printf("Failed to list backup files for retention: %v", err)
		return
	}

	if len(files) <= maxBackups {
		return
	}

	// Files are ordered by started_at DESC, so files after index maxBackups-1 are older
	toDelete := files[maxBackups:]

	// Create remote storage if needed (for both "remote" and "both" modes)
	doRemoteCleanup := (storageType == "remote" || storageType == "both") && remoteHost != ""
	var remote *SFTPStorage
	if doRemoteCleanup {
		remote, err = NewSFTPStorage(remoteHost, remotePort, remoteUser, remotePass, remoteKey)
		if err != nil {
			log.Printf("Failed to connect to remote for cleanup: %v", err)
			return
		}
		defer remote.Close()
	}

	for _, f := range toDelete {
		// Skip current file
		if f.FilePath == currentFile {
			continue
		}

		// Delete local file (silently skip if not found - may be remote-only)
		if err := os.Remove(f.FilePath); err != nil && !os.IsNotExist(err) {
			log.Printf("Failed to delete old local file %s: %v", f.FilePath, err)
		}

		// Delete remote file if applicable - use path.Base for Unix paths
		if remote != nil {
			remoteFileName := path.Base(f.FilePath)
			remoteFilePath := remotePath
			if !strings.HasSuffix(remotePath, "/") {
				remoteFilePath += "/"
			}
			remoteFilePath += remoteFileName
			if err := remote.Delete(remoteFilePath); err != nil {
				log.Printf("Failed to delete old remote file %s: %v", remoteFilePath, err)
			}
		}

		// Delete record
		database.DeleteRecord(f.ID)
		log.Printf("Cleaned up old backup: %s", f.FilePath)
	}
}

func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	// Ensure parent directory exists
	os.MkdirAll(filepath.Dir(dst), 0755)

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

// SFTPStorage is a simple SFTP client for remote file operations
type SFTPStorage struct {
	host     string
	port     int
	user     string
	password string
	keyPath  string
}

func NewSFTPStorage(host string, port int, user, password, keyPath string) (*SFTPStorage, error) {
	return &SFTPStorage{
		host:     host,
		port:     port,
		user:     user,
		password: password,
		keyPath:  keyPath,
	}, nil
}

func (s *SFTPStorage) Upload(localPath, remotePath string) (int64, error) {
	// Open local file for streaming
	localFile, err := os.Open(localPath)
	if err != nil {
		return 0, fmt.Errorf("打开本地文件失败: %w", err)
	}
	defer localFile.Close()

	// Connect via SSH and upload using SFTP
	client, err := connectSSH(s.host, s.port, s.user, s.password, s.keyPath)
	if err != nil {
		return 0, fmt.Errorf("SSH 连接失败: %w", err)
	}
	defer client.Close()

	sftpClient, err := createSFTPClient(client)
	if err != nil {
		return 0, fmt.Errorf("SFTP 会话失败: %w", err)
	}
	defer sftpClient.Close()

	// Ensure remote directory exists (use path.Dir for Unix paths)
	remoteDir := path.Dir(remotePath)
	if remoteDir != "." && remoteDir != "/" {
		sftpClient.MkdirAll(remoteDir)
	}

	// Upload file via streaming
	remoteFile, err := sftpClient.Create(remotePath)
	if err != nil {
		return 0, fmt.Errorf("创建远程文件失败: %w", err)
	}
	defer remoteFile.Close()

	n, err := io.Copy(remoteFile, localFile)
	if err != nil {
		return 0, fmt.Errorf("上传文件失败: %w", err)
	}

	return n, nil
}

func (s *SFTPStorage) Delete(remotePath string) error {
	client, err := connectSSH(s.host, s.port, s.user, s.password, s.keyPath)
	if err != nil {
		return err
	}
	defer client.Close()

	sftpClient, err := createSFTPClient(client)
	if err != nil {
		return err
	}
	defer sftpClient.Close()

	return sftpClient.Remove(remotePath)
}

func (s *SFTPStorage) Close() error {
	return nil
}
