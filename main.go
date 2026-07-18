package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"db-backup-tool/internal/api"
	"db-backup-tool/internal/auth"
	"db-backup-tool/internal/backup"
	"db-backup-tool/internal/config"
	"db-backup-tool/internal/crypto"
	"db-backup-tool/internal/database"
	"db-backup-tool/internal/scheduler"
)

func main() {
	// Determine data directory
	execPath, _ := os.Executable()
	dataDir := filepath.Join(filepath.Dir(execPath), "data")
	if _, err := os.Stat(dataDir); os.IsNotExist(err) {
		dataDir = filepath.Join(".", "data")
	}
	os.MkdirAll(dataDir, 0755)

	// Load configuration (creates config.yaml if not found)
	cfg, err := config.Load(dataDir)
	if err != nil {
		log.Printf("加载配置失败，使用默认配置: %v", err)
		cfg = config.DefaultConfig(dataDir)
	}

	// Initialize encryption
	if cfg.Security.EncryptKey == "" {
		// Generate a random key and save it
		crypto.Init("")
		cfg.Security.EncryptKey = crypto.GetKey()
		config.Save(cfg)
		log.Println("已生成新的加密密钥")
	} else {
		if err := crypto.Init(cfg.Security.EncryptKey); err != nil {
			log.Fatalf("初始化加密模块失败: %v", err)
		}
	}

	// Initialize authentication (defensive: if password is empty or looks like hex key, use default)
	if cfg.Auth.Password == "" || len(cfg.Auth.Password) == 64 {
		cfg.Auth.Password = "admin"
		config.Save(cfg)
		log.Println("密码已重置为默认值: admin，请修改 config.yaml")
	}
	auth.Init(cfg.Auth.Password)
	log.Println("认证模块初始化完成")

	// Initialize database
	if err := database.Init(dataDir); err != nil {
		log.Fatalf("初始化数据库失败: %v", err)
	}
	defer database.Close()
	log.Println("数据库初始化完成")

	// Create backup manager
	manager := backup.NewManager(dataDir)

	// Start scheduler
	sched := scheduler.New(manager)
	sched.Start()
	defer sched.Stop()

	// Setup API
	handler := api.NewHandler(manager, sched, cfg, templatesFS, staticFS)
	router := handler.SetupRouter()

	// Start server
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	log.Printf("数据库备份工具启动于 http://localhost%s", addr)

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("正在关闭...")
		sched.Stop()
		database.Close()
		os.Exit(0)
	}()

	if err := router.Run(addr); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}
