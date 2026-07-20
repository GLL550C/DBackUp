// DB Backup Tool - Frontend Application
var API_BASE = '/api';

// ========== Utility ==========
function $(sel) { return document.querySelector(sel); }

function toast(msg, type) {
    type = type || 'info';
    var container = $('#toast-container');
    var icons = { success: 'bi-check-circle-fill', error: 'bi-x-circle-fill', warning: 'bi-exclamation-triangle-fill', info: 'bi-info-circle-fill' };
    var el = document.createElement('div');
    el.className = 'toast-msg ' + type;
    el.innerHTML = '<i class="bi ' + (icons[type] || icons.info) + '"></i> ' + msg;
    container.appendChild(el);
    setTimeout(function() { el.style.opacity = '0'; el.style.transition = 'opacity .3s'; setTimeout(function() { el.remove(); }, 300); }, 3500);
}

function api(method, path, body) {
    var opts = { method: method, headers: { 'Content-Type': 'application/json' } };
    if (body) opts.body = JSON.stringify(body);
    return fetch(API_BASE + path, opts).then(function(res) {
        return res.json().then(function(data) {
            if (!res.ok) throw new Error(data.error || '请求失败');
            return data;
        });
    });
}

function markActivePage() {
    var path = window.location.pathname;
    document.querySelectorAll('.nav-item').forEach(function(el) { el.classList.toggle('active', el.dataset.page === path); });
}

function doLogout() {
    api('POST', '/logout').then(function() {
        window.location.href = '/login';
    }).catch(function() {
        window.location.href = '/login';
    });
}

function esc(s) { if (!s) return ''; var d = document.createElement('div'); d.textContent = s; return d.innerHTML; }
function formatSize(bytes) { if (!bytes || bytes === 0) return '0 B'; if (bytes < 1024) return bytes + ' B'; if (bytes < 1048576) return (bytes / 1024).toFixed(1) + ' KB'; if (bytes < 1073741824) return (bytes / 1048576).toFixed(1) + ' MB'; return (bytes / 1073741824).toFixed(2) + ' GB'; }
function formatDuration(sec) { if (!sec || sec === 0) return '-'; if (sec < 60) return sec + '秒'; if (sec < 3600) return Math.floor(sec / 60) + '分' + (sec % 60) + '秒'; return Math.floor(sec / 3600) + '小时' + Math.floor((sec % 3600) / 60) + '分'; }
function statusBadge(status) { var m = { running: 'badge-info', success: 'badge-success', failed: 'badge-danger' }; var l = { running: '运行中', success: '成功', failed: '失败' }; return '<span class="badge ' + (m[status] || 'badge-muted') + '">' + (l[status] || status) + '</span>'; }

// ========== Dashboard ==========
function loadDashboard() {
    api('GET', '/dashboard/stats').then(function(s) {
        var e = function(id) { var x = $('#' + id); return x || { textContent: '' }; };
        e('stat-total-tasks').textContent = s.total_tasks;
        e('stat-success').textContent = s.success_backups;
        e('stat-failed').textContent = s.failed_backups;
        e('stat-rate').textContent = s.success_rate.toFixed(1) + '%';
        e('stat-last-backup').textContent = s.last_backup_time || '-';
        var tb = $('#recent-table'), em = $('#recent-empty');
        if (!tb) return; tb.innerHTML = '';
        if (s.recent_records && s.recent_records.length > 0) {
            if (em) em.style.display = 'none';
            s.recent_records.forEach(function(r) {
                tb.innerHTML += '<tr><td>' + esc(r.task_name) + '</td><td><span class="badge badge-muted">' + esc(r.db_type) + '</span></td><td><small>' + esc(r.file_name) + '</small></td><td>' + formatSize(r.file_size) + '</td><td>' + statusBadge(r.status) + '</td><td><small>' + esc(r.started_at) + '</small></td><td>' + formatDuration(r.duration) + '</td></tr>';
            });
        } else { if (em) em.style.display = ''; }
    }).catch(function(e) { toast('加载仪表盘失败：' + e.message, 'error'); });
}

// ========== Connections ==========
var connBsModal;
function showConnModal() {
    $('#connModalTitle').textContent = '添加连接';
    $('#conn-id').value = '';
    $('#conn-name').value = ''; $('#conn-type').value = '';
    $('#conn-host').value = ''; $('#conn-port').value = '3306';
    $('#conn-user').value = ''; $('#conn-pass').value = '';
    if (!connBsModal) connBsModal = new bootstrap.Modal('#connModal'); connBsModal.show();
}
function editConn(id) {
    api('GET', '/connections').then(function(cs) {
        var c = cs.find(function(x) { return x.id === id; }); if (!c) return;
        $('#connModalTitle').textContent = '编辑连接';
        $('#conn-id').value = c.id; $('#conn-name').value = c.name;
        $('#conn-type').value = c.db_type; $('#conn-host').value = c.host;
        $('#conn-port').value = c.port; $('#conn-user').value = c.username;
        $('#conn-pass').value = c.password || '';
        if (!connBsModal) connBsModal = new bootstrap.Modal('#connModal'); connBsModal.show();
    }).catch(function(e) { toast(e.message, 'error'); });
}
function deleteConn(id) { if (!confirm('确定要删除此连接吗？')) return; api('DELETE', '/connections/' + id).then(function() { toast('连接已删除', 'success'); loadConnections(); }).catch(function(e) { toast(e.message, 'error'); }); }
function toggleSelectAllConn() {
    var checked = $('#select-all-conn').checked;
    document.querySelectorAll('.conn-check').forEach(function(cb) { cb.checked = checked; });
    updateConnBatchBtn();
}
function updateConnBatchBtn() {
    var btn = $('#btn-batch-del-conn'); if (!btn) return;
    var count = document.querySelectorAll('.conn-check:checked').length;
    btn.style.display = count > 0 ? '' : 'none';
    btn.textContent = '删除选中 (' + count + ')';
}
function batchDeleteConns() {
    var checked = document.querySelectorAll('.conn-check:checked');
    if (checked.length === 0) { toast('请先选择要删除的连接', 'warning'); return; }
    if (!confirm('确定要删除选中的 ' + checked.length + ' 个连接吗？关联的备份任务也会被删除。')) return;
    var ids = []; checked.forEach(function(cb) { ids.push(parseInt(cb.value)); });
    api('POST', '/connections/batch-delete', { ids: ids }).then(function(r) { toast(r.message, 'success'); loadConnections(); }).catch(function(e) { toast(e.message, 'error'); });
}
function testConn(id) {
    var btn = $('#btn-test-conn');
    if (btn) { btn.disabled = true; btn.innerHTML = '<span class="spinner-border spinner-border-sm"></span> 测试中...'; }
    toast('正在测试连接...', 'info');
    api('POST', '/connections/' + id + '/test').then(function(r) {
        var b = $('#test-result-body');
        if (b) {
            b.innerHTML = r.success ? '<i class="bi bi-check-circle-fill" style="font-size:48px;color:#22c55e;"></i><p style="margin-top:12px;">' + r.message + '</p>' : '<i class="bi bi-x-circle-fill" style="font-size:48px;color:#ef4444;"></i><p style="margin-top:12px;">' + r.message + '</p>';
        } else {
            toast(r.success ? '连接成功: ' + r.message : '连接失败: ' + r.message, r.success ? 'success' : 'error');
        }
        new bootstrap.Modal('#testResultModal').show();
    }).catch(function(e) { toast(e.message, 'error'); }).finally(function() {
        if (btn) { btn.disabled = false; btn.innerHTML = '<i class="bi bi-lightning-charge"></i> 测试连接'; }
    });
}
function loadConnections() {
    api('GET', '/connections').then(function(cs) {
        var tb = $('#connections-table'), em = $('#connections-empty');
        if (!tb) return; tb.innerHTML = '';
        if (!cs || cs.length === 0) { if (em) em.style.display = ''; }
        else { if (em) em.style.display = 'none';
            cs.forEach(function(c) {
                var bc = c.db_type === 'mysql' ? 'badge-warning' : 'badge-info';
                tb.innerHTML += '<tr><td><input type="checkbox" class="conn-check" value="' + c.id + '" onchange="updateConnBatchBtn()"></td><td><strong>' + esc(c.name) + '</strong></td><td><span class="badge ' + bc + '">' + esc(c.db_type) + '</span></td><td>' + esc(c.host) + '</td><td>' + c.port + '</td><td><small>' + esc(c.created_at) + '</small></td><td><button class="btn btn-sm btn-outline" onclick="testConn(' + c.id + ')" title="测试"><i class="bi bi-lightning-charge"></i></button> <button class="btn btn-sm btn-outline" onclick="editConn(' + c.id + ')" title="编辑"><i class="bi bi-pencil"></i></button> <button class="btn btn-sm btn-danger" onclick="deleteConn(' + c.id + ')" title="删除"><i class="bi bi-trash"></i></button></td></tr>';
            });
        }
        updateConnBatchBtn();
        var sa = $('#select-all-conn'); if (sa) sa.checked = false;
    }).catch(function(e) { toast('加载连接列表失败：' + e.message, 'error'); });
}
// Conn save
(function() {
    document.addEventListener('DOMContentLoaded', function() {
        var sel = $('#conn-type'); if (sel) sel.addEventListener('change', function() { $('#conn-port').value = this.value === 'postgresql' ? '5432' : '3306'; });
        var testBtn = $('#btn-test-conn');
        if (testBtn) testBtn.addEventListener('click', function() {
            var body = { db_type: $('#conn-type').value, host: ($('#conn-host').value || '').trim(), port: parseInt($('#conn-port').value) || 3306, username: ($('#conn-user').value || '').trim(), password: $('#conn-pass').value };
            if (!body.db_type || !body.host || !body.username) { toast('请先填写数据库类型、主机和用户名', 'warning'); return; }
            testBtn.disabled = true; testBtn.innerHTML = '<span class="spinner-border spinner-border-sm"></span> 测试中...';
            api('POST', '/connections/test', body).then(function(r) {
                var b = $('#test-result-body');
                if (b) {
                    b.innerHTML = r.success ? '<i class="bi bi-check-circle-fill" style="font-size:48px;color:#22c55e;"></i><p style="margin-top:12px;">' + r.message + '</p>' : '<i class="bi bi-x-circle-fill" style="font-size:48px;color:#ef4444;"></i><p style="margin-top:12px;">' + r.message + '</p>';
                }
                new bootstrap.Modal('#testResultModal').show();
            }).catch(function(e) { toast(e.message, 'error'); }).finally(function() {
                testBtn.disabled = false; testBtn.innerHTML = '<i class="bi bi-lightning-charge"></i> 测试连接';
            });
        });

        var btn = $('#btn-save-conn');
        if (btn) btn.addEventListener('click', function() {
            var id = $('#conn-id').value;
            var body = { name: ($('#conn-name').value || '').trim(), db_type: $('#conn-type').value, host: ($('#conn-host').value || '').trim(), port: parseInt($('#conn-port').value) || 3306, username: ($('#conn-user').value || '').trim(), password: $('#conn-pass').value };
            if (!body.name || !body.db_type || !body.host || !body.username) { toast('请填写所有必填字段', 'warning'); return; }
            var req = id ? api('PUT', '/connections/' + id, body) : api('POST', '/connections', body);
            req.then(function() { toast(id ? '连接已更新' : '连接已创建', 'success'); connBsModal.hide(); loadConnections(); }).catch(function(e) { toast(e.message, 'error'); });
        });
    });
})();

// ========== Tasks ==========
var taskBsModal, loadedDatabases = [];

function onTaskConnChange() {
    var connId = $('#task-conn').value;
    if (!connId) return;
    var modeAll = document.querySelector('input[name="db-mode"][value="all"]');
    if (modeAll) modeAll.checked = true;
    toggleDBMode();
    loadDatabasesForConn(connId);
    renderParamFields();
}

function toggleDBMode() {
    var isSelect = document.querySelector('input[name="db-mode"]:checked').value === 'select';
    var area = $('#db-select-area');
    if (area) area.style.display = isSelect ? '' : 'none';
}

function loadDatabasesForConn(connId) {
    var loading = $('#db-loading'), error = $('#db-error'), cbs = $('#db-checkboxes');
    if (loading) loading.style.display = '';
    if (error) error.style.display = 'none';
    if (cbs) cbs.innerHTML = '';
    api('GET', '/connections/' + connId + '/databases').then(function(data) {
        if (loading) loading.style.display = 'none';
        if (data.error) { if (error) { error.textContent = '加载失败：' + data.error; error.style.display = ''; } return; }
        loadedDatabases = data.databases || [];
        if (cbs && loadedDatabases.length > 0) {
            loadedDatabases.forEach(function(db) {
                cbs.innerHTML += '<label style="display:block;font-size:13px;padding:2px 0;"><input type="checkbox" value="' + esc(db) + '" checked> ' + esc(db) + '</label>';
            });
        } else if (cbs) {
            cbs.innerHTML = '<span style="color:#94a3b8;">没有找到数据库或连接失败</span>';
        }
    }).catch(function(e) {
        if (loading) loading.style.display = 'none';
        if (error) { error.textContent = '加载失败：' + e.message; error.style.display = ''; }
    });
}

function getSelectedDatabases() {
    var mode = document.querySelector('input[name="db-mode"]:checked');
    if (!mode || mode.value === 'all') return ['*'];
    var cbs = document.querySelectorAll('#db-checkboxes input[type="checkbox"]:checked');
    var dbs = [];
    cbs.forEach(function(cb) { dbs.push(cb.value); });
    return dbs.length > 0 ? dbs : ['*'];
}

// Backup parameter definitions
var MYSQL_PARAMS = [
    { key: 'no_data', label: '仅结构 (--no-data)', type: 'bool' },
    { key: 'no_create_info', label: '仅数据 (--no-create-info)', type: 'bool' },
    { key: 'skip_lock_tables', label: '跳过锁表 (--skip-lock-tables)', type: 'bool' },
    { key: 'single_transaction', label: '一致性快照 (--single-transaction)', type: 'bool' },
    { key: 'routines', label: '导出存储过程 (--routines)', type: 'bool' },
    { key: 'triggers', label: '导出触发器 (--triggers)', type: 'bool' },
    { key: 'add_drop_table', label: '添加 DROP TABLE', type: 'bool' },
    { key: 'add_drop_database', label: '添加 DROP DATABASE', type: 'bool' },
    { key: 'hex_blob', label: '二进制用十六进制 (--hex-blob)', type: 'bool' },
    { key: 'complete_insert', label: '完整INSERT含列名', type: 'bool' },
    { key: 'extended_insert', label: '批量INSERT优化', type: 'bool' },
];
var PG_PARAMS = [
    { key: 'schema_only', label: '仅结构 (--schema-only)', type: 'bool' },
    { key: 'data_only', label: '仅数据 (--data-only)', type: 'bool' },
    { key: 'clean', label: '添加DROP语句 (--clean)', type: 'bool' },
    { key: 'if_exists', label: '使用IF EXISTS', type: 'bool' },
    { key: 'create', label: '包含CREATE DATABASE', type: 'bool' },
    { key: 'no_owner', label: '不导出所有者 (--no-owner)', type: 'bool' },
    { key: 'no_comments', label: '不导出注释', type: 'bool' },
    { key: 'no_tablespaces', label: '不导出表空间', type: 'bool' },
    { key: 'rows_per_insert', label: '批量插入行数', type: 'text' },
];

function renderParamFields() {
    var area = $('#backup-params-area');
    if (!area) return;
    var connId = $('#task-conn').value;
    if (!connId) { area.innerHTML = '<small style="color:#64748b;">选择数据库连接后可配置参数</small>'; return; }

    // Get connection type
    api('GET', '/connections/' + connId).then(function(c) {
        var params = c.db_type === 'mysql' ? MYSQL_PARAMS : PG_PARAMS;
        var html = '<div class="form-row" style="flex-wrap:wrap;">';
        params.forEach(function(p) {
            if (p.type === 'bool') {
                html += '<div class="form-group" style="min-width:200px;"><label style="font-size:12px;"><input type="checkbox" class="param-check" data-key="' + p.key + '"> ' + p.label + '</label></div>';
            } else {
                html += '<div class="form-group" style="min-width:200px;"><label style="font-size:12px;">' + p.label + '</label><input type="text" class="form-control form-control-sm param-text" data-key="' + p.key + '" style="width:100px;"></div>';
            }
        });
        html += '</div>';
        area.innerHTML = html;
    }).catch(function() { area.innerHTML = '<small style="color:#64748b;">无法加载参数配置</small>'; });
}

function getBackupParams() {
    var params = {};
    document.querySelectorAll('.param-check:checked').forEach(function(cb) { params[cb.dataset.key] = true; });
    document.querySelectorAll('.param-text').forEach(function(t) { if (t.value.trim()) params[t.dataset.key] = t.value.trim(); });
    return params;
}

function setParamValues(paramsObj) {
    if (!paramsObj) return;
    Object.keys(paramsObj).forEach(function(k) {
        var cb = document.querySelector('.param-check[data-key="' + k + '"]');
        if (cb) cb.checked = !!paramsObj[k];
        var tx = document.querySelector('.param-text[data-key="' + k + '"]');
        if (tx && typeof paramsObj[k] === 'string') tx.value = paramsObj[k];
    });
}

function showTaskModal() {
    $('#taskModalTitle').textContent = '新建备份任务';
    $('#task-id').value = ''; $('#task-name').value = '';
    $('#task-storage-type').value = 'local';
    $('#task-local-path').value = ''; $('#task-remote-host').value = ''; $('#task-remote-port').value = '22';
    $('#task-remote-user').value = ''; $('#task-remote-pass').value = ''; $('#task-remote-key').value = ''; $('#task-remote-path').value = '';
    $('#task-cron').value = ''; $('#task-max').value = '10'; $('#task-enabled').checked = true;
    document.querySelector('input[name="db-mode"][value="all"]').checked = true;
    toggleStorageFields(); toggleDBMode(); loadedDatabases = [];
    $('#db-checkboxes').innerHTML = ''; $('#backup-params-area').innerHTML = '<small style="color:#64748b;">选择数据库连接后可配置参数</small>';

    api('GET', '/connections').then(function(cs) {
        var sel = $('#task-conn'); sel.innerHTML = cs.map(function(c) { return '<option value="' + c.id + '">' + esc(c.name) + ' (' + esc(c.db_type) + ')</option>'; }).join('');
        if (cs.length === 1) { sel.value = cs[0].id; onTaskConnChange(); }
    }).catch(function() {});

    if (!taskBsModal) taskBsModal = new bootstrap.Modal('#taskModal'); taskBsModal.show();
}

function editTask(id) {
    api('GET', '/tasks').then(function(ts) {
        var t = ts.find(function(x) { return x.id === id; }); if (!t) return;
        $('#taskModalTitle').textContent = '编辑备份任务';
        $('#task-id').value = t.id; $('#task-name').value = t.name;
        $('#task-storage-type').value = t.storage_type;
        $('#task-local-path').value = t.local_path || '';
        $('#task-remote-host').value = t.remote_host || ''; $('#task-remote-port').value = t.remote_port || 22;
        $('#task-remote-user').value = t.remote_user || ''; $('#task-remote-pass').value = t.remote_pass || '';
        $('#task-remote-key').value = t.remote_key || '';
        $('#task-remote-path').value = t.remote_path || '';
        $('#task-cron').value = t.cron_expr || ''; $('#task-max').value = t.max_backups || 10;
        $('#task-enabled').checked = t.enabled;

        // Databases
        var dbs = t.databases ? JSON.parse(t.databases) : ['*'];
        if (dbs.length === 1 && dbs[0] === '*') {
            document.querySelector('input[name="db-mode"][value="all"]').checked = true;
        } else {
            document.querySelector('input[name="db-mode"][value="select"]').checked = true;
        }
        toggleStorageFields(); toggleDBMode();

        api('GET', '/connections').then(function(cs) {
            var sel = $('#task-conn');
            sel.innerHTML = cs.map(function(c) { return '<option value="' + c.id + '"' + (c.id === t.connection_id ? ' selected' : '') + '>' + esc(c.name) + ' (' + esc(c.db_type) + ')</option>'; }).join('');
            if (t.connection_id) loadDatabasesForTaskEdit(t.connection_id, dbs);
            renderParamFields();
            // Set param checkboxes after render
            setTimeout(function() {
                var pp = t.backup_params ? JSON.parse(t.backup_params) : {};
                setParamValues(pp);
            }, 200);
        }).catch(function() {});

        if (!taskBsModal) taskBsModal = new bootstrap.Modal('#taskModal'); taskBsModal.show();
    }).catch(function(e) { toast(e.message, 'error'); });
}

function loadDatabasesForTaskEdit(connId, selectedDBs) {
    api('GET', '/connections/' + connId + '/databases').then(function(data) {
        loadedDatabases = data.databases || [];
        var cbs = $('#db-checkboxes');
        if (!cbs) return;
        cbs.innerHTML = '';
        var selSet = {};
        selectedDBs.forEach(function(d) { selSet[d] = true; });
        var isAll = selectedDBs.length === 1 && selectedDBs[0] === '*';
        loadedDatabases.forEach(function(db) {
            var checked = isAll || selSet[db] ? ' checked' : '';
            cbs.innerHTML += '<label style="display:block;font-size:13px;padding:2px 0;"><input type="checkbox" value="' + esc(db) + '"' + checked + '> ' + esc(db) + '</label>';
        });
    }).catch(function() {});
}

function toggleStorageFields() {
    var mode = $('#task-storage-type').value;
    var l = $('#local-path-group'), r = $('#remote-fields');
    if (l) l.style.display = (mode === 'remote') ? 'none' : '';
    if (r) r.style.display = (mode === 'local') ? 'none' : '';
}
function setCronPreset(expr) { $('#task-cron').value = expr; }
function runBackup(id) { api('POST', '/tasks/' + id + '/run').then(function(r) { toast('备份任务已启动', 'info'); }).catch(function(e) { toast(e.message, 'error'); }); }
function deleteTask(id) { if (!confirm('确定要删除此备份任务吗？')) return; api('DELETE', '/tasks/' + id).then(function() { toast('任务已删除', 'success'); loadTasks(); }).catch(function(e) { toast(e.message, 'error'); }); }
function toggleSelectAllTask() {
    var checked = $('#select-all-task').checked;
    document.querySelectorAll('.task-check').forEach(function(cb) { cb.checked = checked; });
    updateTaskBatchBtn();
}
function updateTaskBatchBtn() {
    var btn = $('#btn-batch-del-task'); if (!btn) return;
    var count = document.querySelectorAll('.task-check:checked').length;
    btn.style.display = count > 0 ? '' : 'none';
    btn.textContent = '删除选中 (' + count + ')';
}
function batchDeleteTasks() {
    var checked = document.querySelectorAll('.task-check:checked');
    if (checked.length === 0) { toast('请先选择要删除的任务', 'warning'); return; }
    if (!confirm('确定要删除选中的 ' + checked.length + ' 个备份任务吗？')) return;
    var ids = []; checked.forEach(function(cb) { ids.push(parseInt(cb.value)); });
    api('POST', '/tasks/batch-delete', { ids: ids }).then(function(r) { toast(r.message, 'success'); loadTasks(); }).catch(function(e) { toast(e.message, 'error'); });
}

function loadTasks() {
    api('GET', '/tasks').then(function(ts) {
        var tb = $('#tasks-table'), em = $('#tasks-empty');
        if (!tb) return; tb.innerHTML = '';
        if (!ts || ts.length === 0) { if (em) em.style.display = ''; }
        else { if (em) em.style.display = 'none';
            ts.forEach(function(t) {
                var sched = t.cron_expr || '<span style="color:#94a3b8;">手动触发</span>';
                var ret = t.max_backups > 0 ? '保留 ' + t.max_backups + ' 份' : '不限制';
                var st = t.enabled ? '<span class="badge badge-success">已启用</span>' : '<span class="badge badge-muted">已禁用</span>';
                var modeLabel = {'local':'本地备份', 'remote':'远程备份SFTP', 'both':'本地备份+远程备份SFTP'};
                var mode = modeLabel[t.storage_type] || t.storage_type;
                tb.innerHTML += '<tr><td><input type="checkbox" class="task-check" value="' + t.id + '" onchange="updateTaskBatchBtn()"></td><td><strong>' + esc(t.name) + '</strong></td><td>' + mode + '</td><td>' + sched + '</td><td>' + ret + '</td><td>' + st + '</td><td><button class="btn btn-sm btn-success" onclick="runBackup(' + t.id + ')" title="立即执行"><i class="bi bi-play-fill"></i></button> <button class="btn btn-sm btn-outline" onclick="editTask(' + t.id + ')" title="编辑"><i class="bi bi-pencil"></i></button> <button class="btn btn-sm btn-danger" onclick="deleteTask(' + t.id + ')" title="删除"><i class="bi bi-trash"></i></button></td></tr>';
            });
        }
        updateTaskBatchBtn();
        var sa = $('#select-all-task'); if (sa) sa.checked = false;
    }).catch(function(e) { toast('加载任务列表失败：' + e.message, 'error'); });
}

// Save task
(function() {
    document.addEventListener('DOMContentLoaded', function() {
        var btn = $('#btn-save-task');
        if (btn) btn.addEventListener('click', function() {
            var id = $('#task-id').value;
            var body = {
                name: ($('#task-name').value || '').trim(),
                connection_id: parseInt($('#task-conn').value) || 0,
                databases: getSelectedDatabases(),
                backup_params: getBackupParams(),
                storage_type: $('#task-storage-type').value,
                local_path: ($('#task-local-path').value || '').trim(),
                remote_host: ($('#task-remote-host').value || '').trim(),
                remote_port: parseInt($('#task-remote-port').value) || 22,
                remote_user: ($('#task-remote-user').value || '').trim(),
                remote_pass: $('#task-remote-pass').value,
                remote_key: ($('#task-remote-key').value || '').trim(),
                remote_path: ($('#task-remote-path').value || '').trim(),
                max_backups: parseInt($('#task-max').value) || 10,
                retention_days: 0,
                cron_expr: ($('#task-cron').value || '').trim(),
                enabled: $('#task-enabled').checked
            };
            if (!body.name || !body.connection_id) { toast('请填写任务名称并选择数据库连接', 'warning'); return; }
            var req = id ? api('PUT', '/tasks/' + id, body) : api('POST', '/tasks', body);
            req.then(function() { toast(id ? '任务已更新' : '任务已创建', 'success'); taskBsModal.hide(); loadTasks(); }).catch(function(e) { toast(e.message, 'error'); });
        });
    });
})();

// ========== Backup History ==========
var recordsPage = 1;
function loadRecords() {
    var tid = $('#filter-task') ? $('#filter-task').value : 0;
    api('GET', '/records?task_id=' + tid + '&page=' + recordsPage + '&per_page=20').then(function(d) {
        var tb = $('#records-table'), em = $('#records-empty');
        if (!tb) return; tb.innerHTML = '';
        if (!d.records || d.records.length === 0) { if (em) em.style.display = ''; }
        else { if (em) em.style.display = 'none';
            d.records.forEach(function(r) {
                var dl = r.status === 'success' ? '<a href="' + API_BASE + '/records/' + r.id + '/download" class="btn btn-sm btn-outline" title="下载"><i class="bi bi-download"></i></a> ' : '';
                tb.innerHTML += '<tr><td><input type="checkbox" class="record-check" value="' + r.id + '" onchange="updateBatchBtn()"></td><td>' + esc(r.task_name) + '</td><td><span class="badge badge-muted">' + esc(r.db_type) + '</span></td><td><small>' + esc(r.file_name) + '</small></td><td>' + formatSize(r.file_size) + '</td><td>' + statusBadge(r.status) + '</td><td><small>' + esc(r.started_at) + '</small></td><td>' + formatDuration(r.duration) + '</td><td>' + dl + '<button class="btn btn-sm btn-danger" onclick="deleteRecord(' + r.id + ')" title="删除"><i class="bi bi-trash"></i></button></td></tr>';
            });
        }
        updateBatchBtn();
        var selAll = $('#select-all'); if (selAll) selAll.checked = false;
        var pg = $('#records-pagination');
        if (pg) { var tp = Math.ceil(d.total / 20); if (tp > 1) { var h = ''; for (var i = 1; i <= tp; i++) h += '<a href="#"' + (i === recordsPage ? ' class="active"' : '') + ' onclick="recordsPage=' + i + ';loadRecords();return false;">' + i + '</a>'; pg.innerHTML = h; } else pg.innerHTML = ''; }
    }).catch(function(e) { toast('加载备份历史失败：' + e.message, 'error'); });
}
function toggleSelectAll() {
    var checked = $('#select-all').checked;
    document.querySelectorAll('.record-check').forEach(function(cb) { cb.checked = checked; });
    updateBatchBtn();
}
function updateBatchBtn() {
    var btn = $('#btn-batch-delete');
    if (!btn) return;
    var count = document.querySelectorAll('.record-check:checked').length;
    btn.style.display = count > 0 ? '' : 'none';
    btn.textContent = '删除选中 (' + count + ')';
}
function batchDeleteRecords() {
    var checked = document.querySelectorAll('.record-check:checked');
    if (checked.length === 0) { toast('请先选择要删除的记录', 'warning'); return; }
    if (!confirm('确定要删除选中的 ' + checked.length + ' 条备份记录及其文件吗？')) return;
    var ids = [];
    checked.forEach(function(cb) { ids.push(parseInt(cb.value)); });
    api('POST', '/records/batch-delete', { ids: ids }).then(function(r) {
        toast(r.message, 'success'); loadRecords();
    }).catch(function(e) { toast(e.message, 'error'); });
}
function deleteRecord(id) { if (!confirm('确定要删除此备份记录及其文件吗？')) return; api('DELETE', '/records/' + id).then(function() { toast('备份记录已删除', 'success'); loadRecords(); }).catch(function(e) { toast(e.message, 'error'); }); }
function loadRecordFilters() { api('GET', '/tasks').then(function(ts) { var f = $('#filter-task'); if (f && ts) ts.forEach(function(t) { var o = document.createElement('option'); o.value = t.id; o.textContent = t.name; f.appendChild(o); }); }).catch(function() {}); }

// ========== Settings ==========
(function() {
    document.addEventListener('DOMContentLoaded', function() {
        var sb = $('#btn-save-settings');
        if (sb) sb.addEventListener('click', function() { api('PUT', '/settings', { server_port: parseInt($('#setting-port').value) || 8080, default_backup_dir: ($('#setting-backup-dir').value || '').trim(), log_retention_days: parseInt($('#setting-log-retention').value) || 30 }).then(function() { toast('设置已保存', 'success'); }).catch(function(e) { toast(e.message, 'error'); }); });
    });
    if (window.location.pathname === '/settings') { api('GET', '/settings').then(function(s) { var p = $('#setting-port'); if (p) p.value = s.server_port; var d = $('#setting-backup-dir'); if (d) d.value = s.default_backup_dir; var l = $('#setting-log-retention'); if (l) l.value = s.log_retention_days; }).catch(function() {}); }
})();

// ========== Init ==========
document.addEventListener('DOMContentLoaded', function() {
    markActivePage();
    var p = window.location.pathname;
    if (p === '/' || p === '') loadDashboard();
    if (p === '/connections') loadConnections();
    if (p === '/tasks') loadTasks();
    if (p === '/history') { loadRecordFilters(); loadRecords(); }
});
