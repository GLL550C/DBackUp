package backup

import (
	"database/sql"
	"fmt"
	"io"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

type MySQLEngine struct {
	cfg Config
	db  *sql.DB
}

func NewMySQLEngine(cfg Config) (*MySQLEngine, error) {
	// Connect without specifying a database to allow multi-DB dumps
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/?charset=utf8mb4&parseTime=true&multiStatements=true&timeout=30s",
		cfg.Username, cfg.Password, cfg.Host, cfg.Port,
	)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("MySQL 连接失败: %w", err)
	}
	db.SetMaxOpenConns(2)
	db.SetConnMaxLifetime(30 * time.Minute)
	return &MySQLEngine{cfg: cfg, db: db}, nil
}

func (e *MySQLEngine) DBType() string { return "mysql" }

func (e *MySQLEngine) ValidateParams(params map[string]interface{}) error {
	return ValidateParams("mysql", params)
}

func (e *MySQLEngine) TestConnection() error { return e.db.Ping() }
func (e *MySQLEngine) Close() error          { return e.db.Close() }

// ListDatabases returns all user databases
func (e *MySQLEngine) ListDatabases() ([]string, error) {
	rows, err := e.db.Query("SHOW DATABASES")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dbs []string
	sysDBs := map[string]bool{
		"information_schema": true, "mysql": true,
		"performance_schema": true, "sys": true,
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		if !sysDBs[name] {
			dbs = append(dbs, name)
		}
	}
	return dbs, rows.Err()
}

// getDatabases resolves the database list: ["*"] means all databases
func (e *MySQLEngine) getDatabases() ([]string, error) {
	if len(e.cfg.Databases) == 0 || (len(e.cfg.Databases) == 1 && e.cfg.Databases[0] == "*") {
		return e.ListDatabases()
	}
	return e.cfg.Databases, nil
}

// Dump dumps all configured databases
func (e *MySQLEngine) Dump(w io.Writer) error {
	dbs, err := e.getDatabases()
	if err != nil {
		return fmt.Errorf("获取数据库列表失败: %w", err)
	}
	if len(dbs) == 0 {
		return fmt.Errorf("没有找到可备份的数据库")
	}

	writeHeader(w, "MySQL", fmt.Sprintf("%s:%d", e.cfg.Host, e.cfg.Port))
	io.WriteString(w, "/*!40101 SET NAMES utf8mb4 */;\n\n")

	for _, dbName := range dbs {
		if err := e.DumpDatabase(w, dbName); err != nil {
			return fmt.Errorf("备份数据库 %s 失败: %w", dbName, err)
		}
	}
	return nil
}

// DumpDatabase dumps a single database
func (e *MySQLEngine) DumpDatabase(w io.Writer, dbName string) error {
	params := e.cfg.Params
	if params == nil {
		params = map[string]interface{}{}
	}

	io.WriteString(w, fmt.Sprintf("\n--\n-- 数据库: %s\n--\n\n", dbName))

	// CREATE DATABASE
	addDropDB := paramBool(params, "add_drop_database")
	if addDropDB {
		io.WriteString(w, fmt.Sprintf("DROP DATABASE IF EXISTS `%s`;\n", escapeMySQLIdent(dbName)))
	}
	io.WriteString(w, fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;\n", escapeMySQLIdent(dbName)))
	io.WriteString(w, fmt.Sprintf("USE `%s`;\n\n", escapeMySQLIdent(dbName)))

	// Get tables
	tables, err := e.getTables(dbName)
	if err != nil {
		return err
	}

	noData := paramBool(params, "no_data")
	noCreateInfo := paramBool(params, "no_create_info")
	skipLock := paramBool(params, "skip_lock_tables")
	addDropTable := paramBool(params, "add_drop_table")
	ignoreTables := paramStringArray(params, "ignore_tables")
	ignoreSet := make(map[string]bool)
	for _, t := range ignoreTables {
		ignoreSet[t] = true
	}

	io.WriteString(w, "SET FOREIGN_KEY_CHECKS = 0;\n\n")

	for _, table := range tables {
		if ignoreSet[table] {
			continue
		}

		// Table structure
		if !noCreateInfo {
			if addDropTable {
				io.WriteString(w, fmt.Sprintf("DROP TABLE IF EXISTS `%s`;\n", escapeMySQLIdent(table)))
			}
			var createSQL string
			var tmp string
			if err := e.db.QueryRow(fmt.Sprintf("SHOW CREATE TABLE `%s`.`%s`", escapeMySQLIdent(dbName), escapeMySQLIdent(table))).Scan(&tmp, &createSQL); err != nil {
				return fmt.Errorf("获取表 %s 结构失败: %w", table, err)
			}
			io.WriteString(w, createSQL+";\n\n")
		}

		// Table data
		if !noData {
			if !skipLock {
				io.WriteString(w, fmt.Sprintf("LOCK TABLES `%s` WRITE;\n", escapeMySQLIdent(table)))
			}
			if err := e.dumpTableData(w, dbName, table, params); err != nil {
				return fmt.Errorf("导出表 %s 数据失败: %w", table, err)
			}
			if !skipLock {
				io.WriteString(w, "UNLOCK TABLES;\n\n")
			}
		}
	}

	// Routines (stored procedures/functions)
	if paramBool(params, "routines") {
		if err := e.dumpRoutines(w, dbName); err != nil {
			return err
		}
	}

	// Triggers
	if paramBool(params, "triggers") {
		if err := e.dumpTriggers(w, dbName); err != nil {
			return err
		}
	}

	io.WriteString(w, "SET FOREIGN_KEY_CHECKS = 1;\n")
	return nil
}

func (e *MySQLEngine) getTables(dbName string) ([]string, error) {
	rows, err := e.db.Query(fmt.Sprintf("SHOW FULL TABLES FROM `%s` WHERE Table_type = 'BASE TABLE'", escapeMySQLIdent(dbName)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var name, tableType string
		if err := rows.Scan(&name, &tableType); err != nil {
			return nil, err
		}
		tables = append(tables, name)
	}
	return tables, rows.Err()
}

func (e *MySQLEngine) dumpTableData(w io.Writer, dbName, table string, params map[string]interface{}) error {
	whereClause := ""
	if where := paramString(params, "where"); where != "" {
		whereClause = " WHERE " + where
	}

	rows, err := e.db.Query(fmt.Sprintf("SELECT * FROM `%s`.`%s`%s", escapeMySQLIdent(dbName), escapeMySQLIdent(table), whereClause))
	if err != nil {
		return err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return err
	}
	colTypes, err := rows.ColumnTypes()
	if err != nil {
		return err
	}

	quotedCols := make([]string, len(columns))
	for i, col := range columns {
		quotedCols[i] = "`" + escapeMySQLIdent(col) + "`"
	}

	hexBlob := paramBool(params, "hex_blob")
	extendedInsert := paramBool(params, "extended_insert")
	completeInsert := paramBool(params, "complete_insert")

	rowCount := 0
	var batch []string

	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return err
		}

		valStrs := make([]string, len(values))
		for i, val := range values {
			valStrs[i] = formatMySQLValue2(val, colTypes[i], hexBlob)
		}

		if extendedInsert {
			batch = append(batch, "("+strings.Join(valStrs, ", ")+")")
			rowCount++
			if len(batch) >= 500 {
				var prefix string
				if completeInsert {
					prefix = fmt.Sprintf("INSERT INTO `%s` (%s) VALUES\n", escapeMySQLIdent(table), strings.Join(quotedCols, ", "))
				} else {
					prefix = fmt.Sprintf("INSERT INTO `%s` VALUES\n", escapeMySQLIdent(table))
				}
				io.WriteString(w, prefix+strings.Join(batch, ",\n")+";\n")
				batch = nil
			}
		} else {
			if completeInsert {
				io.WriteString(w, fmt.Sprintf("INSERT INTO `%s` (%s) VALUES (%s);\n",
					escapeMySQLIdent(table), strings.Join(quotedCols, ", "), strings.Join(valStrs, ", ")))
			} else {
				io.WriteString(w, fmt.Sprintf("INSERT INTO `%s` VALUES (%s);\n",
					escapeMySQLIdent(table), strings.Join(valStrs, ", ")))
			}
			rowCount++
		}
	}

	// Flush remaining batch
	if len(batch) > 0 {
		var prefix string
		if completeInsert {
			prefix = fmt.Sprintf("INSERT INTO `%s` (%s) VALUES\n", escapeMySQLIdent(table), strings.Join(quotedCols, ", "))
		} else {
			prefix = fmt.Sprintf("INSERT INTO `%s` VALUES\n", escapeMySQLIdent(table))
		}
		io.WriteString(w, prefix+strings.Join(batch, ",\n")+";\n")
	}

	return rows.Err()
}

func (e *MySQLEngine) dumpRoutines(w io.Writer, dbName string) error {
	rows, err := e.db.Query(fmt.Sprintf(
		"SELECT ROUTINE_NAME, ROUTINE_TYPE FROM information_schema.ROUTINES WHERE ROUTINE_SCHEMA='%s'", escapeMySQLStringRaw(dbName)),
	)
	if err != nil {
		return nil // routines table may not be accessible
	}
	defer rows.Close()

	for rows.Next() {
		var name, rtype string
		if err := rows.Scan(&name, &rtype); err != nil {
			continue
		}
		var createSQL string
		var tmp string
		if err := e.db.QueryRow(fmt.Sprintf("SHOW CREATE %s `%s`.`%s`", rtype, escapeMySQLIdent(dbName), escapeMySQLIdent(name))).Scan(&tmp, &createSQL, &tmp, &tmp, &tmp, &tmp); err != nil {
			continue
		}
		io.WriteString(w, "DELIMITER //\n"+createSQL+"//\nDELIMITER ;\n\n")
	}
	return nil
}

func (e *MySQLEngine) dumpTriggers(w io.Writer, dbName string) error {
	rows, err := e.db.Query(fmt.Sprintf("SHOW TRIGGERS FROM `%s`", escapeMySQLIdent(dbName)))
	if err != nil {
		return nil
	}
	defer rows.Close()

	cols, _ := rows.Columns()
	for rows.Next() {
		values := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			continue
		}
		// The SQL statement is usually the 4th column in SHOW TRIGGERS
		if len(values) >= 4 {
			if sql, ok := values[3].([]byte); ok {
				io.WriteString(w, "DELIMITER //\n"+string(sql)+"//\nDELIMITER ;\n\n")
			}
		}
	}
	return nil
}

func escapeMySQLIdent(s string) string {
	return strings.ReplaceAll(s, "`", "``")
}

func escapeMySQLStringRaw(s string) string {
	return strings.ReplaceAll(s, "'", "\\'")
}

func writeHeader(w io.Writer, dbType, host string) {
	io.WriteString(w, fmt.Sprintf("-- %s 数据库备份\n-- 备份工具: DB Backup Tool\n-- 主机: %s\n-- 时间: %s\n\n",
		dbType, host, time.Now().Format("2006-01-02 15:04:05")))
}

// formatMySQLValue2 formats a value for SQL INSERT, with hex_blob support
func formatMySQLValue2(val interface{}, colType *sql.ColumnType, hexBlob bool) string {
	if val == nil {
		return "NULL"
	}
	dbType := strings.ToUpper(colType.DatabaseTypeName())
	switch v := val.(type) {
	case []byte:
		if hexBlob && (strings.Contains(dbType, "BLOB") || strings.Contains(dbType, "BINARY")) {
			return fmt.Sprintf("0x%X", v)
		}
		s := string(v)
		if isNumericMySQLType(dbType) && isNumericString(s) {
			return s
		}
		return fmt.Sprintf("'%s'", escapeSQLStr(s))
	case string:
		return fmt.Sprintf("'%s'", escapeSQLStr(v))
	case int64:
		return fmt.Sprintf("%d", v)
	case float64:
		return fmt.Sprintf("%g", v)
	case bool:
		if v {
			return "1"
		}
		return "0"
	case time.Time:
		return fmt.Sprintf("'%s'", v.Format("2006-01-02 15:04:05"))
	default:
		return fmt.Sprintf("'%s'", escapeSQLStr(fmt.Sprintf("%v", v)))
	}
}

func escapeSQLStr(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "'", "\\'")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	s = strings.ReplaceAll(s, "\x00", "\\0")
	return s
}

func isNumericMySQLType(dbType string) bool {
	for _, t := range []string{"INT", "TINYINT", "SMALLINT", "MEDIUMINT", "BIGINT", "FLOAT", "DOUBLE", "DECIMAL", "NUMERIC", "REAL", "BIT"} {
		if strings.HasPrefix(dbType, t) {
			return true
		}
	}
	return false
}

func isNumericString(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || c == '.' || c == '-' || c == '+' || c == 'e' || c == 'E') {
			return false
		}
	}
	return true
}
