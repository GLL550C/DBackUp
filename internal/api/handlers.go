package api

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"os"
	"strconv"
	"time"

	"db-backup-tool/internal/backup"
	"db-backup-tool/internal/config"
	"db-backup-tool/internal/crypto"
	"db-backup-tool/internal/database"
	"db-backup-tool/internal/scheduler"

	"db-backup-tool/internal/auth"

	"github.com/gin-gonic/gin"
)

// Handler holds all dependencies for API handlers
type Handler struct {
	manager     *backup.Manager
	scheduler   *scheduler.Scheduler
	cfg         *config.Config
	templatesFS embed.FS
	staticFS    embed.FS
	layoutTmpl  *template.Template
	contentTmpl *template.Template
}

// NewHandler creates a new Handler with dependencies
func NewHandler(manager *backup.Manager, sched *scheduler.Scheduler, cfg *config.Config, tmplFS, staticFS embed.FS) *Handler {
	// Parse layout template (layout.html only, for Gin's SetHTMLTemplate)
	layoutTmpl := template.Must(template.New("").ParseFS(tmplFS, "web/templates/layout.html", "web/templates/login.html"))

	// Parse content templates (all files except layout.html and login.html)
	files, _ := fs.Glob(tmplFS, "web/templates/*.html")
	var contentFiles []string
	for _, f := range files {
		if f != "web/templates/layout.html" && f != "web/templates/login.html" {
			contentFiles = append(contentFiles, f)
		}
	}
	contentTmpl := template.Must(template.New("").ParseFS(tmplFS, contentFiles...))

	return &Handler{
		manager:     manager,
		scheduler:   sched,
		cfg:         cfg,
		templatesFS: tmplFS,
		staticFS:    staticFS,
		layoutTmpl:  layoutTmpl,
		contentTmpl: contentTmpl,
	}
}

// renderPage renders a page by executing the content template first, then the layout
func (h *Handler) renderPage(c *gin.Context, contentName string, data gin.H) {
	// Render content to string
	var buf bytes.Buffer
	if err := h.contentTmpl.ExecuteTemplate(&buf, contentName, data); err != nil {
		c.String(http.StatusInternalServerError, "Template error: "+err.Error())
		return
	}
	data["contentHTML"] = template.HTML(buf.String())

	// Render layout with content injected
	c.HTML(http.StatusOK, "layout.html", data)
}

// SetupRouter configures all routes and returns the Gin engine
func (h *Handler) SetupRouter() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(CORSMiddleware())
	r.Use(gin.Logger())

	// Auth middleware (before routes)
	r.Use(auth.Middleware())

	// Set the layout as the HTML template for Gin
	r.SetHTMLTemplate(h.layoutTmpl)

	// Serve embedded static files
	staticSub, _ := fs.Sub(h.staticFS, "web/static")
	r.StaticFS("/static", http.FS(staticSub))

	// Login (before auth since middleware allows /login)
	r.GET("/login", h.PageLogin)
	r.POST("/api/login", auth.LoginEndpoint)
	r.POST("/api/logout", auth.LogoutEndpoint)

	// === Page Routes ===
	r.GET("/", h.pageDashboard)
	r.GET("/connections", h.pageConnections)
	r.GET("/tasks", h.pageTasks)
	r.GET("/history", h.pageHistory)
	r.GET("/settings", h.pageSettings)

	// === API Routes ===
	api := r.Group("/api")
	{
		// Connections
		api.GET("/connections", h.listConnections)
		api.POST("/connections", h.createConnection)
		api.GET("/connections/:id", h.getConnection)
		api.PUT("/connections/:id", h.updateConnection)
		api.DELETE("/connections/:id", h.deleteConnection)
		api.POST("/connections/:id/test", h.testConnection)
		api.POST("/connections/batch-delete", h.batchDeleteConnections)
		api.POST("/connections/test", h.testConnectionDirect)
		api.GET("/connections/:id/databases", h.listConnectionDatabases)

		// Tasks
		api.GET("/tasks", h.listTasks)
		api.POST("/tasks", h.createTask)
		api.GET("/tasks/:id", h.getTask)
		api.PUT("/tasks/:id", h.updateTask)
		api.DELETE("/tasks/:id", h.deleteTask)
		api.POST("/tasks/batch-delete", h.batchDeleteTasks)
		api.POST("/tasks/:id/run", h.runBackup)

		// Records
		api.GET("/records", h.listRecords)
		api.GET("/records/:id/download", h.downloadBackup)
		api.DELETE("/records/:id", h.deleteRecord)
		api.POST("/records/batch-delete", h.batchDeleteRecords)

		// Dashboard
		api.GET("/dashboard/stats", h.dashboardStats)

		// Settings
		api.GET("/settings", h.getSettings)
		api.PUT("/settings", h.updateSettings)
	}

	return r
}

// === Page Handlers ===

func (h *Handler) pageDashboard(c *gin.Context) {
	h.renderPage(c, "dashboard_content", gin.H{"title": "Dashboard"})
}

func (h *Handler) pageConnections(c *gin.Context) {
	h.renderPage(c, "connections_content", gin.H{"title": "Connections"})
}

func (h *Handler) pageTasks(c *gin.Context) {
	h.renderPage(c, "tasks_content", gin.H{"title": "Backup Tasks"})
}

func (h *Handler) pageHistory(c *gin.Context) {
	h.renderPage(c, "history_content", gin.H{"title": "Backup History"})
}

func (h *Handler) pageSettings(c *gin.Context) {
	h.renderPage(c, "settings_content", gin.H{"title": "Settings"})
}

// === Connection Handlers ===

func (h *Handler) listConnections(c *gin.Context) {
	conns, err := database.ListConnections()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if conns == nil {
		conns = []database.ConnectionRow{}
	}
	for i := range conns {
		maskPassword(&conns[i])
	}
	c.JSON(http.StatusOK, conns)
}

func (h *Handler) createConnection(c *gin.Context) {
	var input struct {
		Name     string `json:"name"`
		DBType   string `json:"db_type"`
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Encrypt password (skip if masked)
	encPass := ""
	if input.Password != maskedValue {
		encPass, _ = crypto.Encrypt(input.Password)
	}

	id, err := database.CreateConnection(database.ConnectionInput{
		Name: input.Name, DBType: input.DBType, Host: input.Host,
		Port: input.Port, Username: input.Username, Password: encPass,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	conn, _ := database.GetConnection(id)
	maskPassword(conn)
	c.JSON(http.StatusCreated, conn)
}

func (h *Handler) getConnection(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	conn, err := database.GetConnection(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "连接不存在"})
		return
	}
	maskPassword(conn)
	c.JSON(http.StatusOK, conn)
}

func (h *Handler) updateConnection(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var input struct {
		Name     string `json:"name"`
		DBType   string `json:"db_type"`
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	encPass := input.Password
	if input.Password == maskedValue {
		// Keep existing password
		if old, _ := database.GetConnection(id); old != nil {
			encPass = old.Password
		}
	} else {
		encPass, _ = crypto.Encrypt(input.Password)
	}

	if err := database.UpdateConnection(id, database.ConnectionInput{
		Name: input.Name, DBType: input.DBType, Host: input.Host,
		Port: input.Port, Username: input.Username, Password: encPass,
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	conn, _ := database.GetConnection(id)
	maskPassword(conn)
	c.JSON(http.StatusOK, conn)
}

func (h *Handler) deleteConnection(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := database.DeleteConnection(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

func (h *Handler) testConnection(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	conn, err := database.GetConnection(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "连接不存在"})
		return
	}

	plainPass, _ := crypto.Decrypt(conn.Password)

	engineCfg := backup.Config{
		Type: conn.DBType, Host: conn.Host, Port: conn.Port,
		Username: conn.Username, Password: plainPass,
		Databases: []string{"*"},
	}

	var engine backup.Engine
	switch engineCfg.Type {
	case "mysql":
		engine, err = backup.NewMySQLEngine(engineCfg)
	case "postgresql":
		engine, err = backup.NewPostgreSQLEngine(engineCfg)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported database type"})
		return
	}
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	defer engine.Close()

	start := time.Now()
	if err := engine.TestConnection(); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	latency := time.Since(start).Milliseconds()

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"message":    fmt.Sprintf("Connection successful (latency: %dms)", latency),
		"latency_ms": latency,
	})
}

func (h *Handler) listConnectionDatabases(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	conn, err := database.GetConnection(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "连接不存在"})
		return
	}

	plainPass, _ := crypto.Decrypt(conn.Password)

	engineCfg := backup.Config{
		Type: conn.DBType, Host: conn.Host, Port: conn.Port,
		Username: conn.Username, Password: plainPass,
	}

	var engine backup.Engine
	switch engineCfg.Type {
	case "mysql":
		engine, err = backup.NewMySQLEngine(engineCfg)
	case "postgresql":
		engine, err = backup.NewPostgreSQLEngine(engineCfg)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "不支持的数据库类型"})
		return
	}
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"databases": []string{}, "error": err.Error()})
		return
	}
	defer engine.Close()

	dbs, err := engine.ListDatabases()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"databases": []string{}, "error": err.Error()})
		return
	}
	if dbs == nil {
		dbs = []string{}
	}
	c.JSON(http.StatusOK, gin.H{"databases": dbs})
}

// === Task Handlers ===

func (h *Handler) listTasks(c *gin.Context) {
	tasks, err := database.ListTasks()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if tasks == nil {
		tasks = []database.TaskRow{}
	}
	for i := range tasks {
		maskTaskSecrets(&tasks[i])
	}
	c.JSON(http.StatusOK, tasks)
}

func (h *Handler) createTask(c *gin.Context) {
	var input struct {
		Name          string                 `json:"name"`
		ConnectionID  int64                  `json:"connection_id"`
		Databases     []string               `json:"databases"`
		BackupParams  map[string]interface{} `json:"backup_params"`
		StorageType   string                 `json:"storage_type"`
		LocalPath     string                 `json:"local_path"`
		RemoteHost    string                 `json:"remote_host"`
		RemotePort    int                    `json:"remote_port"`
		RemoteUser    string                 `json:"remote_user"`
		RemotePass    string                 `json:"remote_pass"`
		RemoteKey     string                 `json:"remote_key"`
		RemotePath    string                 `json:"remote_path"`
		MaxBackups    int                    `json:"max_backups"`
		RetentionDays int                    `json:"retention_days"`
		CronExpr      string                 `json:"cron_expr"`
		Enabled       bool                   `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if input.LocalPath == "" {
		input.LocalPath = h.cfg.Backup.DefaultDir
	}

	// Marshal databases and params to JSON strings
	databasesJSON := marshalJSON(input.Databases)
	if databasesJSON == "" || databasesJSON == "null" {
		databasesJSON = `["*"]`
	}
	paramsJSON := marshalJSON(input.BackupParams)
	if paramsJSON == "" || paramsJSON == "null" {
		paramsJSON = "{}"
	}

	// Get connection for param validation
	conn, err := database.GetConnection(input.ConnectionID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "连接不存在"})
		return
	}

	// Validate backup params
	if input.BackupParams != nil && len(input.BackupParams) > 0 {
		if err := backup.ValidateParams(conn.DBType, input.BackupParams); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "备份参数错误: " + err.Error()})
			return
		}
	}

	// Normalize and validate cron BEFORE saving to DB
	if input.Enabled && input.CronExpr != "" {
		normalized, err := scheduler.NormalizeCron(input.CronExpr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		input.CronExpr = normalized
	}

	id, err := database.CreateTask(database.TaskInput{
		Name: input.Name, ConnectionID: input.ConnectionID,
		Databases: databasesJSON, BackupParams: paramsJSON,
		StorageType: input.StorageType,
		LocalPath: input.LocalPath, RemoteHost: input.RemoteHost, RemotePort: input.RemotePort,
		RemoteUser: input.RemoteUser, RemotePass: crypto.MustEncrypt(input.RemotePass), RemoteKey: crypto.MustEncrypt(input.RemoteKey), RemotePath: input.RemotePath,
		MaxBackups: input.MaxBackups, RetentionDays: input.RetentionDays, CronExpr: input.CronExpr,
		Enabled: input.Enabled,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Register with scheduler
	if input.Enabled && input.CronExpr != "" {
		if err := h.scheduler.AddTask(id, input.CronExpr); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid cron expression: " + err.Error()})
			return
		}
	}

	task, _ := database.GetTask(id)
	maskTaskSecrets(task)
	c.JSON(http.StatusCreated, task)
}

func (h *Handler) getTask(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	task, err := database.GetTask(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Task not found"})
		return
	}
	maskTaskSecrets(task)
	c.JSON(http.StatusOK, task)
}

func (h *Handler) updateTask(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var input struct {
		Name          string                 `json:"name"`
		ConnectionID  int64                  `json:"connection_id"`
		Databases     []string               `json:"databases"`
		BackupParams  map[string]interface{} `json:"backup_params"`
		StorageType   string                 `json:"storage_type"`
		LocalPath     string                 `json:"local_path"`
		RemoteHost    string                 `json:"remote_host"`
		RemotePort    int                    `json:"remote_port"`
		RemoteUser    string                 `json:"remote_user"`
		RemotePass    string                 `json:"remote_pass"`
		RemoteKey     string                 `json:"remote_key"`
		RemotePath    string                 `json:"remote_path"`
		MaxBackups    int                    `json:"max_backups"`
		RetentionDays int                    `json:"retention_days"`
		CronExpr      string                 `json:"cron_expr"`
		Enabled       bool                   `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	databasesJSON := marshalJSON(input.Databases)
	if databasesJSON == "" || databasesJSON == "null" {
		databasesJSON = `["*"]`
	}
	paramsJSON := marshalJSON(input.BackupParams)
	if paramsJSON == "" || paramsJSON == "null" {
		paramsJSON = "{}"
	}

	// Validate backup params
	if input.BackupParams != nil && len(input.BackupParams) > 0 {
		task, _ := database.GetTask(id)
		conn, _ := database.GetConnection(task.ConnectionID)
		if conn != nil {
			if err := backup.ValidateParams(conn.DBType, input.BackupParams); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "备份参数错误: " + err.Error()})
				return
			}
		}
	}

	// Normalize and validate cron BEFORE saving to DB
	if input.Enabled && input.CronExpr != "" {
		normalized, err := scheduler.NormalizeCron(input.CronExpr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		input.CronExpr = normalized
	}

	// Keep old encrypted values if masked
	remotePass := input.RemotePass
	remoteKey := input.RemoteKey
	if input.RemotePass == maskedValue || input.RemoteKey == maskedValue {
		if old, _ := database.GetTask(id); old != nil {
			if input.RemotePass == maskedValue { remotePass = old.RemotePass }
			if input.RemoteKey == maskedValue { remoteKey = old.RemoteKey }
		}
	}
	if input.RemotePass != maskedValue { remotePass = crypto.MustEncrypt(input.RemotePass) }
	if input.RemoteKey != maskedValue { remoteKey = crypto.MustEncrypt(input.RemoteKey) }

	if err := database.UpdateTask(id, database.TaskInput{
		Name: input.Name, ConnectionID: input.ConnectionID,
		Databases: databasesJSON, BackupParams: paramsJSON,
		StorageType: input.StorageType,
		LocalPath: input.LocalPath, RemoteHost: input.RemoteHost, RemotePort: input.RemotePort,
		RemoteUser: input.RemoteUser, RemotePass: remotePass, RemoteKey: remoteKey, RemotePath: input.RemotePath,
		MaxBackups: input.MaxBackups, RetentionDays: input.RetentionDays, CronExpr: input.CronExpr,
		Enabled: input.Enabled,
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Update scheduler
	h.scheduler.UpdateTask(id, input.Enabled, input.CronExpr)

	task, _ := database.GetTask(id)
	maskTaskSecrets(task)
	c.JSON(http.StatusOK, task)
}

func (h *Handler) deleteTask(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	h.scheduler.RemoveTask(id)
	if err := database.DeleteTask(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

func (h *Handler) runBackup(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)

	task, err := database.GetTask(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Task not found"})
		return
	}

	recordID, err := h.manager.ExecuteBackup(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   fmt.Sprintf("Backup started for task: %s", task.Name),
		"record_id": recordID,
	})
}

// === Record Handlers ===

func (h *Handler) listRecords(c *gin.Context) {
	taskID, _ := strconv.ParseInt(c.Query("task_id"), 10, 64)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))

	offset := (page - 1) * perPage
	total, _ := database.CountRecords(taskID)
	records, err := database.ListRecords(taskID, perPage, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if records == nil {
		records = []database.RecordRow{}
	}

	c.JSON(http.StatusOK, gin.H{
		"records": records,
		"total":   total,
		"page":    page,
	})
}

func (h *Handler) downloadBackup(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	record, err := database.GetRecord(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Record not found"})
		return
	}

	if record.Status != "success" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Backup file not available (status: " + record.Status + ")"})
		return
	}

	if _, err := os.Stat(record.FilePath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Backup file not found on disk"})
		return
	}

	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", record.FileName))
	c.Header("Content-Type", "application/gzip")
	c.File(record.FilePath)
}

func (h *Handler) deleteRecord(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	record, err := database.GetRecord(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Record not found"})
		return
	}

	if record.FilePath != "" {
		os.Remove(record.FilePath)
	}

	if err := database.DeleteRecord(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

// === Dashboard Handlers ===

func (h *Handler) dashboardStats(c *gin.Context) {
	stats, err := database.GetDashboardStats()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	successRate := float64(0)
	if stats.TotalRecords > 0 {
		successRate = float64(stats.SuccessRecords) / float64(stats.TotalRecords) * 100
	}

	recent, _ := database.GetRecentRecords(10)
	if recent == nil {
		recent = []database.RecordRow{}
	}

	c.JSON(http.StatusOK, gin.H{
		"total_tasks":      stats.TotalTasks,
		"enabled_tasks":    stats.EnabledTasks,
		"total_backups":    stats.TotalRecords,
		"success_backups":  stats.SuccessRecords,
		"failed_backups":   stats.FailedRecords,
		"success_rate":     successRate,
		"total_size":       stats.TotalSize,
		"last_backup_time": stats.LastBackupTime,
		"recent_records":   recent,
	})
}

// === Settings Handlers ===

func (h *Handler) getSettings(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"server_port":        h.cfg.Server.Port,
		"default_backup_dir": h.cfg.Backup.DefaultDir,
		"log_retention_days": h.cfg.Logging.RetentionDays,
	})
}

func (h *Handler) updateSettings(c *gin.Context) {
	var input struct {
		ServerPort       int    `json:"server_port"`
		DefaultBackupDir string `json:"default_backup_dir"`
		LogRetentionDays int    `json:"log_retention_days"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	h.cfg.Server.Port = input.ServerPort
	h.cfg.Backup.DefaultDir = input.DefaultBackupDir
	h.cfg.Logging.RetentionDays = input.LogRetentionDays

	if err := config.Save(h.cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	os.MkdirAll(h.cfg.Backup.DefaultDir, 0755)
	c.JSON(http.StatusOK, gin.H{"message": "设置已保存"})
}

// testConnectionDirect tests a connection without needing it saved first
func (h *Handler) testConnectionDirect(c *gin.Context) {
	var input struct {
		DBType   string `json:"db_type"`
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "参数错误: " + err.Error()})
		return
	}

	engineCfg := backup.Config{
		Type: input.DBType, Host: input.Host, Port: input.Port,
		Username: input.Username, Password: input.Password,
		Databases: []string{"*"},
	}

	var engine backup.Engine
	var err error
	switch engineCfg.Type {
	case "mysql":
		engine, err = backup.NewMySQLEngine(engineCfg)
	case "postgresql":
		engine, err = backup.NewPostgreSQLEngine(engineCfg)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "不支持的数据库类型"})
		return
	}
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "连接失败: " + err.Error()})
		return
	}
	defer engine.Close()

	start := time.Now()
	if err := engine.TestConnection(); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "连接失败: " + err.Error()})
		return
	}
	latency := time.Since(start).Milliseconds()

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"message":    fmt.Sprintf("连接成功（延迟: %dms）", latency),
		"latency_ms": latency,
	})
}

// batchDeleteRecords deletes multiple backup records at once
func (h *Handler) batchDeleteRecords(c *gin.Context) {
	var input struct {
		IDs []int64 `json:"ids"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	if len(input.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择要删除的记录"})
		return
	}

	var deleted, failed int
	for _, id := range input.IDs {
		record, err := database.GetRecord(id)
		if err != nil {
			failed++
			continue
		}
		if record.FilePath != "" {
			os.Remove(record.FilePath)
		}
		if err := database.DeleteRecord(id); err != nil {
			failed++
		} else {
			deleted++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("成功删除 %d 条，失败 %d 条", deleted, failed),
		"deleted": deleted,
		"failed":  failed,
	})
}

// batchDeleteConnections deletes multiple connections and their related tasks
func (h *Handler) batchDeleteConnections(c *gin.Context) {
	var input struct {
		IDs []int64 `json:"ids"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || len(input.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择要删除的连接"})
		return
	}
	var deleted, failed int
	for _, id := range input.IDs {
		h.scheduler.RemoveTask(id)
		if err := database.DeleteConnection(id); err != nil {
			failed++
		} else {
			deleted++
		}
	}
	c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("成功删除 %d 个，失败 %d 个", deleted, failed)})
}

// batchDeleteTasks deletes multiple backup tasks
func (h *Handler) batchDeleteTasks(c *gin.Context) {
	var input struct {
		IDs []int64 `json:"ids"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || len(input.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择要删除的任务"})
		return
	}
	var deleted, failed int
	for _, id := range input.IDs {
		h.scheduler.RemoveTask(id)
		if err := database.DeleteTask(id); err != nil {
			failed++
		} else {
			deleted++
		}
	}
	c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("成功删除 %d 个，失败 %d 个", deleted, failed)})
}

const maskedValue = "******"

// maskPassword replaces the password field with masked value for API responses
func maskPassword(conn *database.ConnectionRow) {
	if conn != nil && conn.Password != "" {
		conn.Password = maskedValue
	}
}

// maskTaskSecrets clears sensitive fields in task API responses
func maskTaskSecrets(t *database.TaskRow) {
	if t != nil {
		if t.RemotePass != "" {
			t.RemotePass = maskedValue
		}
		if t.RemoteKey != "" {
			t.RemoteKey = maskedValue
		}
	}
}

// PageLogin serves the login page
func (h *Handler) PageLogin(c *gin.Context) {
	c.HTML(http.StatusOK, "login.html", gin.H{"title": "登录"})
}

// marshalJSON marshals a value to JSON string
func marshalJSON(v interface{}) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}
