package scheduler

import (
	"fmt"
	"log"
	"strings"
	"sync"

	"db-backup-tool/internal/backup"
	"db-backup-tool/internal/database"

	"github.com/robfig/cron/v3"
)

// cronParser is the standard 5-field parser
var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// NormalizeCron tries to fix common cron expression mistakes.
// Returns the normalized expression and any error.
func NormalizeCron(expr string) (string, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return "", nil
	}

	// Try parsing as-is (5-field)
	if _, err := cronParser.Parse(expr); err == nil {
		return expr, nil
	}

	// Count fields
	fields := strings.Fields(expr)
	if len(fields) == 4 {
		// Auto-pad: assume user forgot day-of-week, append *
		padded := expr + " *"
		if _, err := cronParser.Parse(padded); err == nil {
			return padded, nil
		}
	}

	// If still invalid, show helpful error
	return "", fmt.Errorf("Cron 格式无效：需要 5 个字段（分 时 日 月 周），当前输入 \"%s\" 为 %d 个字段\n常用示例：\n  0 2 * * *      每天凌晨2点\n  */5 * * * *     每5分钟\n  0 9 * * 1-5    工作日早上9点", expr, len(fields))
}

// ValidateCron checks a cron expression and returns an error if invalid
func ValidateCron(expr string) error {
	_, err := NormalizeCron(expr)
	return err
}

// Scheduler manages cron-based backup execution
type Scheduler struct {
	cron    *cron.Cron
	manager *backup.Manager
	mu      sync.Mutex
	entries map[int64]cron.EntryID // taskID -> cron entry ID
}

// New creates a new scheduler
func New(manager *backup.Manager) *Scheduler {
	return &Scheduler{
		cron: cron.New(
			cron.WithParser(cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)),
			cron.WithLogger(cron.VerbosePrintfLogger(log.New(log.Writer(), "cron: ", log.LstdFlags))),
		),
		manager: manager,
		entries: make(map[int64]cron.EntryID),
	}
}

// Start loads all enabled tasks and starts the cron engine
func (s *Scheduler) Start() {
	tasks, err := database.ListTasks()
	if err != nil {
		log.Printf("scheduler: failed to load tasks: %v", err)
		return
	}

	for _, task := range tasks {
		if task.Enabled && task.CronExpr != "" {
			if err := s.AddTask(task.ID, task.CronExpr); err != nil {
				log.Printf("scheduler: failed to add task %d (%s): %v", task.ID, task.Name, err)
			}
		}
	}

	s.cron.Start()
	log.Printf("scheduler: started with %d jobs", len(s.entries))
}

// Stop gracefully stops the scheduler
func (s *Scheduler) Stop() {
	ctx := s.cron.Stop()
	<-ctx.Done()
	log.Println("scheduler: stopped")
}

// AddTask adds or updates a cron job for the given task
func (s *Scheduler) AddTask(taskID int64, cronExpr string) error {
	normalized, err := NormalizeCron(cronExpr)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove existing entry if present
	s.removeEntry(taskID)

	entryID, err := s.cron.AddFunc(normalized, func() {
		log.Printf("scheduler: running scheduled backup for task %d", taskID)
		recordID, err := s.manager.ExecuteBackup(taskID)
		if err != nil {
			log.Printf("scheduler: backup task %d failed: %v", taskID, err)
		} else {
			log.Printf("scheduler: backup task %d completed, record %d", taskID, recordID)
		}
	})
	if err != nil {
		return err
	}

	s.entries[taskID] = entryID
	log.Printf("scheduler: added task %d with cron '%s'", taskID, cronExpr)
	return nil
}

// RemoveTask removes a cron job for the given task
func (s *Scheduler) RemoveTask(taskID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removeEntry(taskID)
}

func (s *Scheduler) removeEntry(taskID int64) {
	if entryID, ok := s.entries[taskID]; ok {
		s.cron.Remove(entryID)
		delete(s.entries, taskID)
	}
}

// UpdateTask updates the schedule for a task (enable/disable/cron change)
func (s *Scheduler) UpdateTask(taskID int64, enabled bool, cronExpr string) {
	s.RemoveTask(taskID)
	if enabled && cronExpr != "" {
		s.AddTask(taskID, cronExpr)
	}
}
