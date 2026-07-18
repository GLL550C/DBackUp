#!/usr/bin/env bash
# ============================================================
# DB Backup Tool - 数据库初始化脚本
# 用法: ./scripts/init_db.sh
# 功能: 执行 db/init.sql 创建 main.db，并校验表结构完整性
# 依赖: sqlite3 CLI 或 python3（内置 sqlite3 模块）
# ============================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

INIT_SQL="$PROJECT_DIR/db/init.sql"
DATA_DIR="$PROJECT_DIR/data"
DB_FILE="$DATA_DIR/main.db"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

PASS=0
FAIL=0

ok()   { echo -e "  ${GREEN}✓${NC} $1"; PASS=$((PASS + 1)); }
err()  { echo -e "  ${RED}✗${NC} $1"; FAIL=$((FAIL + 1)); }

echo "=========================================="
echo " DB Backup Tool - 数据库初始化"
echo "=========================================="
echo ""

# ---- 选 sqlite 执行器 ----
SQLITE=""
if command -v sqlite3 &>/dev/null; then
    SQLITE="sqlite3"
    ok "使用 sqlite3: $(sqlite3 --version 2>&1 | head -1)"
elif command -v python3 &>/dev/null; then
    SQLITE="python3"
    ok "使用 python3 内置 sqlite3 模块"
elif command -v python &>/dev/null; then
    SQLITE="python"
    ok "使用 python 内置 sqlite3 模块"
else
    err "未找到 sqlite3 或 python3，请安装其一"
    exit 1
fi

if [ ! -f "$INIT_SQL" ]; then
    err "init.sql 不存在: $INIT_SQL"
    exit 1
fi
ok "init.sql 存在: $INIT_SQL"

# ---- 创建目录 ----
mkdir -p "$DATA_DIR"
ok "数据目录: $DATA_DIR"

# ---- 备份旧数据库 ----
if [ -f "$DB_FILE" ]; then
    BACKUP="${DB_FILE}.bak.$(date +%Y%m%d_%H%M%S)"
    cp "$DB_FILE" "$BACKUP"
    echo -e "  ${YELLOW}⚠${NC} 已备份旧数据库到: $(basename "$BACKUP")"
    rm -f "$DB_FILE"
fi

# ---- 执行 init.sql ----
echo ""
echo "--- 2. 执行 init.sql ---"

run_sql_file() {
    local db="$1"
    local sql="$2"
    case "$SQLITE" in
        sqlite3)
            sqlite3 "$db" < "$sql" 2>&1
            ;;
        python3|python)
            $SQLITE -c "
import sqlite3, sys
try:
    conn = sqlite3.connect('$db')
    conn.executescript(open('$sql').read())
    conn.commit()
    conn.close()
except Exception as e:
    print(f'SQL ERROR: {e}', file=sys.stderr)
    sys.exit(1)
" 2>&1
            ;;
    esac
}

run_sql_query() {
    local db="$1"
    local query="$2"
    case "$SQLITE" in
        sqlite3)
            sqlite3 "$db" "$query" 2>&1
            ;;
        python3|python)
            $SQLITE -c "
import sqlite3
conn = sqlite3.connect('$db')
cur = conn.cursor()
cur.execute('''$query''')
for row in cur.fetchall():
    print(row[0])
conn.close()
" 2>&1
            ;;
    esac
}

if run_sql_file "$DB_FILE" "$INIT_SQL"; then
    ok "init.sql 执行成功"
else
    err "init.sql 执行失败"
    exit 1
fi

# ---- 校验表结构 ----
echo ""
echo "--- 3. 校验表结构 ---"

EXPECTED_TABLES=(
    "schema_version"
    "connections"
    "backup_tasks"
    "backup_records"
    "app_settings"
)

# 获取所有表名
case "$SQLITE" in
    sqlite3)
        ACTUAL_TABLES=$(sqlite3 "$DB_FILE" ".tables" 2>&1)
        ;;
    python3|python)
        ACTUAL_TABLES=$($SQLITE -c "
import sqlite3
conn = sqlite3.connect('$DB_FILE')
cur = conn.cursor()
cur.execute(\"SELECT name FROM sqlite_master WHERE type='table' ORDER BY name\")
tables = [row[0] for row in cur.fetchall()]
print(' '.join(tables))
conn.close()
" 2>&1)
        ;;
esac
echo "  实际表: $ACTUAL_TABLES"

for tbl in "${EXPECTED_TABLES[@]}"; do
    if echo "$ACTUAL_TABLES" | grep -qw "$tbl"; then
        ok "表 '$tbl' 已创建"
    else
        err "表 '$tbl' 缺失"
    fi
done

# ---- 校验列数 ----
echo ""
echo "--- 4. 校验列数 ---"

check_columns() {
    local table="$1"
    local expected="$2"
    local count
    case "$SQLITE" in
        sqlite3)
            count=$(sqlite3 "$DB_FILE" "PRAGMA table_info($table);" 2>&1 | wc -l)
            ;;
        python3|python)
            count=$($SQLITE -c "
import sqlite3
conn = sqlite3.connect('$DB_FILE')
cur = conn.cursor()
cur.execute('PRAGMA table_info($table)')
print(len(cur.fetchall()))
conn.close()
" 2>&1)
            ;;
    esac
    if [ "$count" -eq "$expected" ]; then
        ok "表 '$table': $expected 列"
    else
        err "表 '$table': 期望 $expected 列，实际 $count 列"
    fi
}

check_columns "connections"     9
check_columns "backup_tasks"   19
check_columns "backup_records" 13
check_columns "app_settings"    4
check_columns "schema_version"  2

# ---- 校验初始数据 ----
echo ""
echo "--- 5. 校验初始数据 ---"

SETTINGS_COUNT=$(run_sql_query "$DB_FILE" "SELECT COUNT(*) FROM app_settings")
if [ "$SETTINGS_COUNT" -ge 1 ]; then
    ok "app_settings 初始数据: $SETTINGS_COUNT 行"
else
    err "app_settings 初始数据缺失"
fi

VERSION_COUNT=$(run_sql_query "$DB_FILE" "SELECT COUNT(*) FROM schema_version")
if [ "$VERSION_COUNT" -ge 1 ]; then
    ok "schema_version 迁移记录: $VERSION_COUNT 行"
else
    err "schema_version 迁移记录缺失"
fi

# ---- 校验外键 ----
echo ""
echo "--- 6. 校验外键 ---"

check_fk() {
    local table="$1"
    local count
    case "$SQLITE" in
        sqlite3)
            count=$(sqlite3 "$DB_FILE" "PRAGMA foreign_key_list($table);" 2>&1 | wc -l)
            ;;
        python3|python)
            count=$($SQLITE -c "
import sqlite3
conn = sqlite3.connect('$DB_FILE')
cur = conn.cursor()
cur.execute('PRAGMA foreign_key_list($table)')
print(len(cur.fetchall()))
conn.close()
" 2>&1)
            ;;
    esac
    if [ "$count" -ge 1 ]; then
        ok "$table 外键约束: $count 个"
    else
        err "$table 外键约束缺失"
    fi
}

check_fk "backup_tasks"
check_fk "backup_records"

# ---- 结果 ----
echo ""
echo "=========================================="
printf " 结果: ${GREEN}%d 通过${NC}" "$PASS"
if [ "$FAIL" -gt 0 ]; then
    printf ", ${RED}%d 失败${NC}" "$FAIL"
fi
echo ""
echo " 数据库文件: $DB_FILE"
echo "=========================================="

if [ "$FAIL" -gt 0 ]; then
    echo -e "\n${RED}初始化存在异常，请检查 db/init.sql${NC}"
    exit 1
else
    echo -e "\n${GREEN}数据库初始化完成，可以启动程序。${NC}"
    exit 0
fi
