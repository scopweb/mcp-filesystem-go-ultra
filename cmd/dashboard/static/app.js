// MCP Filesystem Ultra Dashboard

const $ = (sel) => document.querySelector(sel);
const $$ = (sel) => document.querySelectorAll(sel);

// Navigation
document.querySelector('nav').addEventListener('click', (e) => {
  if (e.target.tagName !== 'BUTTON') return;
  $$('nav button').forEach(b => b.classList.remove('active'));
  e.target.classList.add('active');
  $$('.page').forEach(p => p.classList.remove('active'));
  $(`#page-${e.target.dataset.page}`).classList.add('active');
});

// Helpers
function formatTime(ts) {
  const d = new Date(ts);
  return d.toLocaleTimeString('en-US', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

function formatDuration(ms) {
  if (ms < 1) return '<1ms';
  if (ms < 1000) return ms + 'ms';
  return (ms / 1000).toFixed(1) + 's';
}

function formatBytes(b) {
  if (!b || b === 0) return '—';
  if (b < 1024) return b + 'B';
  if (b < 1024 * 1024) return (b / 1024).toFixed(1) + 'KB';
  return (b / (1024 * 1024)).toFixed(1) + 'MB';
}

function toolBadgeClass(tool) {
  if (tool.includes('read')) return 'read';
  if (tool.includes('write')) return 'write';
  if (tool.includes('edit')) return 'edit';
  if (tool.includes('search')) return 'search';
  if (tool.includes('list')) return 'list';
  return '';
}

function shortPath(p) {
  if (!p) return '—';
  const parts = p.replace(/\\/g, '/').split('/');
  if (parts.length <= 3) return p;
  return '.../' + parts.slice(-2).join('/');
}

// Global counter for unique operation row IDs
let opRowCounter = 0;

// When true, the Operations page stops redrawing #all-ops (both polling and SSE)
// so the user can inspect an expanded detail row without it being wiped.
let operationsPaused = false;

function operationRow(op, extended) {
  const statusClass = op.status === 'ok' ? 'ok' : op.status === 'warn' ? 'warn' : 'error';
  const toolClass = toolBadgeClass(op.tool);
  const rowId = 'op-row-' + (opRowCounter++);

  let cols = `
    <td>${formatTime(op.ts)}</td>
    <td><span class="badge ${toolClass}">${op.tool}</span>${op.sub_op ? `<span class="sub-op">${op.sub_op}</span>` : ''}</td>
    <td class="path" title="${op.path || ''}">${shortPath(op.path)}</td>
    <td>${formatDuration(op.duration_ms)}</td>`;

  if (extended) {
    cols += `<td>${formatBytes(op.bytes_in)}</td>`;
    cols += `<td>${formatBytes(op.bytes_out)}</td>`;
    cols += `<td>${formatBytes(op.file_size)}</td>`;
  }

  cols += `<td><span class="badge ${statusClass}">${op.status}</span></td>`;

  if (extended) {
    cols += `<td style="color:var(--red);font-size:12px;">${op.error || ''}</td>`;
    cols += `<td><button class="btn-detail" onclick="toggleOpDetail('${rowId}', this)">View</button></td>`;
  }

  let html = `<tr>${cols}</tr>`;

  if (extended) {
    // Build detail content
    let details = [];
    if (op.sub_op) details.push(`<span class="detail-label">Sub-operation:</span> <span class="badge ${toolClass}">${op.sub_op}</span>`);
    if (op.risk) details.push(`<span class="detail-label">Risk:</span> <span class="risk-badge risk-${op.risk.toLowerCase()}">${op.risk}</span>`);
    if (op.feedback_pattern) {
      const fbClass = op.feedback_status === 'ko' ? 'error' : 'warn';
      details.push(`<span class="detail-label">Pattern:</span> <span class="badge ${fbClass}">${op.feedback_pattern}</span>`);
    }
    if (op.lines_changed) details.push(`<span class="detail-label">Lines changed:</span> ${op.lines_changed}`);
    if (op.diff_lines) details.push(`<span class="detail-label">Diff lines:</span> ${op.diff_lines}`);
    if (op.matches) details.push(`<span class="detail-label">Matches:</span> ${op.matches}`);
    if (op.cache_hit !== undefined && op.cache_hit !== null) details.push(`<span class="detail-label">Cache hit:</span> ${op.cache_hit ? 'Yes' : 'No'}`);
    if (op.backup_id) {
      let backupInfo = `Backup: ${op.backup_id}`;
      if (op.previous_backup_id) backupInfo += ` → parent ${op.previous_backup_id}`;
      details.push(`<span class="detail-label">Undo chain:</span> <span class="backup-chain" title="Parent: ${op.previous_backup_id || 'none'}">${backupInfo}</span>`);
    }
    if (op.integrity_status) {
      const invClass = op.integrity_status === 'OK' ? 'ok' : op.integrity_status === 'WARNING' ? 'warn' : 'error';
      let invLabel = `Integrity: <span class="badge ${invClass}">${op.integrity_status}</span>`;
      if (op.integrity_warn) invLabel += ` — ${escapeHtml(op.integrity_warn)}`;
      details.push(invLabel);
    }
    if (op.path) details.push(`<span class="detail-label">Full path:</span> <span class="path">${op.path}</span>`);

    // Args table
    let argsHtml = '';
    if (op.args && Object.keys(op.args).length > 0) {
      argsHtml = '<div class="op-args"><span class="detail-label">Arguments:</span><table class="args-table">';
      for (const [k, v] of Object.entries(op.args)) {
        argsHtml += `<tr><td class="args-key">${k}</td><td class="args-val">${escapeHtml(String(v))}</td></tr>`;
      }
      argsHtml += '</table></div>';
    }

    html += `<tr id="${rowId}" class="op-detail-row" style="display:none;">
      <td colspan="${extended ? 11 : 5}">
        <div class="op-detail-content">
          <div class="op-detail-meta">${details.join('<span class="detail-sep">|</span>')}</div>
          ${argsHtml}
        </div>
      </td>
    </tr>`;
  }

  return html;
}

// Metrics polling
let metricsConnected = false;

async function fetchMetrics() {
  try {
    const res = await fetch('/api/metrics?_t=' + Date.now());
    const m = await res.json();

    if (m.error) {
      $('#status').textContent = 'Waiting for data...';
      $('#status').className = 'status';
      return;
    }

    metricsConnected = true;
    $('#status').textContent = 'Connected';
    $('#status').className = 'status connected';

    $('#m-ops-sec').textContent = (m.ops_per_sec || 0).toFixed(1);
    $('#m-cache').textContent = (m.cache_hit_rate || 0).toFixed(1) + '%';
    $('#m-memory').textContent = (m.memory_mb || 0).toFixed(1) + 'MB';
    $('#m-total').textContent = (m.ops_total || 0).toLocaleString();
    $('#m-reads').textContent = (m.reads || 0).toLocaleString();
    $('#m-writes').textContent = (m.writes || 0).toLocaleString();
    $('#m-lists').textContent = (m.lists || 0).toLocaleString();
    $('#m-searches').textContent = (m.searches || 0).toLocaleString();

    // Edit analysis
    if (m.edits) {
      $('#a-total').textContent = (m.edits.total || 0).toLocaleString();
      $('#a-targeted').textContent = (m.edits.targeted || 0).toLocaleString();
      $('#a-rewrites').textContent = (m.edits.rewrites || 0).toLocaleString();
      $('#a-avgbytes').textContent = Math.round(m.edits.avg_bytes || 0).toLocaleString();

      const total = m.edits.total || 1;
      const tPct = ((m.edits.targeted || 0) / total * 100).toFixed(1);
      const rPct = ((m.edits.rewrites || 0) / total * 100).toFixed(1);
      $('#bar-targeted').style.width = tPct + '%';
      $('#bar-rewrites').style.width = rPct + '%';
      $('#pct-targeted').textContent = tPct + '%';
      $('#pct-rewrites').textContent = rPct + '%';
    }
  } catch (e) {
    $('#status').textContent = 'Disconnected';
    $('#status').className = 'status';
    metricsConnected = false;
  }
}

// Operations polling
async function fetchOperations() {
  try {
    const res = await fetch('/api/operations?limit=200&_t=' + Date.now());
    const ops = await res.json();

    // Recent ops (dashboard — last 10)
    const recentHtml = ops.slice(0, 10).map(op => operationRow(op, false)).join('');
    $('#recent-ops').innerHTML = recentHtml || '<tr><td colspan="5" class="empty">No operations yet</td></tr>';

    // All ops (operations page) — skip rewrite while paused so the user can
    // inspect an expanded detail row without it being blown away.
    if (!operationsPaused) {
      const allHtml = ops.map(op => operationRow(op, true)).join('');
      $('#all-ops').innerHTML = allHtml || '<tr><td colspan="10" class="empty">No operations yet</td></tr>';
    }
  } catch (e) {
    // silent
  }
}

// Backups — Enterprise Recovery System
const backupPage = { offset: 0, limit: 50, total: 0 };
let backupDebounceTimer = null;

async function searchBackups() {
  try {
    const q = ($('#bk-search-q') || {}).value || '';
    const operation = ($('#bk-search-op') || {}).value || '';
    const preset = ($('#bk-search-preset') || {}).value || '';
    const from = ($('#bk-date-from') || {}).value || '';
    const to = ($('#bk-date-to') || {}).value || '';

    const params = new URLSearchParams();
    if (q) params.set('q', q);
    if (operation) params.set('operation', operation);
    if (preset && preset !== 'custom') params.set('preset', preset);
    if (from) params.set('from', from);
    if (to) params.set('to', to);
    params.set('limit', backupPage.limit);
    params.set('offset', backupPage.offset);

    const res = await fetch('/api/backups/search?' + params);
    const data = await res.json();

    backupPage.total = data.total || 0;

    // Populate operation dropdown (keep current selection)
    const opSel = $('#bk-search-op');
    const curOp = opSel.value;
    const ops = data.operations || [];
    opSel.innerHTML = '<option value="">All Operations</option>' +
      ops.map(o => `<option value="${o}" ${o === curOp ? 'selected' : ''}>${o}</option>`).join('');

    // Summary cards
    const results = data.results || [];
    $('#bk-total').textContent = data.total;

    let totalSize = 0, totalFiles = 0, latestTs = null;
    results.forEach(b => {
      totalSize += b.total_size || 0;
      totalFiles += (b.files || []).length;
      const ts = new Date(b.timestamp);
      if (!latestTs || ts > latestTs) latestTs = ts;
    });
    $('#bk-size').textContent = formatBytes(totalSize);
    $('#bk-latest').textContent = latestTs ? latestTs.toLocaleString() : '—';
    $('#bk-files').textContent = totalFiles;

    // Status
    const statusEl = $('#bk-status');
    if (q || operation || preset || from || to) {
      statusEl.textContent = `Showing ${results.length} of ${data.total} backups`;
    } else {
      statusEl.textContent = data.total > 0 ? `${data.total} backups total` : '';
    }

    // Results table
    const container = $('#backups-list');
    if (results.length === 0) {
      container.innerHTML = '<div class="empty">No backups found</div>';
    } else {
      container.innerHTML = '<table><thead><tr><th>Time</th><th>ID</th><th>Operation</th><th>Files</th><th>Size</th><th></th></tr></thead><tbody>' +
        results.map(b => {
          const fileCount = (b.files || []).length;
          const size = formatBytes(b.total_size);
          const time = new Date(b.timestamp).toLocaleString();
          const opClass = b.operation === 'batch' ? 'badge batch' : '';
          return `<tr class="backup-row" data-id="${b.backup_id}">
            <td>${time}</td>
            <td class="path">${b.backup_id}</td>
            <td><span class="${opClass || ''}">${b.operation || '—'}</span></td>
            <td>${fileCount}</td>
            <td>${size}</td>
            <td><button class="btn-detail" onclick="toggleBackupDetail('${b.backup_id}', this)">Details</button></td>
          </tr>`;
        }).join('') +
        '</tbody></table>';
    }

    // Pagination
    renderBackupPagination();
  } catch (e) {
    // silent
  }
}

function renderBackupPagination() {
  const el = $('#bk-pagination');
  if (!el) return;
  const totalPages = Math.ceil(backupPage.total / backupPage.limit);
  const currentPage = Math.floor(backupPage.offset / backupPage.limit);
  if (totalPages <= 1) { el.innerHTML = ''; return; }

  let html = '';
  for (let i = 0; i < totalPages && i < 10; i++) {
    const active = i === currentPage ? ' active' : '';
    html += `<button class="page-btn${active}" onclick="goBackupPage(${i})">${i + 1}</button>`;
  }
  if (totalPages > 10) html += `<span class="page-ellipsis">... (${totalPages} pages)</span>`;
  el.innerHTML = html;
}

function goBackupPage(page) {
  backupPage.offset = page * backupPage.limit;
  searchBackups();
}

async function searchBackupContent() {
  const q = ($('#bk-content-q') || {}).value || '';
  if (!q) return;

  const el = $('#bk-content-results');
  el.innerHTML = '<div class="empty">Searching...</div>';

  try {
    const res = await fetch('/api/backups/search-content?q=' + encodeURIComponent(q) + '&max_results=20');
    const data = await res.json();

    if (data.error) {
      el.innerHTML = `<div class="empty">${data.error}</div>`;
      return;
    }

    const matches = data.matches || [];
    if (matches.length === 0) {
      el.innerHTML = '<div class="empty">No matches found</div>';
      return;
    }

    el.innerHTML = `<div class="result-count">${data.total} match(es) found for "${data.query}"</div>` +
      matches.map(m => `<div class="content-match">
        <div class="content-match-header">
          <span class="badge read">${m.backup_id}</span>
          <span class="path">${m.file_name}</span>
          <span class="content-match-line">Line ${m.line}</span>
        </div>
        <pre class="content-snippet">${escapeHtml(m.context)}</pre>
      </div>`).join('');
  } catch (e) {
    el.innerHTML = '<div class="empty">Search failed</div>';
  }
}

function escapeHtml(s) {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

function toggleOpDetail(rowId, btn) {
  const row = document.getElementById(rowId);
  if (!row) return;
  const visible = row.style.display !== 'none';
  row.style.display = visible ? 'none' : 'table-row';
  btn.textContent = visible ? 'View' : 'Hide';
}

async function toggleBackupDetail(backupId, btn) {
  const existingDetail = document.getElementById('detail-' + backupId);
  if (existingDetail) {
    existingDetail.remove();
    btn.textContent = 'Details';
    return;
  }

  btn.textContent = 'Loading...';
  try {
    const res = await fetch('/api/backups/detail/' + backupId);
    const detail = await res.json();
    if (detail.error) {
      btn.textContent = 'Error';
      return;
    }

    const row = btn.closest('tr');
    const colspan = row.children.length;

    const filesHtml = (detail.FileDetails || []).map(f => {
      const name = f.original_path.replace(/\\/g, '/').split('/').pop();
      const origPath = f.original_path.replace(/\\/g, '/');
      const modTime = f.modified_time ? new Date(f.modified_time).toLocaleString() : '—';
      const actions = f.Exists
        ? `<a href="${f.ViewURL}" target="_blank" class="btn-action">View</a>
           <a href="${f.ViewURL}?download=true" class="btn-action btn-download">Download</a>`
        : '<span class="badge error">Missing</span>';

      return `<tr class="detail-file-row">
        <td class="path" title="${origPath}">${origPath}</td>
        <td>${name}</td>
        <td>${formatBytes(f.size)}</td>
        <td class="path" style="font-size:11px;" title="${f.hash}">${f.hash ? f.hash.substring(0, 12) + '...' : '—'}</td>
        <td>${modTime}</td>
        <td>${actions}</td>
      </tr>`;
    }).join('');

    const detailHtml = `<tr id="detail-${backupId}" class="backup-detail-row">
      <td colspan="${colspan}">
        <div class="backup-detail">
          <div class="backup-detail-header">
            <span><strong>Backup:</strong> ${backupId}</span>
            <span><strong>Path:</strong> <span class="path">${detail.BackupPath || '—'}</span></span>
            ${detail.user_context ? `<span><strong>Context:</strong> ${detail.user_context}</span>` : ''}
          </div>
          <table class="detail-files-table">
            <thead><tr><th>Original Path</th><th>File</th><th>Size</th><th>Hash</th><th>Modified</th><th>Actions</th></tr></thead>
            <tbody>${filesHtml || '<tr><td colspan="6" class="empty">No files</td></tr>'}</tbody>
          </table>
        </div>
      </td>
    </tr>`;

    row.insertAdjacentHTML('afterend', detailHtml);
    btn.textContent = 'Close';
  } catch (e) {
    btn.textContent = 'Error';
  }
}

// Statistics
async function fetchStats() {
  try {
    const res = await fetch('/api/stats');
    const s = await res.json();

    // Summary cards
    $('#s-total-ops').textContent = (s.total_ops || 0).toLocaleString();
    const errRate = s.error_rate || 0;
    $('#s-error-rate').textContent = errRate.toFixed(1) + '%';
    $('#s-error-rate').className = 'value ' + (errRate > 5 ? 'red' : 'green');
    $('#s-avg-duration').textContent = formatDuration(s.avg_duration_ms || 0);
    const bIn = formatBytes(s.total_bytes_in || 0);
    const bOut = formatBytes(s.total_bytes_out || 0);
    $('#s-total-bytes').textContent = `${bIn} / ${bOut}`;
    $('#s-tokens').textContent = (s.tokens_estimate || 0).toLocaleString();
    $('#s-timespan').textContent = s.time_span || '—';
    $('#s-total-lines').textContent = (s.total_lines_changed || 0).toLocaleString();
    $('#s-total-matches').textContent = (s.total_matches || 0).toLocaleString();

    // By tool table
    const tools = Object.entries(s.by_tool || {}).sort((a, b) => b[1].count - a[1].count);
    $('#s-by-tool').innerHTML = tools.map(([name, t]) => {
      const errClass = t.error_rate > 5 ? 'color:var(--red)' : '';
      return `<tr>
        <td><span class="badge ${toolBadgeClass(name)}">${name}</span></td>
        <td>${t.count.toLocaleString()}</td>
        <td style="${errClass}">${t.errors}</td>
        <td style="${errClass}">${t.error_rate.toFixed(1)}%</td>
        <td>${formatDuration(t.avg_ms)}</td>
        <td>${formatDuration(t.min_ms)}</td>
        <td>${formatDuration(t.max_ms)}</td>
        <td>${formatDuration(t.p95_ms)}</td>
        <td>${formatBytes(t.total_bytes_in || 0)}</td>
        <td>${formatBytes(t.total_bytes)}</td>
        <td>${(t.total_lines_changed || 0).toLocaleString()}</td>
      </tr>`;
    }).join('') || '<tr><td colspan="11" class="empty">No data</td></tr>';

    // Top files
    $('#s-top-files').innerHTML = (s.top_files || []).map((f, i) => {
      return `<tr>
        <td>${i + 1}</td>
        <td class="path" title="${f.path}">${shortPath(f.path)}</td>
        <td>${f.count}</td>
      </tr>`;
    }).join('') || '<tr><td colspan="3" class="empty">No data</td></tr>';

    // Hour chart
    const hours = s.by_hour || [];
    if (hours.length > 0) {
      const maxCount = Math.max(...hours.map(h => h.count));
      $('#s-hour-chart').innerHTML = hours.map(h => {
        const pct = maxCount > 0 ? (h.count / maxCount * 100) : 0;
        const label = h.hour.substring(11, 16); // "HH:MM"
        return `<div class="hour-bar-wrap" title="${h.hour}: ${h.count} ops">
          <div class="hour-bar" style="height:${pct}%"></div>
          <div class="hour-label">${label}</div>
        </div>`;
      }).join('');
    } else {
      $('#s-hour-chart').innerHTML = '<div class="empty">No hourly data</div>';
    }

    // Risk distribution
    const risks = s.by_risk || {};
    const riskEntries = Object.entries(risks);
    if (riskEntries.length > 0) {
      const riskTotal = riskEntries.reduce((sum, [, c]) => sum + c, 0);
      const riskColors = { LOW: 'var(--green)', MEDIUM: 'var(--yellow)', HIGH: 'var(--orange)', CRITICAL: 'var(--red)' };
      $('#s-risk-dist').innerHTML = riskEntries.map(([level, count]) => {
        const pct = (count / riskTotal * 100).toFixed(1);
        const color = riskColors[level] || 'var(--accent)';
        return `<div class="risk-item">
          <div class="risk-header"><span style="color:${color}">${level}</span><span>${count} (${pct}%)</span></div>
          <div class="risk-track"><div class="risk-fill" style="width:${pct}%;background:${color}"></div></div>
        </div>`;
      }).join('');
    } else {
      $('#s-risk-dist').innerHTML = '<div class="empty" style="padding:16px">No risk data</div>';
    }

    // Slowest ops
    $('#s-slowest').innerHTML = (s.slowest_ops || []).map(op => {
      const statusClass = op.status === 'ok' ? 'ok' : 'error';
      return `<tr>
        <td>${formatTime(op.ts)}</td>
        <td><span class="badge ${toolBadgeClass(op.tool)}">${op.tool}</span></td>
        <td class="path" title="${op.path || ''}">${shortPath(op.path)}</td>
        <td><strong>${formatDuration(op.duration_ms)}</strong></td>
        <td><span class="badge ${statusClass}">${op.status}</span></td>
      </tr>`;
    }).join('') || '<tr><td colspan="5" class="empty">No data</td></tr>';

  } catch (e) {
    // silent
  }
}

// Proxy / Tokens
async function fetchProxyStats() {
  try {
    const res = await fetch('/api/proxy-stats');
    const s = await res.json();

    if (!s.total_calls) {
      $('#p-no-data').style.display = 'block';
      return;
    }
    $('#p-no-data').style.display = 'none';

    $('#p-total-calls').textContent = (s.total_calls || 0).toLocaleString();
    $('#p-tokens-in').textContent = (s.total_tokens_in || 0).toLocaleString();
    $('#p-tokens-out').textContent = (s.total_tokens_out || 0).toLocaleString();
    $('#p-tokens-total').textContent = (s.total_tokens || 0).toLocaleString();
    const errRate = s.error_rate || 0;
    $('#p-error-rate').textContent = errRate.toFixed(1) + '%';
    $('#p-error-rate').className = 'value ' + (errRate > 5 ? 'red' : 'green');
    $('#p-timespan').textContent = s.time_span || '—';

    // By model table
    const models = Object.entries(s.by_model || {}).sort((a, b) => b[1].count - a[1].count);
    const modelColors = ['var(--accent)', 'var(--green)', 'var(--yellow)', 'var(--orange)', 'var(--red)'];
    $('#p-by-model').innerHTML = models.map(([name, m], i) => {
      const errClass = m.error_rate > 5 ? 'color:var(--red)' : '';
      const total = m.tokens_in + m.tokens_out;
      return `<tr>
        <td><span class="badge" style="background:${modelColors[i % modelColors.length]}22;color:${modelColors[i % modelColors.length]}">${name}</span></td>
        <td>${m.count.toLocaleString()}</td>
        <td>${m.tokens_in.toLocaleString()}</td>
        <td>${m.tokens_out.toLocaleString()}</td>
        <td><strong>${total.toLocaleString()}</strong></td>
        <td style="${errClass}">${m.errors}</td>
        <td style="${errClass}">${m.error_rate.toFixed(1)}%</td>
        <td>${formatDuration(m.avg_ms)}</td>
      </tr>`;
    }).join('') || '<tr><td colspan="8" class="empty">No data</td></tr>';

    // By tool table
    const tools = Object.entries(s.by_tool || {}).sort((a, b) => (b[1].tokens_in + b[1].tokens_out) - (a[1].tokens_in + a[1].tokens_out));
    $('#p-by-tool').innerHTML = tools.map(([name, t]) => {
      const errClass = t.error_rate > 5 ? 'color:var(--red)' : '';
      return `<tr>
        <td><span class="badge ${toolBadgeClass(name)}">${name}</span></td>
        <td>${t.count.toLocaleString()}</td>
        <td>${t.tokens_in.toLocaleString()}</td>
        <td>${t.tokens_out.toLocaleString()}</td>
        <td style="${errClass}">${t.error_rate.toFixed(1)}%</td>
        <td>${formatDuration(t.avg_ms)}</td>
      </tr>`;
    }).join('') || '<tr><td colspan="6" class="empty">No data</td></tr>';

    // Model token distribution chart
    if (models.length > 0) {
      const totalTokens = s.total_tokens || 1;
      $('#p-model-chart').innerHTML = models.map(([name, m], i) => {
        const total = m.tokens_in + m.tokens_out;
        const pct = (total / totalTokens * 100).toFixed(1);
        const color = modelColors[i % modelColors.length];
        const inPct = total > 0 ? (m.tokens_in / total * 100).toFixed(0) : 0;
        return `<div class="risk-item">
          <div class="risk-header">
            <span style="color:${color}">${name}</span>
            <span>${total.toLocaleString()} tokens (${pct}%) — ${inPct}% in / ${100-inPct}% out</span>
          </div>
          <div class="risk-track"><div class="risk-fill" style="width:${pct}%;background:${color}"></div></div>
        </div>`;
      }).join('');
    } else {
      $('#p-model-chart').innerHTML = '<div class="empty" style="padding:16px">No model data</div>';
    }
  } catch (e) {
    // silent
  }
}

// SSE for real-time updates
function connectSSE() {
  const source = new EventSource('/api/operations/live');
  source.onmessage = (event) => {
    try {
      const op = JSON.parse(event.data);
      // Prepend to recent ops (Dashboard page)
      const recentTbody = $('#recent-ops');
      if (recentTbody) {
        recentTbody.insertAdjacentHTML('afterbegin', operationRow(op, false));
        // Keep max 10 rows
        while (recentTbody.children.length > 10) {
          recentTbody.removeChild(recentTbody.lastChild);
        }
      }
      // Prepend to all ops (Operations page) — but not while paused,
      // so the user can study an expanded detail row uninterrupted.
      const allTbody = $('#all-ops');
      if (allTbody && !operationsPaused) {
        allTbody.insertAdjacentHTML('afterbegin', operationRow(op, true));
        // Keep max 200 rows
        while (allTbody.children.length > 400) { // 2 rows per op (data + detail)
          allTbody.removeChild(allTbody.lastChild);
        }
      }
      // Also refresh metrics on new operation
      fetchMetrics();
    } catch (e) {
      // ignore parse errors
    }
  };
  source.onerror = () => {
    source.close();
    // Reconnect after 5 seconds
    setTimeout(connectSSE, 5000);
  };
}

// Backup search event listeners
document.addEventListener('DOMContentLoaded', () => {
  const searchBtn = $('#bk-search-btn');
  const contentBtn = $('#bk-content-btn');
  const searchQ = $('#bk-search-q');
  const presetSel = $('#bk-search-preset');
  const opSel = $('#bk-search-op');
  const dateRange = $('#bk-date-range');
  const contentQ = $('#bk-content-q');

  if (searchBtn) searchBtn.addEventListener('click', () => { backupPage.offset = 0; searchBackups(); });
  if (contentBtn) contentBtn.addEventListener('click', searchBackupContent);

  if (searchQ) searchQ.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') { backupPage.offset = 0; searchBackups(); }
  });
  if (searchQ) searchQ.addEventListener('input', () => {
    clearTimeout(backupDebounceTimer);
    backupDebounceTimer = setTimeout(() => { backupPage.offset = 0; searchBackups(); }, 300);
  });
  if (contentQ) contentQ.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') searchBackupContent();
  });
  if (presetSel) presetSel.addEventListener('change', () => {
    if (dateRange) dateRange.style.display = presetSel.value === 'custom' ? 'flex' : 'none';
    backupPage.offset = 0;
    searchBackups();
  });
  if (opSel) opSel.addEventListener('change', () => { backupPage.offset = 0; searchBackups(); });

  // Operations page — pause/resume toggle. While paused, fetchOperations and SSE
  // stop rewriting #all-ops so the user can inspect an expanded row.
  const pauseBtn = $('#ops-pause-btn');
  if (pauseBtn) {
    pauseBtn.addEventListener('click', () => {
      operationsPaused = !operationsPaused;
      pauseBtn.textContent = operationsPaused ? 'Resume refresh' : 'Pause refresh';
      pauseBtn.style.background = operationsPaused ? 'var(--red, #d9534f)' : '';
      pauseBtn.style.color = operationsPaused ? '#fff' : '';
    });
  }
});

// Normalizer Status
async function fetchNormalizer() {
  try {
    const res = await fetch('/api/normalizer?_t=' + Date.now());
    const s = await res.json();

    $('#n-total').textContent = (s.total_processed || 0).toLocaleString();
    $('#n-normalized').textContent = (s.total_normalized || 0).toLocaleString();
    const rate = s.total_processed > 0
      ? ((s.total_normalized / s.total_processed) * 100).toFixed(1)
      : '0.0';
    $('#n-rate').textContent = rate + '%';

    const byRule = s.by_rule || {};
    $('#n-rules').textContent = Object.keys(byRule).length;

    // By tool table — values are {processed, normalized} objects
    const byTool = Object.entries(s.by_tool || {}).sort((a, b) => {
      const av = typeof a[1] === 'object' ? (a[1].processed || 0) : a[1];
      const bv = typeof b[1] === 'object' ? (b[1].processed || 0) : b[1];
      return bv - av;
    });
    $('#n-by-tool').innerHTML = byTool.map(([name, v]) => {
      const processed = typeof v === 'object' ? (v.processed || 0) : v;
      const normalized = typeof v === 'object' ? (v.normalized || 0) : 0;
      return `<tr>
        <td><span class="badge ${toolBadgeClass(name)}">${name}</span></td>
        <td>${processed.toLocaleString()} / <span style="color:var(--green)">${normalized.toLocaleString()} norm</span></td>
      </tr>`;
    }).join('') || '<tr><td colspan="2" class="empty">No normalizations yet</td></tr>';

    // By rule table — values are {rule_id, type, hits, tools} objects
    const rules = Object.entries(byRule).sort((a, b) => {
      const av = typeof a[1] === 'object' ? (a[1].hits || 0) : a[1];
      const bv = typeof b[1] === 'object' ? (b[1].hits || 0) : b[1];
      return bv - av;
    });
    $('#n-by-rule').innerHTML = rules.map(([id, v]) => {
      const hits = typeof v === 'object' ? (v.hits || 0) : v;
      return `<tr>
        <td><code>${id}</code></td>
        <td>${hits.toLocaleString()}</td>
      </tr>`;
    }).join('') || '<tr><td colspan="2" class="empty">No rules triggered yet</td></tr>';

    // Recent normalizations — each entry has {ts, tool, applied: [{rule_id, type, param, from, to}]}
    const recent = s.recent_normalizations || s.recent || [];
    const rows = [];
    for (const r of recent) {
      const applied = r.applied || [];
      if (applied.length === 0) {
        rows.push(`<tr>
          <td>${r.ts ? formatTime(r.ts) : '—'}</td>
          <td><span class="badge ${toolBadgeClass(r.tool || '')}">${r.tool || '—'}</span></td>
          <td>—</td><td>—</td><td>—</td><td>—</td>
        </tr>`);
      } else {
        for (const a of applied) {
          rows.push(`<tr>
            <td>${r.ts ? formatTime(r.ts) : '—'}</td>
            <td><span class="badge ${toolBadgeClass(r.tool || '')}">${r.tool || '—'}</span></td>
            <td><code>${a.rule_id || '—'}</code></td>
            <td>${a.param || '—'}</td>
            <td><code>${a.from || '—'}</code></td>
            <td><code>${a.to || '—'}</code></td>
          </tr>`);
        }
      }
    }
    $('#n-recent').innerHTML = rows.join('') || '<tr><td colspan="6" class="empty">No recent normalizations</td></tr>';
  } catch (e) {
    // silent
  }
}

// Error Patterns
async function fetchErrorPatterns() {
  try {
    const res = await fetch('/api/error-patterns?_t=' + Date.now());
    const s = await res.json();

    $('#ep-total').textContent = (s.total_errors || 0).toLocaleString();
    $('#ep-unique').textContent = (s.unique_patterns || 0).toLocaleString();
    $('#ep-suggestions').textContent = (s.with_suggestions || 0).toLocaleString();

    const patterns = s.patterns || [];
    const trendIcons = { increasing: '\u2191', decreasing: '\u2193', stable: '\u2192' };
    const trendColors = { increasing: 'var(--red)', decreasing: 'var(--green)', stable: 'var(--text-dim)' };

    $('#ep-patterns').innerHTML = patterns.map(p => {
      const suggested = p.suggested_rule
        ? `<span class="badge" style="background:var(--green)22;color:var(--green)">${p.suggested_rule.type}: ${p.suggested_rule.from} \u2192 ${p.suggested_rule.to}</span>`
        : '—';
      return `<tr>
        <td><span class="badge ${toolBadgeClass(p.tool)}">${p.tool}</span></td>
        <td style="max-width:400px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;" title="${escapeHtml(p.pattern)}">${escapeHtml(p.pattern)}</td>
        <td>${p.count}</td>
        <td style="color:${trendColors[p.trend] || 'inherit'}">${trendIcons[p.trend] || '—'} ${p.trend}</td>
        <td>${formatTime(p.first_seen)}</td>
        <td>${formatTime(p.last_seen)}</td>
        <td>${suggested}</td>
      </tr>`;
    }).join('') || '<tr><td colspan="7" class="empty">No error patterns detected</td></tr>';
  } catch (e) {
    // silent
  }
}

// Initial load and polling
fetchMetrics();
fetchOperations();
searchBackups();
fetchStats();
fetchProxyStats();
fetchNormalizer();
fetchErrorPatterns();
fetchROI();
connectSSE();

// Poll every 5 seconds
setInterval(fetchMetrics, 5000);
setInterval(fetchOperations, 10000);
setInterval(searchBackups, 30000);
setInterval(fetchStats, 15000);
setInterval(fetchProxyStats, 15000);
setInterval(fetchNormalizer, 10000);
setInterval(fetchErrorPatterns, 30000);
setInterval(fetchROI, 30000);

// ─── ROI / Savings page ───────────────────────────────────────────────────────
async function fetchROI() {
  try {
    const res = await fetch('/api/roi?_t=' + Date.now());
    const d = await res.json();

    const fmt = n => (n || 0).toLocaleString();
    const pct = n => (n || 0).toFixed(1) + '%';
    const kb  = n => n > 1024 ? (n/1024).toFixed(1) + ' KB' : n + ' B';

    $('#roi-saved').textContent     = fmt(d.tokens_saved);
    $('#roi-pct').textContent       = pct(d.savings_pct);
    $('#roi-consumed').textContent  = fmt(d.tokens_consumed);
    $('#roi-baseline').textContent  = fmt(d.tokens_baseline);
    $('#roi-sessions').textContent  = fmt(d.session_count);
    $('#roi-range-pct').textContent = pct(d.range_read_pct) + ' of reads';
    $('#roi-avg-read-pct').textContent = pct(d.avg_read_pct);
    $('#roi-timespan').textContent  = d.time_span || '—';

    // By tool table
    const byTool = d.by_tool || [];
    $('#roi-by-tool').innerHTML = byTool.map(t => `<tr>
      <td><span class="badge ${toolBadgeClass(t.tool)}">${t.tool}</span></td>
      <td>${fmt(t.ops_count)}</td>
      <td>${fmt(t.tokens_consumed)}</td>
      <td>${fmt(t.tokens_baseline)}</td>
      <td class="green">${fmt(t.tokens_saved)}</td>
      <td>${pct(t.savings_pct)}</td>
      <td>${t.avg_saved_per_op.toFixed(0)}</td>
    </tr>`).join('') || '<tr><td colspan="7" class="empty">No ROI data yet — requires v4.3.3+ server</td></tr>';

    // Top savings
    const top = d.top_savings || [];
    $('#roi-top-savings').innerHTML = top.map(op => `<tr>
      <td>${formatTime(op.ts)}</td>
      <td><span class="badge ${toolBadgeClass(op.tool)}">${op.tool}</span></td>
      <td style="max-width:200px;overflow:hidden;text-overflow:ellipsis" title="${escapeHtml(op.path||'')}">${escapeHtml(op.path||'—')}</td>
      <td>${op.file_size ? kb(op.file_size) : '—'}</td>
      <td>${fmt(op.tokens_baseline)}</td>
      <td>${fmt(op.tokens_consumed)}</td>
      <td class="green">${fmt(op.tokens_saved)}</td>
    </tr>`).join('') || '<tr><td colspan="7" class="empty">No data</td></tr>';

    // Sessions table
    const sessions = d.sessions || [];
    $('#roi-sessions-table').innerHTML = sessions.map(s => `<tr>
      <td><code>${s.session_id}</code></td>
      <td>${formatTime(s.first_op)}</td>
      <td>${s.duration_min < 1 ? '<1 min' : s.duration_min.toFixed(0) + ' min'}</td>
      <td>${fmt(s.ops_count)}</td>
      <td>${fmt(s.tokens_consumed)}</td>
      <td class="green">${fmt(s.tokens_saved)}</td>
      <td>${pct(s.savings_pct)}</td>
      <td>${s.errors > 0 ? `<span style="color:var(--red)">${s.errors}</span>` : '0'}</td>
    </tr>`).join('') || '<tr><td colspan="8" class="empty">No session data yet</td></tr>';

    // Anti-patterns
    const ap = d.anti_patterns || {};
    const apEntries = Object.entries(ap).sort((a,b) => b[1]-a[1]);
    if (apEntries.length > 0) {
      $('#roi-antipatterns-section').style.display = '';
      $('#roi-antipatterns').innerHTML = apEntries.map(([k, v]) =>
        `<tr><td>${escapeHtml(k)}</td><td>${v}</td></tr>`
      ).join('');
    } else {
      $('#roi-antipatterns-section').style.display = 'none';
    }
  } catch(e) {
    // silent
  }
}
