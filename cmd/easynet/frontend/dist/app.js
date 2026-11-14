const App = () => window.go && window.go.main && window.go.main.App;
const RT = () => window.runtime;

function winMin() { RT() && RT().WindowMinimise(); }
function winMax() { RT() && RT().WindowToggleMaximise(); }
function winClose() { RT() && RT().Quit(); }
function openRepo() { RT() && RT().BrowserOpenURL("https://github.com/love486love/easynet"); }
function openRelease() { RT() && RT().BrowserOpenURL("https://github.com/love486love/easynet/releases"); }

/* ---------- 主题 ---------- */
const THEME_KEY = 'easynet-theme';
const WINDOW_BG = { dark: [20, 22, 26, 1], light: [246, 248, 250, 1] };
let themeMode = 'auto';

function resolveTheme(mode) {
  if (mode === 'auto') {
    return (window.matchMedia && window.matchMedia('(prefers-color-scheme: light)').matches) ? 'light' : 'dark';
  }
  return mode === 'light' ? 'light' : 'dark';
}

function updateThemeSeg() {
  const seg = document.getElementById('themeSeg');
  if (!seg) return;
  seg.querySelectorAll('.seg-item').forEach(function (el) {
    el.classList.toggle('on', el.dataset.mode === themeMode);
  });
}

function applyTheme(mode) {
  themeMode = (mode === 'light' || mode === 'dark') ? mode : 'auto';
  const m = resolveTheme(themeMode);
  document.body.dataset.theme = m;
  const btn = document.getElementById('themeBtn');
  if (btn) {
    btn.title = m === 'light' ? '切换到深色' : '切换到浅色';
    btn.setAttribute('aria-checked', m === 'light' ? 'true' : 'false');
  }
  try { localStorage.setItem(THEME_KEY, themeMode); } catch (e) { }
  updateThemeSeg();
  if (RT() && RT().WindowSetBackgroundColour) {
    const c = WINDOW_BG[m];
    RT().WindowSetBackgroundColour(c[0], c[1], c[2], c[3]);
  }
}

function toggleTheme() {
  applyTheme(document.body.dataset.theme === 'light' ? 'dark' : 'light');
}

(function initTheme() {
  let saved = null;
  try { saved = localStorage.getItem(THEME_KEY); } catch (e) { }
  if (saved !== 'light' && saved !== 'dark' && saved !== 'auto') saved = 'auto';
  applyTheme(saved);
  if (window.matchMedia) {
    const mq = window.matchMedia('(prefers-color-scheme: dark)');
    if (mq.addEventListener) {
      mq.addEventListener('change', function () {
        if (themeMode === 'auto') applyTheme('auto');
      });
    }
  }
})();

document.querySelectorAll('.nav-item').forEach(el => {
  el.onclick = () => {
    document.querySelectorAll('.nav-item').forEach(n => n.classList.remove('active'));
    el.classList.add('active');
    document.querySelectorAll('.page').forEach(p => p.classList.remove('active'));
    document.getElementById('page-' + el.dataset.page).classList.add('active');
    if (el.dataset.page === 'logs') loadLogs();
  };
});

function setMsg(text, ok) {
  const el = document.getElementById('msg');
  el.textContent = text;
  el.className = 'msg ' + (ok === true ? 'ok' : ok === false ? 'bad' : '');
}

let toastTimer = null;
function toast(text, ok) {
  let el = document.getElementById('toast');
  if (!el) {
    el = document.createElement('div');
    el.id = 'toast';
    document.body.appendChild(el);
  }
  el.textContent = text;
  el.className = 'toast show' + (ok === false ? ' bad' : '');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(function () { el.className = 'toast' + (ok === false ? ' bad' : ''); }, 2600);
}

function renderState(s) {
  const dot = document.getElementById('dot');
  const text = document.getElementById('stateText');
  const sub = document.getElementById('stateSub');
  dot.className = 'dot ' + (s.state || '');
  if (s.state === 'online') { text.textContent = '已连接'; sub.textContent = '网络认证正常'; }
  else if (s.state === 'offline') { text.textContent = '未认证'; sub.textContent = '需要登录校园网'; }
  else { text.textContent = s.ssid ? '状态未知' : '未连接 Wi-Fi'; sub.textContent = s.message || '无法判断网络状态'; }

  const detail = document.getElementById('stateDetail');
  if (detail) {
    detail.textContent = s.detail || '';
    detail.style.display = s.detail ? 'block' : 'none';
  }

  document.getElementById('ssid').textContent = s.ssid || '—';
  document.getElementById('ipv4').textContent = s.ipv4 || '—';
  document.getElementById('gateway').textContent = s.gateway || '—';
  document.getElementById('gwmac').textContent = s.gateway_mac || '（未取到）';
  document.getElementById('mac').textContent = (s.mac || '—') + (s.random_mac ? '（随机）' : '');
  document.getElementById('cred').innerHTML =
    '<span class="pill ' + (s.has_cred ? 'ok' : 'bad') + '">' + (s.has_cred ? '账号已保存' : '账号未保存') + '</span>' +
    '<span class="pill ' + (s.has_trust ? 'ok' : '') + '">' + (s.has_trust ? '网关已信任' : '网关未记录') + '</span>';
  document.getElementById('credHint').style.display = s.has_cred ? 'none' : 'flex';
}

function goAccount() {
  document.querySelector('.nav-item[data-page="account"]').click();
}

// 实时网速：每秒取一次累计字节做差分（App.Counters → 默认路由网卡），
// 大字显示瞬时速率，附本机累计/会话峰值，右侧画最近 60 秒历史曲线。
let netPrev = null;
const netHist = []; // 最近 60 个采样点 {d, u}（KB/s）
const netPeak = { d: 0 };

function fmtRate(bps) {
  const k = bps / 1024;
  if (k < 1024) return { v: k.toFixed(1), u: 'KB/s' };
  return { v: (k / 1024).toFixed(2), u: 'MB/s' };
}
function fmtBytes(n) {
  if (n >= 1073741824) return (n / 1073741824).toFixed(2) + ' GB';
  if (n >= 1048576) return (n / 1048576).toFixed(1) + ' MB';
  if (n >= 1024) return (n / 1024).toFixed(0) + ' KB';
  return Math.round(n) + ' B';
}
function setRateNum(id, r) {
  const el = document.getElementById(id);
  if (el) el.textContent = r.v;
  const unit = document.getElementById(id + 'U');
  if (unit) unit.textContent = r.u;
}
async function loadNetRate() {
  if (!App() || !App().Counters) return;
  try {
    const c = await App().Counters();
    const now = Date.now();
    let d = 0, u = 0;
    if (netPrev && now > netPrev.t) {
      const dt = (now - netPrev.t) / 1000;
      // 计数器回绕或网卡切换时差分取 0，不显示负值
      d = c.in >= netPrev.in ? (c.in - netPrev.in) / dt : 0;
      u = c.out >= netPrev.out ? (c.out - netPrev.out) / dt : 0;
    }
    netPrev = { in: c.in, out: c.out, t: now };
    if (d > netPeak.d) netPeak.d = d;

    setRateNum('rateDown', fmtRate(d));
    setRateNum('rateUp', fmtRate(u));
    const st = document.getElementById('rateStats');
    if (st) {
      st.textContent = '本机累计 ↓ ' + fmtBytes(c.in) + '  ↑ ' + fmtBytes(c.out) +
        '   ｜   会话峰值 ↓ ' + fmtBytes(netPeak.d) + '/s';
    }

    netHist.push({ d: d / 1024, u: u / 1024 }); // 曲线用 KB/s
    if (netHist.length > 60) netHist.shift();
    drawRateChart();
  } catch (e) { }
}

// drawRateChart 画最近 60 秒曲线：下行渐变面积 + 上行虚线，随主题取色。
function drawRateChart() {
  const cv = document.getElementById('rateChart');
  if (!cv || !cv.getContext) return;
  const dpr = window.devicePixelRatio || 1;
  const w = cv.clientWidth || 440, h = cv.clientHeight || 76;
  if (cv.width !== w * dpr || cv.height !== h * dpr) {
    cv.width = w * dpr; cv.height = h * dpr;
  }
  const g = cv.getContext('2d');
  g.setTransform(dpr, 0, 0, dpr, 0, 0);
  g.clearRect(0, 0, w, h);

  const cs = getComputedStyle(document.documentElement);
  const cDown = (cs.getPropertyValue('--accent') || '#0b7bff').trim();
  const cUp = (cs.getPropertyValue('--accent2') || '#4dd0e1').trim();

  const n = 60;
  let max = 0.1;
  for (const p of netHist) { if (p.d > max) max = p.d; if (p.u > max) max = p.u; }
  max *= 1.25;
  const x = i => (i / (n - 1)) * w;
  const y = v => h - 3 - (v / max) * (h - 10);
  const pad = n - netHist.length; // 未采样的历史留白（靠右对齐最新）

  // 下行：折线 + 渐变填充
  g.beginPath();
  netHist.forEach((p, i) => {
    const px = x(pad + i), py = y(p.d);
    i ? g.lineTo(px, py) : g.moveTo(px, py);
  });
  if (netHist.length > 1) {
    g.strokeStyle = cDown; g.lineWidth = 1.8; g.lineJoin = 'round'; g.stroke();
    g.lineTo(x(n - 1), h); g.lineTo(x(pad), h); g.closePath();
    const grad = g.createLinearGradient(0, 0, 0, h);
    grad.addColorStop(0, rgba(cDown, 0.25)); grad.addColorStop(1, rgba(cDown, 0));
    g.fillStyle = grad; g.fill();
  }

  // 上行：虚线
  g.beginPath();
  netHist.forEach((p, i) => {
    const px = x(pad + i), py = y(p.u);
    i ? g.lineTo(px, py) : g.moveTo(px, py);
  });
  if (netHist.length > 1) {
    g.strokeStyle = cUp; g.lineWidth = 1.4;
    g.setLineDash([3, 3]); g.stroke(); g.setLineDash([]);
  }
}
function rgba(c, a) {
  if (c[0] !== '#') return `rgba(11,123,255,${a})`;
  let s = c.slice(1);
  if (s.length === 3) s = s.split('').map(x => x + x).join('');
  const r = parseInt(s.slice(0, 2), 16), gg = parseInt(s.slice(2, 4), 16), b = parseInt(s.slice(4, 6), 16);
  return `rgba(${r},${gg},${b},${a})`;
}

async function loadDaemon() {
  if (!App()) return;
  try {
    const d = await App().Daemon();
    const running = !!d.running;
    const dot = document.getElementById('dDot');
    dot.className = 'dot ' + (running ? (d.state === 'online' ? 'online' : '') : 'offline');
    document.getElementById('dTitle').textContent = running ? '后台守护运行中' : '后台守护未运行';
    let sub = running ? '已自动维持在线，关闭窗口不影响' : '关闭窗口后不会自动重连';
    if (d.message) sub = d.message;
    document.getElementById('dSub').textContent = sub;
    document.getElementById('dSwitch').classList.toggle('on', !!(await App().DaemonEnabled()));
  } catch (e) { }
}

async function toggleDaemon() {
  if (!App()) return;
  const sw = document.getElementById('dSwitch');
  const on = !sw.classList.contains('on');
  sw.style.pointerEvents = 'none';
  setMsg(on ? '正在开启守护…' : '正在关闭守护…');
  try {
    const msg = await App().SetDaemonEnabled(on);
    setMsg(msg || (on ? '守护已开启' : '守护已关闭'), true);
  } catch (e) {
    setMsg(String(e), false);
  }
  sw.style.pointerEvents = '';
  await refresh();
}

const CARRIER_NAMES = { cmcc: '中国移动', ctcc: '中国电信', cucc: '中国联通', campus: '校园线路' };
const CAMPUS_NAMES = { dongqu: '东区', benbu: '本部' };
// 校区 → 目标 SSID（与 config.SSIDForCampus 保持一致）
const CAMPUS_SSID = { dongqu: 'JXAU-Stu', benbu: 'JXAU' };
function carrierName(c) { return CARRIER_NAMES[c] || c || '未指定线路'; }

let accountsCache = [];
let selectedUser = '';

async function loadAccounts() {
  if (!App()) return;
  try {
    accountsCache = await App().Accounts();
    if (selectedUser && !accountsCache.some(a => a.username === selectedUser)) {
      selectedUser = '';
    }
    renderAccLists();
    renderFoldCounts();
    renderEditor();
  } catch (e) { }
}

// 双列渲染：东区/本部各一个账号列表，行点击 = 载入编辑器 + 设为该校区使用中。
function renderAccLists() {
  ['dongqu', 'benbu'].forEach(campus => {
    const box = document.getElementById('accList' + (campus === 'dongqu' ? 'Dongqu' : 'Benbu'));
    if (!box) return;
    box.innerHTML = '';
    const list = accountsCache.filter(a => a.campus === campus);
    if (!list.length) {
      const empty = document.createElement('div');
      empty.className = 'acc-empty';
      empty.textContent = '暂无' + CAMPUS_NAMES[campus] + '账号';
      box.appendChild(empty);
      return;
    }
    list.forEach(a => {
      const row = document.createElement('div');
      row.className = 'acc-row' + (a.username === selectedUser ? ' active' : '');
      const main = document.createElement('div');
      main.className = 'acc-row-main';
      const u = document.createElement('div');
      u.className = 'acc-row-user';
      u.textContent = a.username;
      const c = document.createElement('div');
      c.className = 'acc-row-carrier';
      c.textContent = carrierName(a.carrier) + (a.current ? ' · 使用中' : '');
      main.appendChild(u);
      main.appendChild(c);
      row.appendChild(main);
      if (a.current) {
        const badge = document.createElement('span');
        badge.className = 'acc-row-badge';
        badge.textContent = '使用中';
        row.appendChild(badge);
      }
      row.onclick = () => selectAccount(a.username);
      box.appendChild(row);
    });
  });
}

function renderEditor() {
  const a = accountsCache.find(x => x.username === selectedUser);
  const label = document.getElementById('editorLabel');
  const title = document.getElementById('editorTitle');
  const del = document.getElementById('delBtn');
  const campusSel = document.getElementById('editorCampus');
  if (a) {
    const cname = CAMPUS_NAMES[a.campus] || '东区';
    label.textContent = cname + '账号' + (a.current ? ' · 使用中' : '');
    title.textContent = a.username + ' · ' + carrierName(a.carrier);
    del.style.display = 'inline-flex';
    document.getElementById('user').value = a.username;
    document.getElementById('carrier').value = a.carrier || 'cmcc';
    campusSel.value = a.campus || 'dongqu';
  } else {
    if (!campusSel.value) campusSel.value = 'dongqu';
    label.textContent = '新增' + (CAMPUS_NAMES[campusSel.value] || '东区') + '账号';
    title.textContent = '填写学号、密码与运营商';
    del.style.display = 'none';
    document.getElementById('user').value = '';
    document.getElementById('carrier').value = 'cmcc';
  }
  document.getElementById('pass').value = '';
}

function newAccount(campus) {
  selectedUser = '';
  document.getElementById('editorCampus').value = campus || 'dongqu';
  renderEditor();
  document.getElementById('user').focus();
}

// 校区下拉变更时只刷新标题，不动已填内容。
function updateCampusLabel() {
  const campus = document.getElementById('editorCampus').value || 'dongqu';
  document.getElementById('editorLabel').textContent = '新增' + (CAMPUS_NAMES[campus] || '东区') + '账号';
}

async function selectAccount(username) {
  const el = document.getElementById('credMsg');
  try {
    const msg = await App().SelectAccount(username);
    selectedUser = username;
    el.textContent = msg;
    el.className = 'msg ok';
    await refresh();
  } catch (e) {
    el.textContent = String(e);
    el.className = 'msg bad';
  }
}

async function deleteCurrent() {
  const a = accountsCache.find(x => x.username === selectedUser);
  if (!a) return;
  if (!confirm('确定删除账号 ' + a.username + '？')) return;
  const el = document.getElementById('credMsg');
  try {
    const msg = await App().DeleteAccount(a.username);
    el.textContent = msg;
    el.className = 'msg ok';
    selectedUser = '';
    await refresh();
  } catch (e) {
    el.textContent = String(e);
    el.className = 'msg bad';
  }
}

/* ---------- 校区切换（右上角两段式，点击滑动） ---------- */
function markCampusSide(active) {
  const seg = document.getElementById('campusSeg');
  if (!seg) return;
  seg.classList.toggle('at-benbu', active === 'benbu');
  seg.querySelectorAll('.campus-seg-item').forEach(el => {
    el.classList.toggle('on', el.dataset.c === active);
  });
}

async function loadCampus() {
  if (!App() || !App().Campus) return;
  try {
    const c = await App().Campus();
    // 指示块优先跟随实际连接（门户由网络决定）；未连校园网时显示记忆的偏好。
    const at = c.connected || c.prefer || 'dongqu';
    markCampusSide(at);
    const note = document.getElementById('campusNote');
    if (note) {
      if (c.connected) {
        note.textContent = '已连接' + CAMPUS_NAMES[c.connected] + ' · ' + c.ssid;
        note.className = 'campus-note ok';
      } else if (c.ssid) {
        note.textContent = '非校园网 ' + c.ssid + '，回连：' + CAMPUS_NAMES[c.prefer || 'dongqu'];
        note.className = 'campus-note';
      } else {
        note.textContent = '未连 Wi-Fi，回连：' + CAMPUS_NAMES[c.prefer || 'dongqu'];
        note.className = 'campus-note';
      }
    }
    // 设置页「自动连接校园 Wi-Fi」文案跟随滑条所选校区。
    const wd = document.getElementById('wifiDesc');
    if (wd) {
      const prefer = c.prefer || 'dongqu';
      wd.textContent = '守护发现没连校园网时，自动连回' + CAMPUS_NAMES[prefer] +
        '校园网 ' + (CAMPUS_SSID[prefer] || '') + '。';
    }
  } catch (e) { }
}

async function setCampus(campus) {
  if (!App() || !App().SetCampus) return;
  markCampusSide(campus); // 点击即滑动
  try {
    const msg = await App().SetCampus(campus);
    setMsg(msg, true);
  } catch (e) {
    setMsg(String(e), false);
  }
  await loadCampus();
}

/* ---------- 账号折叠面板 ---------- */
function toggleFold(campus) {
  const el = document.getElementById(campus === 'dongqu' ? 'foldDongqu' : 'foldBenbu');
  if (el) el.classList.toggle('open');
}

// 头部计数徽标：N 个 · 使用中
function renderFoldCounts() {
  const set = (id, campus) => {
    const el = document.getElementById(id);
    if (!el) return;
    const list = accountsCache.filter(a => a.campus === campus);
    const cur = list.find(a => a.current);
    el.textContent = list.length
      ? (list.length + ' 个' + (cur ? ' · 使用中 ' + cur.username : ''))
      : '未配置';
  };
  set('countDongqu', 'dongqu');
  set('countBenbu', 'benbu');
}

async function refresh() {
  if (!App()) { setMsg('后端未就绪', false); return; }
  try { renderState(await App().Status()); }
  catch (e) { setMsg('刷新失败，请稍后重试', false); }
  await loadLogs();
  await loadDaemon();
  await loadAccounts();
  await loadWifiAuto();
  await loadCampus();
}

function setLogMsg(text, ok) {
  const el = document.getElementById('logMsg');
  el.textContent = text || '';
  el.className = 'msg ' + (ok === true ? 'ok' : ok === false ? 'bad' : '');
}

async function exportLogs() {
  if (!App()) return;
  try {
    const msg = await App().SaveLogAs();
    if (!msg) { setLogMsg('已取消导出'); return; }
    setLogMsg(msg, true);
  } catch (e) {
    setLogMsg('导出失败：' + e + '。可改用「打开日志目录」手动复制日志文件。', false);
  }
}

async function openLogsDir() {
  if (!App()) return;
  try {
    const msg = await App().OpenLogsDir();
    setLogMsg(msg, true);
  } catch (e) {
    setLogMsg(String(e), false);
  }
}

async function loadLogs() {
  if (!App()) return;
  try {
    const lines = await App().Logs();
    const pre = document.getElementById('logs');
    pre.textContent = (lines || []).join('\n') || '（暂无日志）';
    pre.scrollTop = pre.scrollHeight;
  } catch (e) { }
}

async function doLogin() {
  if (!App()) return;
  const btn = document.getElementById('loginBtn');
  btn.disabled = true;
  setMsg('正在认证…');
  try {
    const msg = await App().Login();
    setMsg(msg, msg.indexOf('成功') >= 0);
  } catch (e) {
    setMsg(String(e), false);
  } finally {
    btn.disabled = false;
    await refresh();
  }
}

async function saveCred() {
  if (!App()) return;
  const el = document.getElementById('credMsg');
  const username = document.getElementById('user').value.trim();
  const password = document.getElementById('pass').value;
  const carrier = document.getElementById('carrier').value;
  const campus = document.getElementById('editorCampus').value || 'dongqu';
  if (!username || !password) {
    el.textContent = '账号和密码不能为空';
    el.className = 'msg bad';
    return;
  }
  try {
    const msg = await App().SaveCred(username, password, carrier, campus);
    el.textContent = msg;
    el.className = 'msg ok';
    document.getElementById('pass').value = '';
    selectedUser = username;
    // 确保目标校区面板展开，保存后能立刻看到新账号。
    const fold = document.getElementById(campus === 'dongqu' ? 'foldDongqu' : 'foldBenbu');
    if (fold) fold.classList.add('open');
    await refresh();
    // 守护开关默认开启：首次保存账号后自动把守护拉起来，不必再手动点一次。
    if (App().EnsureDaemon) {
      App().EnsureDaemon().then(function (m) { if (m) setMsg(m, true); }).catch(function () { });
    }
  } catch (e) {
    el.textContent = String(e);
    el.className = 'msg bad';
  }
}

async function loadAutostart() {
  if (!App()) return;
  try {
    const s = await App().Autostart();
    document.getElementById('autoSwitch').classList.toggle('on', !!s.enabled);
  } catch (e) { }
}

async function loadWifiAuto() {
  if (!App()) return;
  try {
    const on = !!(await App().WifiAutoEnabled());
    document.getElementById('wifiSwitch').classList.toggle('on', on);
  } catch (e) { }
}

async function toggleWifiAuto() {
  if (!App()) return;
  const sw = document.getElementById('wifiSwitch');
  const next = !sw.classList.contains('on');
  sw.classList.toggle('on', next); // 先给视觉反馈，再等后端
  try {
    const msg = await App().SetWifiAutoEnabled(next);
    await loadWifiAuto();
    toast(msg || (next ? '已开启自动连接' : '已关闭自动连接'), true);
  } catch (e) {
    sw.classList.toggle('on', !next);
    toast((e && e.message) ? e.message : String(e), false);
  }
}

async function toggleAutostart() {
  if (!App()) return;
  const sw = document.getElementById('autoSwitch');
  const next = !sw.classList.contains('on');
  sw.classList.toggle('on', next);
  try {
    const msg = await App().SetAutostart(next);
    await loadAutostart();
    toast(msg || (next ? '已开启开机自启' : '已关闭开机自启'), true);
  } catch (e) {
    sw.classList.toggle('on', !next);
    toast((e && e.message) ? e.message : String(e), false);
  }
}

let updateInfo = null;

async function checkUpdate(silent) {
  if (!App()) return;
  const badge = document.getElementById('verBadge');
  const msg = document.getElementById('updateMsg');
  const pill = document.getElementById('updatePill');
  const st = document.getElementById('updateState');
  const nums = document.getElementById('updateNums');
  const notes = document.getElementById('updateNotes');
  const btn = document.getElementById('updateBtn');

  if (pill) { pill.textContent = '检查中'; pill.classList.remove('on'); }
  if (st && !silent) st.textContent = '正在检查最新版本…';
  if (msg) { msg.textContent = ''; msg.className = 'msg'; }

  try {
    const u = await App().CheckUpdate();
    updateInfo = u;

    if (badge) {
      if (u.has_update) {
        badge.classList.add('update');
        badge.textContent = 'v' + u.current + ' → v' + u.latest;
        badge.title = '发现新版本 v' + u.latest + '，点击前往下载';
      } else {
        badge.classList.remove('update');
        badge.textContent = 'v' + u.current;
        badge.title = '已是最新版本';
      }
    }
    if (pill) {
      pill.textContent = u.has_update ? '有新版本' : '已是最新';
      pill.classList.toggle('on', u.has_update);
    }
    if (st) st.textContent = u.has_update ? ('发现新版本 v' + u.latest) : ('当前已是最新版本 ' + u.current);
    if (nums) nums.textContent = '当前 ' + u.current + '      最新 ' + u.latest;

    if (notes) {
      if (u.has_update && u.body) {
        notes.hidden = false;
        notes.textContent = u.body;
      } else {
        notes.hidden = true;
        notes.textContent = '';
      }
    }
    if (btn) {
      btn.hidden = !(u.has_update && u.can_auto);
      btn.disabled = false;
      if (u.has_update && u.can_auto) btn.textContent = '立即更新到 v' + u.latest;
    }

    if (msg && !silent && u.has_update) {
      msg.className = 'msg ok';
      msg.textContent = '新版本 v' + u.latest + ' 已发布，点击「查看更新说明」了解变化。';
    }
  } catch (e) {
    if (pill) { pill.textContent = '检查失败'; pill.classList.remove('on'); }
    if (btn) btn.hidden = true;
    if (st && !silent) st.textContent = '无法获取更新信息，请检查网络后重试';
    if (msg && !silent) {
      msg.className = 'msg bad';
      msg.textContent = (e && e.message) ? e.message : String(e);
    }
  }
}

async function doUpdate() {
  if (!App()) return;
  const btn = document.getElementById('updateBtn');
  const wrap = document.getElementById('updateProgress');
  const fill = document.getElementById('updateFill');
  const pctEl = document.getElementById('updatePct');
  const speedEl = document.getElementById('updateSpeed');
  const label = btn.textContent;
  btn.disabled = true;
  btn.textContent = '准备下载…';
  if (wrap) {
    wrap.hidden = false;
    if (fill) fill.style.width = '0%';
    if (pctEl) pctEl.textContent = '0%';
    if (speedEl) speedEl.textContent = '正在获取下载地址…';
  }
  try {
    const msg = await App().ApplyUpdate();
    toast(msg, true);
  } catch (e) {
    btn.disabled = false;
    btn.textContent = label;
    if (wrap) wrap.hidden = true;
    toast((e && e.message) ? e.message : String(e), false);
  }
}

if (window.runtime && window.runtime.EventsOn) {
  window.runtime.EventsOn('update-progress', function (d) {
    const wrap = document.getElementById('updateProgress');
    const fill = document.getElementById('updateFill');
    const pctEl = document.getElementById('updatePct');
    const speedEl = document.getElementById('updateSpeed');
    const btn = document.getElementById('updateBtn');
    if (!wrap) return;
    wrap.hidden = false;
    const pct = Math.max(0, Math.min(100, (d && d.pct) || 0));
    if (fill) fill.style.width = pct + '%';
    if (pctEl) pctEl.textContent = pct + '%';
    if (speedEl) {
      speedEl.textContent = pct >= 100
        ? '下载完成，正在安装…'
        : ((d && d.speed) ? (d.speed + ' KB/s') : '');
    }
    if (btn && pct >= 100) btn.textContent = '正在安装…';
  });
}

function onVersionClick() {
  if (updateInfo && updateInfo.has_update) {
    RT() && RT().BrowserOpenURL(updateInfo.url);
    return;
  }
  document.querySelector('.nav-item[data-page="about"]').click();
}

/* ---------- 新手引导 ---------- */
/* ---------- 校园网时段提示 ---------- */
// 江西农业大学：周一至周五 00:00–06:00 断网，周六、周日全天不断网。
const NW_SUN = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M2 12h2M20 12h2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/></svg>';
const NW_MOON = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M20 14.5A8.5 8.5 0 0 1 9.5 4a7 7 0 1 0 10.5 10.5z"/></svg>';

function nwAt(now, hour) {
  return new Date(now.getFullYear(), now.getMonth(), now.getDate(), hour, 0, 0, 0);
}

function nwWindow(now) {
  const dow = now.getDay();
  if (dow >= 1 && dow <= 5 && now.getHours() < 6) {
    return { offline: true, start: nwAt(now, 0), end: nwAt(now, 6) };
  }
  let start = nwAt(now, 6);
  if (now < start) start = new Date(start.getTime() - 86400000);
  for (let i = 1; i <= 8; i++) {
    const d = new Date(now.getFullYear(), now.getMonth(), now.getDate() + i, 0, 0, 0, 0);
    if (d.getDay() >= 1 && d.getDay() <= 5) {
      return { offline: false, start: start, end: d };
    }
  }
  return { offline: false, start: start, end: new Date(start.getTime() + 86400000) };
}

function nwFmt(ms) {
  const m = Math.max(0, Math.floor(ms / 60000));
  const d = Math.floor(m / 1440), h = Math.floor((m % 1440) / 60), mm = m % 60;
  if (d > 0) return d + '天' + (h ? h + '小时' : '');
  if (h > 0) return h + '小时' + (mm ? mm + '分' : '');
  return mm + '分钟';
}

function nwClock(d) {
  const wd = ['日', '一', '二', '三', '四', '五', '六'][d.getDay()];
  const hh = (d.getHours() < 10 ? '0' : '') + d.getHours();
  return '周' + wd + ' ' + hh + ':00';
}

function updateNetWindow() {
  const box = document.getElementById('netWindow');
  if (!box) return;
  const now = new Date();
  const w = nwWindow(now);
  box.classList.toggle('offline', w.offline);
  document.getElementById('nwIcon').innerHTML = w.offline ? NW_MOON : NW_SUN;
  document.getElementById('nwTitle').textContent = w.offline ? '断网中' : '可上网';
  const total = w.end - w.start;
  const pct = total > 0 ? ((now - w.start) / total) * 100 : 0;
  document.getElementById('nwFill').style.width = Math.max(0, Math.min(100, pct)).toFixed(1) + '%';
  document.getElementById('nwRemain').textContent = w.offline
    ? nwFmt(w.end - now) + '后恢复'
    : '距断网 ' + nwFmt(w.end - now);
  document.getElementById('nwEnd').textContent = w.offline
    ? '06:00 恢复上网'
    : nwClock(w.end) + ' 断网';
}

const TUTORIAL_STEPS = [
  {
    title: '欢迎使用 easynet',
    text: '连上校园 Wi-Fi 后自动完成认证，东区和本部校区都能自动登录。下面用 7 步带你完成设置。',
  },
  {
    page: 'account',
    target: '.acc-fold',
    title: '第 1 步 · 保存账号',
    text: '「账号」页分东区、本部两个折叠面板。连哪个校区就用哪边的账号——东区账号填学号密码，本部账号也一样，各自独立保存（DPAPI 加密）。',
  },
  {
    page: 'overview',
    target: '.campus-mini-wrap',
    title: '第 2 步 · 切换校区',
    text: '右上角两段开关选择校区：点击「东区」或「本部」即自动连接对应校园网并登录。滑块会跟随当前实际连上的校区，不用时它只负责记住你的偏好。',
  },
  {
    page: 'overview',
    target: '.daemon-bar',
    title: '第 3 步 · 打开守护',
    text: '守护开关默认打开。开启后掉线会在几秒内自动重连，按探测到的门户自动选用东区或本部协议，期间不需要打开界面。',
  },
  {
    page: 'overview',
    target: '.rate-card',
    title: '第 4 步 · 实时网速',
    text: '概览页底部是实时网速卡：每秒刷新上下行速率，附带累计流量、会话峰值和最近 60 秒曲线，全部来自系统网卡计数器。',
  },
  {
    page: 'settings',
    target: '#setAutostart',
    title: '第 5 步 · 开机自启',
    text: '在「设置」页打开「开机自启」，下次开机守护会自动运行。旁边的「自动连接校园 Wi-Fi」会在没连校园网时按所选校区自动回连。',
  },
  {
    title: '关闭窗口也没关系',
    text: '守护是独立进程。关闭界面后它仍在工作，托盘图标可以查看状态、打开界面或退出。',
  },
  {
    title: '设置完成',
    text: '外观与后台开关都在「设置」页。想再看一遍引导，可以在设置页点「重新播放」。祝你上网顺畅。',
  },
];

let tutIndex = 0;

function startTutorial() {
  tutIndex = 0;
  document.getElementById('tutOverlay').hidden = false;
  showTutorialStep(0);
  // 一开始播放就记录，中途关闭窗口也不会再次自动弹出
  if (App() && App().MarkTutorialDone) {
    App().MarkTutorialDone().catch(function () { });
  }
}

function showTutorialStep(i) {
  tutIndex = i;
  const steps = TUTORIAL_STEPS;
  const s = steps[i];

  if (s.page) {
    const nav = document.querySelector('.nav-item[data-page="' + s.page + '"]');
    if (nav) nav.click();
  }

  document.getElementById('tutStep').textContent = 'STEP ' + (i + 1) + ' / ' + steps.length;
  document.getElementById('tutTitle').textContent = s.title;
  document.getElementById('tutText').textContent = s.text;
  document.getElementById('tutPrev').style.visibility = i === 0 ? 'hidden' : 'visible';
  document.getElementById('tutNext').textContent = i === steps.length - 1 ? '开始使用' : '下一步';

  const dots = document.getElementById('tutDots');
  dots.innerHTML = '';
  steps.forEach((_, idx) => {
    const d = document.createElement('span');
    d.className = 'tut-dot' + (idx === i ? ' on' : '');
    dots.appendChild(d);
  });

  const spot = document.getElementById('tutSpot');
  const dim = document.getElementById('tutDim');
  const card = document.getElementById('tutCard');
  const el = s.target ? document.querySelector(s.target) : null;

  if (el) {
    const r = el.getBoundingClientRect();
    spot.hidden = false;
    dim.hidden = true;
    spot.style.left = (r.left - 6) + 'px';
    spot.style.top = (r.top - 6) + 'px';
    spot.style.width = (r.width + 12) + 'px';
    spot.style.height = (r.height + 12) + 'px';

    let top = r.bottom + 16;
    if (top + 210 > window.innerHeight) top = Math.max(14, r.top - 220);
    let left = r.left;
    if (left + 340 > window.innerWidth - 14) left = window.innerWidth - 354;
    card.style.top = top + 'px';
    card.style.left = Math.max(14, left) + 'px';
  } else {
    spot.hidden = true;
    dim.hidden = false;
    card.style.top = Math.max(14, window.innerHeight / 2 - 120) + 'px';
    card.style.left = Math.max(14, window.innerWidth / 2 - 170) + 'px';
  }
}

function nextTutorial() {
  if (tutIndex >= TUTORIAL_STEPS.length - 1) { endTutorial(); return; }
  showTutorialStep(tutIndex + 1);
}

function prevTutorial() {
  if (tutIndex > 0) showTutorialStep(tutIndex - 1);
}

function endTutorial() {
  document.getElementById('tutOverlay').hidden = true;
}

async function maybeStartTutorial() {
  if (!App() || !App().TutorialDone) return;
  try {
    if (await App().TutorialDone()) return;
  } catch (e) { }
  setTimeout(startTutorial, 900);
}

document.addEventListener('keydown', function (e) {
  if (e.key === 'Escape' && !document.getElementById('tutOverlay').hidden) endTutorial();
});

refresh();
loadAutostart();
setInterval(loadLogs, 5000);
setInterval(loadDaemon, 5000);
setInterval(loadNetRate, 1000);
maybeStartTutorial();

setTimeout(function () {
  const badge = document.getElementById('verBadge');
  const aboutVer = document.querySelector('.about-ver');
  if (App() && App().Version) {
    // Version() 是 Wails 异步绑定（返回 Promise），直接拼串会显示
    // "v[object Promise]"——用 Promise.resolve 兼容同步与异步两种返回。
    Promise.resolve(App().Version()).then(function (ver) {
      const v = 'v' + ver;
      if (badge) badge.textContent = v;
      if (aboutVer) aboutVer.textContent = v;
    }).catch(function () { });
  }
  checkUpdate(true);
}, 2500);

updateNetWindow();
setInterval(updateNetWindow, 30000);
