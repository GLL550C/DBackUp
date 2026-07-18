package backup

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type PostgreSQLEngine struct {
	cfg Config
	conn *pgx.Conn
}

func NewPostgreSQLEngine(cfg Config) (*PostgreSQLEngine, error) {
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/postgres?sslmode=disable&connect_timeout=30",
		cfg.Username, cfg.Password, cfg.Host, cfg.Port,
	)
	conn, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		return nil, fmt.Errorf("PostgreSQL 连接失败: %w", err)
	}
	return &PostgreSQLEngine{cfg: cfg, conn: conn}, nil
}

func (e *PostgreSQLEngine) DBType() string { return "postgresql" }

func (e *PostgreSQLEngine) ValidateParams(params map[string]interface{}) error {
	return ValidateParams("postgresql", params)
}

func (e *PostgreSQLEngine) TestConnection() error {
	return e.conn.Ping(context.Background())
}

func (e *PostgreSQLEngine) Close() error {
	return e.conn.Close(context.Background())
}

func (e *PostgreSQLEngine) ListDatabases() ([]string, error) {
	rows, err := e.conn.Query(context.Background(),
		`SELECT datname FROM pg_database WHERE datistemplate = false AND datname NOT IN ('postgres') ORDER BY datname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var dbs []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		dbs = append(dbs, name)
	}
	return dbs, rows.Err()
}

func (e *PostgreSQLEngine) getDatabases() ([]string, error) {
	if len(e.cfg.Databases) == 0 || (len(e.cfg.Databases) == 1 && e.cfg.Databases[0] == "*") {
		return e.ListDatabases()
	}
	return e.cfg.Databases, nil
}

func (e *PostgreSQLEngine) Dump(w io.Writer) error {
	dbs, err := e.getDatabases()
	if err != nil {
		return fmt.Errorf("获取数据库列表失败: %w", err)
	}
	if len(dbs) == 0 {
		return fmt.Errorf("没有找到可备份的数据库")
	}

	writeHeader(w, "PostgreSQL", fmt.Sprintf("%s:%d", e.cfg.Host, e.cfg.Port))
	io.WriteString(w, "SET statement_timeout = 0;\nSET client_encoding = 'UTF8';\nSET standard_conforming_strings = on;\n\n")

	for _, dbName := range dbs {
		if err := e.DumpDatabase(w, dbName); err != nil {
			return fmt.Errorf("备份数据库 %s 失败: %w", dbName, err)
		}
	}
	return nil
}

func (e *PostgreSQLEngine) DumpDatabase(w io.Writer, dbName string) error {
	params := e.cfg.Params
	if params == nil {
		params = map[string]interface{}{}
	}

	io.WriteString(w, fmt.Sprintf("\n--\n-- 数据库: %s\n--\n\n", dbName))

	clean := paramBool(params, "clean")
	ifExists := paramBool(params, "if_exists")
	create := paramBool(params, "create")
	noOwner := paramBool(params, "no_owner")

	// Connect to the specific database for dumping
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable&connect_timeout=30",
		e.cfg.Username, e.cfg.Password, e.cfg.Host, e.cfg.Port, dbName,
	)
	dbConn, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		return fmt.Errorf("连接数据库 %s 失败: %w", dbName, err)
	}
	defer dbConn.Close(context.Background())

	// CREATE DATABASE
	if create {
		ifClean := ""
		if ifExists {
			ifClean = "IF NOT EXISTS "
		}
		io.WriteString(w, fmt.Sprintf("CREATE DATABASE %s%s;\n", ifClean, quotePG(dbName)))
	}

	// Get schemas
	schemas, err := getPGSchemas(dbConn)
	if err != nil {
		return err
	}

	schemaOnly := paramBool(params, "schema_only")
	dataOnly := paramBool(params, "data_only")
	excludeTables := paramStringArray(params, "exclude_table")
	excludeSchemas := paramStringArray(params, "exclude_schema")
	includeSchemas := paramStringArray(params, "include_schema")

	exclTableSet := make(map[string]bool)
	for _, t := range excludeTables {
		exclTableSet[t] = true
	}
	exclSchemaSet := make(map[string]bool)
	for _, s := range excludeSchemas {
		exclSchemaSet[s] = true
	}
	inclSchemaSet := make(map[string]bool)
	for _, s := range includeSchemas {
		inclSchemaSet[s] = true
	}

	io.WriteString(w, fmt.Sprintf("\\connect %s\n\n", quotePG(dbName)))

	for _, schema := range schemas {
		if exclSchemaSet[schema] {
			continue
		}
		if len(inclSchemaSet) > 0 && !inclSchemaSet[schema] {
			continue
		}

		io.WriteString(w, fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s;\n", quotePG(schema)))
		io.WriteString(w, fmt.Sprintf("SET search_path = %s, pg_catalog;\n\n", quotePG(schema)))

		// Dump sequences
		if !dataOnly {
			if err := dumpPGSequences(dbConn, w, schema); err != nil {
				return err
			}
		}

		// Dump tables
		tables, err := getPGTables(dbConn, schema)
		if err != nil {
			return err
		}

		for _, table := range tables {
			if exclTableSet[table] || exclTableSet[schema+"."+table] {
				continue
			}

			// DDL
			if !dataOnly {
				if err := dumpPGTableDDL(dbConn, w, schema, table, clean, ifExists, noOwner); err != nil {
					return fmt.Errorf("导出表 %s.%s 结构失败: %w", schema, table, err)
				}
			}

			// Data
			if !schemaOnly {
				if err := dumpPGTableData(dbConn, w, schema, table, params); err != nil {
					return fmt.Errorf("导出表 %s.%s 数据失败: %w", schema, table, err)
				}
			}
		}
	}

	return nil
}

func getPGSchemas(conn *pgx.Conn) ([]string, error) {
	rows, err := conn.Query(context.Background(),
		`SELECT schema_name FROM information_schema.schemata
		 WHERE schema_name NOT IN ('information_schema', 'pg_catalog', 'pg_toast')
		 ORDER BY schema_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var schemas []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		schemas = append(schemas, name)
	}
	return schemas, rows.Err()
}

func getPGTables(conn *pgx.Conn, schema string) ([]string, error) {
	rows, err := conn.Query(context.Background(),
		`SELECT table_name FROM information_schema.tables
		 WHERE table_schema=$1 AND table_type='BASE TABLE' ORDER BY table_name`, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		tables = append(tables, name)
	}
	return tables, rows.Err()
}

func dumpPGTableDDL(conn *pgx.Conn, w io.Writer, schema, table string, clean, ifExists, noOwner bool) error {
	fullName := quotePG(schema) + "." + quotePG(table)

	if clean {
		ifDrop := "DROP TABLE "
		if ifExists {
			ifDrop = "DROP TABLE IF EXISTS "
		}
		io.WriteString(w, ifDrop+fullName+" CASCADE;\n")
	}

	// Get columns
	colRows, err := conn.Query(context.Background(),
		`SELECT a.attname,
			pg_catalog.format_type(a.atttypid, a.atttypmod),
			a.attnotnull,
			COALESCE(pg_catalog.pg_get_expr(ad.adbin, ad.adrelid), ''),
			a.attidentity != ''
		 FROM pg_catalog.pg_attribute a
		 LEFT JOIN pg_catalog.pg_attrdef ad ON a.attrelid = ad.adrelid AND a.attnum = ad.adnum
		 WHERE a.attrelid = $1::regclass AND a.attnum > 0 AND NOT a.attisdropped
		 ORDER BY a.attnum`, fullName)
	if err != nil {
		return err
	}
	defer colRows.Close()

	var colDefs []string
	for colRows.Next() {
		var name, colType, defaultVal string
		var notNull, isIdentity bool
		if err := colRows.Scan(&name, &colType, &notNull, &defaultVal, &isIdentity); err != nil {
			return err
		}
		def := quotePG(name) + " " + colType
		if notNull {
			def += " NOT NULL"
		}
		if defaultVal != "" {
			def += " DEFAULT " + defaultVal
		}
		colDefs = append(colDefs, def)
	}
	if err := colRows.Err(); err != nil {
		return err
	}

	createSQL := "CREATE TABLE " + fullName + " (\n    " + strings.Join(colDefs, ",\n    ") + "\n)"
	if noOwner {
		createSQL += ";"
	} else {
		// Get owner
		var owner string
		if err := conn.QueryRow(context.Background(),
			`SELECT pg_catalog.pg_get_userbyid(c.relowner)
			 FROM pg_catalog.pg_class c WHERE c.oid = $1::regclass`, fullName).Scan(&owner); err == nil && owner != "" {
			createSQL += ";\nALTER TABLE " + fullName + " OWNER TO " + quotePG(owner) + ";"
		} else {
			createSQL += ";"
		}
	}
	io.WriteString(w, createSQL+"\n\n")
	return nil
}

func dumpPGSequences(conn *pgx.Conn, w io.Writer, schema string) error {
	rows, err := conn.Query(context.Background(),
		`SELECT sequence_name FROM information_schema.sequences WHERE sequence_schema=$1`, schema)
	if err != nil {
		return nil
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		var lastVal int64
		err := conn.QueryRow(context.Background(),
			fmt.Sprintf("SELECT last_value FROM %s.%s", quotePG(schema), quotePG(name))).Scan(&lastVal)
		if err != nil {
			continue
		}
		io.WriteString(w, fmt.Sprintf("CREATE SEQUENCE IF NOT EXISTS %s.%s;\n", quotePG(schema), quotePG(name)))
		io.WriteString(w, fmt.Sprintf("SELECT pg_catalog.setval('%s.%s', %d, true);\n",
			quotePG(schema), quotePG(name), lastVal))
	}
	return nil
}

func dumpPGTableData(conn *pgx.Conn, w io.Writer, schema, table string, params map[string]interface{}) error {
	fullName := quotePG(schema) + "." + quotePG(table)
	rowsPerInsert := 1
	if v := paramString(params, "rows_per_insert"); v != "" {
		fmt.Sscanf(v, "%d", &rowsPerInsert)
	}

	rows, err := conn.Query(context.Background(), fmt.Sprintf("SELECT * FROM %s", fullName))
	if err != nil {
		return err
	}
	defer rows.Close()

	descriptions := rows.FieldDescriptions()
	columns := make([]string, len(descriptions))
	for i, d := range descriptions {
		columns[i] = string(d.Name)
	}

	quotedCols := make([]string, len(columns))
	for i, col := range columns {
		quotedCols[i] = quotePG(col)
	}

	var batch []string
	rowCount := 0
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return err
		}
		valStrs := make([]string, len(values))
		for i, val := range values {
			pgType := ""
			if i < len(descriptions) {
				pgType = pgOIDToName(descriptions[i].DataTypeOID)
			}
			valStrs[i] = formatPGVal(val, pgType)
		}

		if rowsPerInsert > 1 {
			batch = append(batch, "("+strings.Join(valStrs, ", ")+")")
			if len(batch) >= rowsPerInsert {
				io.WriteString(w, fmt.Sprintf("INSERT INTO %s (%s) VALUES\n%s;\n",
					fullName, strings.Join(quotedCols, ", "), strings.Join(batch, ",\n")))
				batch = nil
			}
		} else {
			io.WriteString(w, fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s);\n",
				fullName, strings.Join(quotedCols, ", "), strings.Join(valStrs, ", ")))
		}
		rowCount++
	}

	if len(batch) > 0 {
		io.WriteString(w, fmt.Sprintf("INSERT INTO %s (%s) VALUES\n%s;\n",
			fullName, strings.Join(quotedCols, ", "), strings.Join(batch, ",\n")))
	}

	return rows.Err()
}

func formatPGVal(val interface{}, pgType string) string {
	if val == nil {
		return "NULL"
	}
	switch v := val.(type) {
	case []byte:
		s := string(v)
		if isNumericPG(pgType) {
			return s
		}
		return fmt.Sprintf("'%s'", escapeSQLStr(s))
	case string:
		return fmt.Sprintf("'%s'", escapeSQLStr(v))
	case int64:
		return fmt.Sprintf("%d", v)
	case int32:
		return fmt.Sprintf("%d", v)
	case int16:
		return fmt.Sprintf("%d", v)
	case float64:
		return fmt.Sprintf("%g", v)
	case float32:
		return fmt.Sprintf("%g", v)
	case bool:
		if v {
			return "true"
		}
		return "false"
	case time.Time:
		return fmt.Sprintf("'%s'", v.Format("2006-01-02 15:04:05"))
	default:
		return fmt.Sprintf("'%s'", escapeSQLStr(fmt.Sprintf("%v", v)))
	}
}

func quotePG(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func pgOIDToName(oid uint32) string {
	types := map[uint32]string{
		16: "bool", 20: "int8", 21: "int2", 23: "int4", 25: "text",
		26: "oid", 700: "float4", 701: "float8", 1043: "varchar",
		1082: "date", 1114: "timestamp", 1184: "timestamptz", 1700: "numeric",
	}
	if name, ok := types[oid]; ok {
		return name
	}
	return "text"
}

func isNumericPG(pgType string) bool {
	switch pgType {
	case "int2", "int4", "int8", "float4", "float8", "numeric", "oid":
		return true
	}
	return false
}
