package backup

import (
	"fmt"
	"io"
)

// Engine defines the interface for database backup operations
type Engine interface {
	Dump(w io.Writer) error
	DumpDatabase(w io.Writer, dbName string) error
	TestConnection() error
	ListDatabases() ([]string, error)
	Close() error
	DBType() string
	ValidateParams(params map[string]interface{}) error
}

// Config holds parameters to create a backup engine
type Config struct {
	Type      string                 // "mysql" | "postgresql"
	Host      string
	Port      int
	Username  string
	Password  string
	Databases []string               // ["*"] = all, ["db1","db2"] = specific
	Params    map[string]interface{} // custom backup parameters
}

// ==================== Parameter Validation ====================

// Known MySQL backup parameters
var validMySQLParams = map[string]string{
	"no_data":             "bool",
	"no_create_info":      "bool",
	"skip_lock_tables":    "bool",
	"single_transaction":  "bool",
	"routines":            "bool",
	"triggers":            "bool",
	"ignore_tables":       "array",
	"where":               "string",
	"complete_insert":     "bool",
	"extended_insert":     "bool",
	"add_drop_table":      "bool",
	"add_drop_database":   "bool",
	"hex_blob":            "bool",
}

// Known PostgreSQL backup parameters
var validPGParams = map[string]string{
	"schema_only":     "bool",
	"data_only":       "bool",
	"clean":           "bool",
	"no_owner":        "bool",
	"if_exists":       "bool",
	"create":          "bool",
	"exclude_table":   "array",
	"exclude_schema":  "array",
	"include_schema":  "array",
	"no_comments":     "bool",
	"no_publications": "bool",
	"no_security_labels": "bool",
	"no_subscriptions":  "bool",
	"no_table_access_method": "bool",
	"no_tablespaces":   "bool",
	"rows_per_insert":  "number",
}

func ValidateParams(dbType string, params map[string]interface{}) error {
	var validParams map[string]string
	switch dbType {
	case "mysql":
		validParams = validMySQLParams
	case "postgresql":
		validParams = validPGParams
	default:
		return fmt.Errorf("不支持的数据库类型: %s", dbType)
	}

	for key, val := range params {
		expectedType, ok := validParams[key]
		if !ok {
			return fmt.Errorf("不支持的参数 '%s'，%s 备份支持的参数: %s",
				key, dbType, listKnownParams(validParams))
		}
		switch expectedType {
		case "bool":
			if _, ok := val.(bool); !ok {
				return fmt.Errorf("参数 '%s' 需要布尔值 (true/false)，当前值: %v", key, val)
			}
		case "string":
			if _, ok := val.(string); !ok {
				return fmt.Errorf("参数 '%s' 需要字符串值，当前值: %v", key, val)
			}
		case "number":
			switch val.(type) {
			case float64, int, int64:
			default:
				return fmt.Errorf("参数 '%s' 需要数字值，当前值: %v", key, val)
			}
		case "array":
			if _, ok := val.([]interface{}); !ok {
				return fmt.Errorf("参数 '%s' 需要数组值，当前值: %v", key, val)
			}
		}
	}
	return nil
}

func listKnownParams(valid map[string]string) string {
	var keys []string
	for k := range valid {
		keys = append(keys, k)
	}
	return fmt.Sprintf("%v", keys)
}

// paramBool safely extracts a bool param
func paramBool(params map[string]interface{}, key string) bool {
	if v, ok := params[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

// paramString safely extracts a string param
func paramString(params map[string]interface{}, key string) string {
	if v, ok := params[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// paramStringArray safely extracts a string array param
func paramStringArray(params map[string]interface{}, key string) []string {
	if v, ok := params[key]; ok {
		if arr, ok := v.([]interface{}); ok {
			var result []string
			for _, item := range arr {
				if s, ok := item.(string); ok {
					result = append(result, s)
				}
			}
			return result
		}
	}
	return nil
}
