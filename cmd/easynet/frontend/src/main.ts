import './style.css'

// 迁移阶段：DOM 取元素先用 any 兜底（原 JS 行为不变），类型收窄留给后续类型化步骤。
const $ = (id: string): any => document.getElementById(id)
const $$ = (sel: string): any => document.querySelectorAll(sel)
const $q = (sel: string): any => document.querySelector(sel)

declare global {
  interface Window {
    go: any
    runtime: any
  }
}

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
  const seg = $('themeSeg');
  if (!seg) return;
  seg.querySelectorAll('.seg-item').forEach(function (el) {
    el.classList.toggle('on', el.dataset.mode === themeMode);
  });
}

function applyTheme(mode) {
  themeMode = (mode === 'light' || mode === 'dark') ? mode : 'auto';
  const m = resolveTheme(themeMode);
  document.body.dataset.theme = m;
  const btn = $('themeBtn');
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

$$('.nav-item').forEach(el => {
  el.onclick = () => {
    $$('.nav-item').forEach(n => n.classList.remove('active'));
    el.classList.add('active');
    $$('.page').forEach(p => p.classList.remove('active'));
    $('page-' + el.dataset.page).classList.add('active');
    if (el.dataset.page === 'logs') loadLogs();
  };
});

function setMsg(text: string, ok?: boolean) {
  // 操作反馈同样用弹窗：渐隐提示，不在页面里留一条常驻文本
  if (ok === false) toastError(text);
  else toast(text, ok === true);
  const el = $('msg');
  if (el) { el.textContent = ''; el.className = 'msg'; }
}

let toastTimer = null;
function toast(text, ok) {
  let el = $('toast');
  if (!el) {
    el = document.createElement('div');
    el.id = 'toast';
    document.body.appendChild(el);
  }
  el.textContent = text;
  el.className = 'toast show' + (ok === false ? ' bad' : '');
  clearTimeout(toastTimer);
  // 错误给更长的阅读时间，正常提示保持轻快
  toastTimer = setTimeout(function () { el.className = 'toast' + (ok === false ? ' bad' : ''); }, ok === false ? 5200 : 2600);
}

let lastErrText = '';
let lastErrAt = 0;
// 错误统一走弹窗并自动隐去；同一内容 30 秒内只弹一次，避免轮询反复刷屏
function toastError(text: string) {
  const t = String(text || '').trim();
  if (!t) return;
  const now = Date.now();
  if (t === lastErrText && now - lastErrAt < 30000) return;
  lastErrText = t;
  lastErrAt = now;
  toast(t, false);
}

function renderState(s) {
  const dot = $('dot');
  const text = $('stateText');
  const sub = $('stateSub');
  dot.className = 'dot ' + (s.state || '');
  if (s.state === 'online') { text.textContent = '已连接'; sub.textContent = '网络认证正常'; }
  else if (s.state === 'offline') { text.textContent = '未认证'; sub.textContent = '需要登录校园网'; }
  else { text.textContent = s.ssid ? '状态未知' : '未连接 Wi-Fi'; sub.textContent = s.ssid ? '正在检测网络状态' : '请先连接校园网 Wi-Fi'; }

  // 状态卡只表达状态，错误与详情交给弹窗，不占用卡片空间
  const detail = $('stateDetail');
  if (detail) { detail.textContent = ''; detail.style.display = 'none'; }
  if (s.state !== 'online' && s.state !== 'offline') toastError(s.detail || s.message);

  $('ssid').textContent = s.ssid || '—';
  $('ipv4').textContent = s.ipv4 || '—';
  $('gateway').textContent = s.gateway || '—';
  $('gwmac').textContent = s.gateway_mac || '（未取到）';
  $('mac').textContent = (s.mac || '—') + (s.random_mac ? '（随机）' : '');
  $('cred').innerHTML =
    '<span class="pill ' + (s.has_cred ? 'ok' : 'bad') + '">' + (s.has_cred ? '账号已保存' : '账号未保存') + '</span>' +
    '<span class="pill ' + (s.has_trust ? 'ok' : '') + '">' + (s.has_trust ? '网关已信任' : '网关未记录') + '</span>';
  $('credHint').style.display = s.has_cred ? 'none' : 'flex';
}

function goAccount() {
  $q('.nav-item[data-page="account"]').click();
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
  const el = $(id);
  if (el) el.textContent = r.v;
  const unit = $(id + 'U');
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
    const st = $('rateStats');
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
  const cv = $('rateChart');
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

// fmtClock 把 Unix 秒格式化成 HH:mm，用于展示定时恢复时间。
function fmtClock(unixSec) {
  const d = new Date(Number(unixSec) * 1000);
  const p = (n) => (n < 10 ? '0' : '') + n;
  return p(d.getHours()) + ':' + p(d.getMinutes());
}

async function loadDaemon() {
  if (!App()) return;
  try {
    const d = await App().Daemon();
    const running = !!d.running;
    const reason = String(d.reason || '');
    const retryAt = Number(d.retry_at || 0);
    const dot = $('dDot');
    dot.className = 'dot ' + (running ? (d.state === 'online' ? 'online' : '') : 'offline');
    $('dTitle').textContent = running ? '后台守护运行中' : '后台守护未运行';
    let sub = running ? '已自动维持在线，关闭窗口不影响' : '关闭窗口后不会自动重连';
    if (running && reason === 'time_policy') {
      // 限时策略：固定静默，期间守护只探测不登录
      sub = retryAt
        ? '当前不在上网时段，' + fmtClock(retryAt) + ' 后自动重试'
        : '当前不在上网时段，已暂停自动登录';
    } else if (running && (reason === 'credential' || reason === 'locked')) {
      sub = reason === 'credential' ? '账号或密码被拒，自动重试已暂停' : '账号被网关锁定，自动重试已暂停';
    }
    $('dSub').textContent = sub;

    // 需要人工处理时给出直达账号页的提示条
    const hint = $('authHint');
    if (hint) {
      const needFix = running && (reason === 'credential' || reason === 'locked');
      hint.style.display = needFix ? 'flex' : 'none';
      if (needFix) {
        $('authHintText').textContent = String(d.message || '请检查账号密码后重新保存');
      }
    }

    // 守护出错时用弹窗提示，不把长错误塞进卡片；已暂停原因改由提示条展示
    if (d.message && !reason && String(d.message) !== sub) toastError(String(d.message));
    $('dSwitch').classList.toggle('on', !!(await App().DaemonEnabled()));
  } catch (e) { }
}

async function toggleDaemon() {
  if (!App()) return;
  const sw = $('dSwitch');
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
// 头像上的单字标记，用运营商区分线路
const CARRIER_SHORT = { cmcc: '移', ctcc: '电', cucc: '联', campus: '校' };
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
    const box = $('accList' + (campus === 'dongqu' ? 'Dongqu' : 'Benbu'));
    if (!box) return;
    box.innerHTML = '';
    const list = accountsCache.filter(a => a.campus === campus);
    if (!list.length) {
      const empty = document.createElement('div');
      empty.className = 'acc-empty';
      empty.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round"><circle cx="12" cy="8" r="3.4"/><path d="M5 20c0-3.4 3.1-5.2 7-5.2s7 1.8 7 5.2"/></svg>' +
        '<span>还没有' + CAMPUS_NAMES[campus] + '账号</span>';
      box.appendChild(empty);
      return;
    }
    list.forEach(a => {
      const row = document.createElement('div');
      row.className = 'acc-row' + (a.username === selectedUser ? ' active' : '');
      // 运营商色块头像：扫一眼就能区分线路
      const avatar = document.createElement('div');
      avatar.className = 'acc-avatar ' + (a.carrier || 'campus');
      avatar.textContent = CARRIER_SHORT[a.carrier] || '校';
      const main = document.createElement('div');
      main.className = 'acc-row-main';
      const u = document.createElement('div');
      u.className = 'acc-row-user';
      u.textContent = a.username;
      const c = document.createElement('div');
      c.className = 'acc-row-carrier';
      c.textContent = carrierName(a.carrier) + ' · ' + (CAMPUS_NAMES[a.campus] || '东区');
      main.appendChild(u);
      main.appendChild(c);
      row.appendChild(avatar);
      row.appendChild(main);
      if (a.current) {
        const st = document.createElement('div');
        st.className = 'acc-row-state';
        st.innerHTML = '<span class="acc-row-dot"></span>使用中';
        row.appendChild(st);
      }
      row.onclick = () => selectAccount(a.username);
      box.appendChild(row);
    });
  });
}

function renderEditor() {
  const a = accountsCache.find(x => x.username === selectedUser);
  const label = $('editorLabel');
  const title = $('editorTitle');
  const del = $('delBtn');
  const campusSel = $('editorCampus');
  if (a) {
    const cname = CAMPUS_NAMES[a.campus] || '东区';
    label.textContent = cname + '账号' + (a.current ? ' · 使用中' : '');
    title.textContent = a.username + ' · ' + carrierName(a.carrier);
    del.style.display = 'inline-flex';
    $('user').value = a.username;
    $('carrier').value = a.carrier || 'cmcc';
    campusSel.value = a.campus || 'dongqu';
  } else {
    if (!campusSel.value) campusSel.value = 'dongqu';
    label.textContent = '新增' + (CAMPUS_NAMES[campusSel.value] || '东区') + '账号';
    title.textContent = '填写学号、密码与运营商';
    del.style.display = 'none';
    $('user').value = '';
    $('carrier').value = 'cmcc';
  }
  $('pass').value = '';
}

function newAccount(campus) {
  selectedUser = '';
  $('editorCampus').value = campus || 'dongqu';
  renderEditor();
  $('user').focus();
}

// 校区下拉变更时只刷新标题，不动已填内容。
function updateCampusLabel() {
  const campus = $('editorCampus').value || 'dongqu';
  $('editorLabel').textContent = '新增' + (CAMPUS_NAMES[campus] || '东区') + '账号';
}

async function selectAccount(username) {
  const el = $('credMsg');
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
  const el = $('credMsg');
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
  const seg = $('campusSeg');
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
    const set = c.prefer || 'dongqu';
    const setName = CAMPUS_NAMES[set] || '东区';

    // 滑条只反映用户设定，绝不被实际连接覆盖——设定是唯一权威。
    markCampusSide(set);

    const setEl = $('campusSet');
    if (setEl) setEl.textContent = '设定 · ' + setName;

    const curEl = $('campusCur');
    const note = $('campusNote');
    let conflict = false;
    if (c.ssid) {
      conflict = c.connected !== set;
    }
    if (curEl) {
      curEl.textContent = c.ssid ? '当前 · ' + c.ssid : '当前 · 未连接';
      curEl.classList.toggle('warn', !c.ssid || conflict);
    }
    if (note) {
      if (conflict) {
        note.textContent = c.auto
          ? '已开启自动连接，正在切回' + setName + '校园网 ' + (CAMPUS_SSID[set] || '')
          : '当前不在设定校区，未自动切换；可在设置中开启自动连接';
        note.className = 'campus-note warn';
      } else if (!c.ssid) {
        note.textContent = c.auto ? '未连 Wi-Fi，将自动连回' + setName : '未连 Wi-Fi';
        note.className = 'campus-note';
      } else {
        note.textContent = '';
        note.className = 'campus-note';
      }
    }
    // 设置页「自动连接校园 Wi-Fi」文案跟随设定校区。
    const wd = $('wifiDesc');
    if (wd) {
      wd.textContent = '守护发现没连校园网时，自动连回' + setName +
        '校园网 ' + (CAMPUS_SSID[set] || '') + '。';
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
  const el = $(campus === 'dongqu' ? 'foldDongqu' : 'foldBenbu');
  if (el) el.classList.toggle('open');
}

// 头部计数徽标：N 个 · 使用中
function renderFoldCounts() {
  const set = (id, campus) => {
    const el = $(id);
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
  await loadUpdateDirect();
  await loadCampus();
}

function setLogMsg(text: string, ok?: boolean) {
  // 导出 / 打开目录的反馈统一走弹窗
  if (ok === false) toastError(text);
  else toast(text, ok === true);
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

// 日志行是后端输出的 JSON，这里解析成结构化事件；解析失败则按原文显示
type LogEntry = { time: string; level: string; msg: string; extra: string; raw: string };
let logEntries: LogEntry[] = [];
let logLevelFilter = 'all';

function escHtml(s: string): string {
  return String(s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' } as any)[c]);
}

function parseLogLine(line: string): LogEntry {
  const raw = line;
  try {
    const o = JSON.parse(line);
    const m = String(o.time || '').match(/T(\d{2}:\d{2}:\d{2})/);
    const level = String(o.level || 'INFO').toUpperCase();
    const msg = String(o.msg || '');
    const extras: string[] = [];
    for (const k of Object.keys(o)) {
      if (k === 'time' || k === 'level' || k === 'msg') continue;
      const v = o[k];
      if (v === '' || v === null || v === undefined) continue;
      extras.push(k + ' ' + String(v));
    }
    return { time: m ? m[1] : '', level, msg, extra: extras.join(' · '), raw };
  } catch (e) {
    return { time: '', level: '', msg: raw, extra: '', raw };
  }
}

function levelClass(level: string): string {
  const l = level.toLowerCase();
  if (l === 'warn' || l === 'warning') return 'warn';
  if (l === 'error' || l === 'fatal') return 'error';
  if (l === 'debug') return 'debug';
  return 'info';
}

function renderLogs() {
  const box = $('logView');
  if (!box) return;
  const search = $('logSearch');
  const kw = String(search ? search.value : '').trim().toLowerCase();
  const rows = logEntries.filter(e => {
    if (logLevelFilter === 'warn' && e.level !== 'WARN' && e.level !== 'ERROR') return false;
    if (logLevelFilter === 'error' && e.level !== 'ERROR') return false;
    return !kw || e.raw.toLowerCase().includes(kw);
  });
  if (!rows.length) {
    box.innerHTML = '<div class="log-empty">' + (logEntries.length ? '没有匹配的日志' : '暂无日志') + '</div>';
    return;
  }
  const nearBottom = box.scrollHeight - box.scrollTop - box.clientHeight < 24;
  const keepTop = box.scrollTop;
  box.innerHTML = rows.map(e => {
    const extra = e.extra ? '<span class="log-extra"> · ' + escHtml(e.extra) + '</span>' : '';
    return '<div class="log-row ' + levelClass(e.level) + '">' +
      '<span class="log-time">' + escHtml(e.time || '--:--:--') + '</span>' +
      '<span class="log-lv">' + (e.level ? escHtml(e.level) : '—') + '</span>' +
      '<span class="log-msg">' + escHtml(e.msg) + extra + '</span>' +
      '</div>';
  }).join('');
  const follow = $('logFollow');
  if ((!follow || follow.checked) && nearBottom) box.scrollTop = box.scrollHeight;
  else box.scrollTop = keepTop;
}

// 内联 oninput / onclick 需要显式暴露
(window as any).renderLogs = renderLogs;

async function loadLogs() {
  if (!App()) return;
  try {
    const lines = await App().Logs();
    logEntries = (lines || []).map(parseLogLine);
    const meta = $('logMeta');
    if (meta) meta.textContent = '最近 ' + logEntries.length + ' 条 · 每 5 秒刷新';
    renderLogs();
  } catch (e) { }
}

// 级别筛选
$$('#logLevel .seg-item').forEach((el: any) => {
  el.onclick = () => {
    $$('#logLevel .seg-item').forEach((n: any) => n.classList.remove('on'));
    el.classList.add('on');
    logLevelFilter = el.dataset.level || 'all';
    renderLogs();
  };
});

async function doLogin() {
  if (!App()) return;
  const btn = $('loginBtn');
  btn.disabled = true;
  setMsg('正在认证…');
  try {
    const msg = await App().Login();
    // 本部验证码自动识别连续失败：转为人工输入
    if (msg === '需要手动输入验证码') {
      const img = await App().CaptchaChallenge();
      if (img) {
        openCaptcha(img);
        btn.disabled = false;
        return;
      }
    }
    setMsg(msg, msg.indexOf('成功') >= 0);
  } catch (e) {
    setMsg(String(e), false);
  } finally {
    btn.disabled = false;
    await refresh();
  }
}

/* ---------- 人工输入验证码 ---------- */
function openCaptcha(img) {
  const modal = $('captchaModal');
  const el = $('captchaImg') as HTMLImageElement;
  if (!modal || !el) return;
  el.src = img;
  ($('captchaInput') as HTMLInputElement).value = '';
  modal.hidden = false;
  ($('captchaInput') as HTMLInputElement).focus();
}

function closeCaptcha() {
  const modal = $('captchaModal');
  if (modal) modal.hidden = true;
}

async function submitCaptcha() {
  if (!App()) return;
  const code = ($('captchaInput') as HTMLInputElement).value.trim();
  if (code.length !== 4) {
    toast('请输入 4 位验证码', false);
    return;
  }
  try {
    const msg = await App().SubmitCaptcha(code);
    if (msg.indexOf('成功') >= 0) {
      closeCaptcha();
      toast(msg, true);
      await refresh();
    } else {
      toast(msg, false);
    }
  } catch (e) {
    toast(String(e), false);
  }
}

(window as any).openCaptcha = openCaptcha;
(window as any).closeCaptcha = closeCaptcha;
(window as any).submitCaptcha = submitCaptcha;

async function saveCred() {
  if (!App()) return;
  const el = $('credMsg');
  const username = $('user').value.trim();
  const password = $('pass').value;
  const carrier = $('carrier').value;
  const campus = $('editorCampus').value || 'dongqu';
  if (!username || !password) {
    el.textContent = '账号和密码不能为空';
    el.className = 'msg bad';
    return;
  }
  try {
    const msg = await App().SaveCred(username, password, carrier, campus);
    el.textContent = msg;
    el.className = 'msg ok';
    $('pass').value = '';
    selectedUser = username;
    // 确保目标校区面板展开，保存后能立刻看到新账号。
    const fold = $(campus === 'dongqu' ? 'foldDongqu' : 'foldBenbu');
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
    $('autoSwitch').classList.toggle('on', !!s.enabled);
  } catch (e) { }
}

async function loadWifiAuto() {
  if (!App()) return;
  try {
    const on = !!(await App().WifiAutoEnabled());
    $('wifiSwitch').classList.toggle('on', on);
  } catch (e) { }
}

async function toggleWifiAuto() {
  if (!App()) return;
  const sw = $('wifiSwitch');
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

async function loadUpdateDirect() {
  if (!App() || !App().UpdateDirect) return;
  try {
    $('directSwitch').classList.toggle('on', !!(await App().UpdateDirect()));
  } catch (e) { }
}

async function toggleUpdateDirect() {
  if (!App() || !App().SetUpdateDirect) return;
  const sw = $('directSwitch');
  const next = !sw.classList.contains('on');
  sw.classList.toggle('on', next);
  try {
    const msg = await App().SetUpdateDirect(next);
    await loadUpdateDirect();
    toast(msg || (next ? '已开启更新直连' : '已关闭更新直连'), true);
  } catch (e) {
    sw.classList.toggle('on', !next);
    toast((e && e.message) ? e.message : String(e), false);
  }
}

async function toggleAutostart() {
  if (!App()) return;
  const sw = $('autoSwitch');
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

/* ---------- 更新说明 Markdown 渲染 ---------- */
// Release 正文是 Markdown，直接放进纯文本容器会把 # 和 ** 原样显示。
// 这里做最小渲染：标题、列表、加粗、行内代码、链接、分隔线、引用、表格。
function escHTML(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

function inlineMD(s: string): string {
  let out = escHTML(s);
  out = out.replace(/`([^`]+)`/g, '<code>$1</code>');
  out = out.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
  out = out.replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, '<a href="$2" target="_blank" rel="noreferrer">$1</a>');
  return out;
}

function renderMarkdown(md: string): string {
  const lines = md.replace(/\r\n?/g, '\n').split('\n');
  const out: string[] = [];
  let inList = false;
  const closeList = () => { if (inList) { out.push('</ul>'); inList = false; } };

  for (let i = 0; i < lines.length; i++) {
    const t = lines[i].trim();
    if (!t) { closeList(); continue; }

    // 表格：表头行 + 分隔行 + 数据行
    if (t.startsWith('|') && t.endsWith('|') && i + 1 < lines.length) {
      const sep = lines[i + 1].trim();
      if (/^\|[\s:|-]+\|$/.test(sep)) {
        closeList();
        const cells = (row: string) => row.trim().slice(1, -1).split('|').map(c => c.trim());
        const head = cells(t);
        const rows: string[][] = [];
        i += 2;
        while (i < lines.length && lines[i].trim().startsWith('|') && lines[i].trim().endsWith('|')) {
          rows.push(cells(lines[i]));
          i++;
        }
        i--;
        out.push(
          '<div class="md-table"><table><thead><tr>' +
          head.map(c => '<th>' + inlineMD(c) + '</th>').join('') +
          '</tr></thead><tbody>' +
          rows.map(r => '<tr>' + r.map(c => '<td>' + inlineMD(c) + '</td>').join('') + '</tr>').join('') +
          '</tbody></table></div>',
        );
        continue;
      }
    }

    const h = /^(#{1,4})\s+(.*)$/.exec(t);
    if (h) {
      closeList();
      const lv = Math.min(h[1].length, 3);
      out.push('<h' + lv + '>' + inlineMD(h[2]) + '</h' + lv + '>');
      continue;
    }
    if (/^(-{3,}|\*{3,}|_{3,})$/.test(t)) { closeList(); out.push('<hr>'); continue; }

    const item = /^[-*+]\s+(.*)$/.exec(t);
    if (item) {
      if (!inList) { out.push('<ul>'); inList = true; }
      out.push('<li>' + inlineMD(item[1]) + '</li>');
      continue;
    }
    if (t.startsWith('>')) {
      closeList();
      out.push('<blockquote>' + inlineMD(t.replace(/^>\s?/, '')) + '</blockquote>');
      continue;
    }

    closeList();
    out.push('<p>' + inlineMD(t) + '</p>');
  }
  closeList();
  return out.join('');
}

/* ---------- GitHub Star 数 ---------- */
// 校园网未认证时没有外网，请求会失败；这里静默处理，按钮本身仍可点击跳转。
async function loadStarCount() {
  const el = $('starCount');
  if (!el) return;
  try {
    const ctrl = new AbortController();
    const timer = setTimeout(() => ctrl.abort(), 4000);
    const resp = await fetch('https://api.github.com/repos/love486love/easynet', { signal: ctrl.signal });
    clearTimeout(timer);
    if (!resp.ok) return;
    const info = await resp.json();
    const n = Number(info && info.stargazers_count);
    if (!Number.isFinite(n) || n <= 0) return;
    el.textContent = n >= 1000 ? (n / 1000).toFixed(1) + 'k' : String(n);
    el.hidden = false;
  } catch {
    // 离线、限流或请求被拦截：不显示数字，不打扰用户
  }
}

let updateInfo = null;

async function checkUpdate(silent) {
  if (!App()) return;
  const badge = $('verBadge');
  const msg = $('updateMsg');
  const pill = $('updatePill');
  const st = $('updateState');
  const nums = $('updateNums');
  const notes = $('updateNotes');
  const btn = $('updateBtn');

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
        notes.innerHTML = renderMarkdown(u.body);
      } else {
        notes.hidden = true;
        notes.innerHTML = '';
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
  const btn = $('updateBtn');
  const wrap = $('updateProgress');
  const fill = $('updateFill');
  const pctEl = $('updatePct');
  const speedEl = $('updateSpeed');
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
    const wrap = $('updateProgress');
    const fill = $('updateFill');
    const pctEl = $('updatePct');
    const speedEl = $('updateSpeed');
    const btn = $('updateBtn');
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
  $q('.nav-item[data-page="about"]').click();
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
  const box = $('netWindow');
  if (!box) return;
  const now = new Date();
  const w = nwWindow(now);
  box.classList.toggle('offline', w.offline);
  $('nwIcon').innerHTML = w.offline ? NW_MOON : NW_SUN;
  $('nwTitle').textContent = w.offline ? '断网中' : '可上网';
  const total = w.end.getTime() - w.start.getTime();
  const pct = total > 0 ? ((now.getTime() - w.start.getTime()) / total) * 100 : 0;
  $('nwFill').style.width = Math.max(0, Math.min(100, pct)).toFixed(1) + '%';
  $('nwRemain').textContent = w.offline
    ? nwFmt(w.end.getTime() - now.getTime()) + '后恢复'
    : '距断网 ' + nwFmt(w.end.getTime() - now.getTime());
  $('nwEnd').textContent = w.offline
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
  $('tutOverlay').hidden = false;
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
    const nav = $q('.nav-item[data-page="' + s.page + '"]');
    if (nav) nav.click();
  }

  $('tutStep').textContent = 'STEP ' + (i + 1) + ' / ' + steps.length;
  $('tutTitle').textContent = s.title;
  $('tutText').textContent = s.text;
  $('tutPrev').style.visibility = i === 0 ? 'hidden' : 'visible';
  $('tutNext').textContent = i === steps.length - 1 ? '开始使用' : '下一步';

  const dots = $('tutDots');
  dots.innerHTML = '';
  steps.forEach((_, idx) => {
    const d = document.createElement('span');
    d.className = 'tut-dot' + (idx === i ? ' on' : '');
    dots.appendChild(d);
  });

  const spot = $('tutSpot');
  const dim = $('tutDim');
  const card = $('tutCard');
  const el = s.target ? $q(s.target) : null;

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
  $('tutOverlay').hidden = true;
}

async function maybeStartTutorial() {
  if (!App() || !App().TutorialDone) return;
  try {
    if (await App().TutorialDone()) return;
  } catch (e) { }
  setTimeout(startTutorial, 900);
}

document.addEventListener('keydown', function (e) {
  if (e.key === 'Escape' && !$('tutOverlay').hidden) endTutorial();
});

refresh();
loadAutostart();
setInterval(loadLogs, 5000);
setInterval(loadDaemon, 5000);
setInterval(loadNetRate, 1000);
maybeStartTutorial();
loadStarCount();

setTimeout(function () {
  const badge = $('verBadge');
  const aboutVer = $q('.about-ver');
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

// 迁移到 ES module 后，index.html 的内联 onclick 需要显式挂到 window。
Object.assign(window, { winMin, winMax, winClose, openRepo, openRelease, resolveTheme, updateThemeSeg, applyTheme, toggleTheme, setMsg, toast, renderState, goAccount, fmtRate, fmtBytes, setRateNum, loadNetRate, drawRateChart, rgba, loadDaemon, toggleDaemon, carrierName, loadAccounts, renderAccLists, renderEditor, newAccount, updateCampusLabel, selectAccount, deleteCurrent, markCampusSide, loadCampus, setCampus, toggleFold, renderFoldCounts, refresh, setLogMsg, exportLogs, openLogsDir, loadLogs, doLogin, saveCred, loadAutostart, loadWifiAuto, toggleWifiAuto, loadUpdateDirect, toggleUpdateDirect, toggleAutostart, checkUpdate, doUpdate, onVersionClick, nwAt, nwWindow, nwFmt, nwClock, updateNetWindow, startTutorial, showTutorialStep, nextTutorial, prevTutorial, endTutorial, maybeStartTutorial })

/* ---------- 鼠标光晕 ---------- */
// 光晕挂在一个只覆盖内容区的固定容器里：容器负责裁切与边缘渐隐，
// 光晕自身保持正圆，因此边界不会出现直角。
function initCursorGlow() {
  const area = document.createElement('div');
  area.className = 'glow-area';
  const glow = document.createElement('div');
  glow.className = 'cursor-glow';
  area.appendChild(glow);
  document.body.appendChild(area);

  const R = 230; // 与 .cursor-glow 的 460px 尺寸对应
  let rect = area.getBoundingClientRect();
  let raf = 0;
  let x = 0;
  let y = 0;
  let shown = false;

  const place = () => {
    glow.style.transform =
      'translate3d(' + (x - rect.left - R) + 'px,' + (y - rect.top - R) + 'px,0)';
  };

  window.addEventListener('mousemove', (e: MouseEvent) => {
    x = e.clientX;
    y = e.clientY;
    if (!shown) {
      shown = true;
      place();
      area.classList.add('on');
      return;
    }
    if (!raf) {
      raf = requestAnimationFrame(() => {
        raf = 0;
        place();
      });
    }
  }, { passive: true });

  window.addEventListener('resize', () => {
    rect = area.getBoundingClientRect();
  });

  const hide = () => {
    shown = false;
    area.classList.remove('on');
  };
  window.addEventListener('mouseleave', hide);
  window.addEventListener('blur', hide);
}

initCursorGlow();



