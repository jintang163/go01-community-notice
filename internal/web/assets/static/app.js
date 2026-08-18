/* 社区通知公告系统 · 公共脚本 */
const App = {
  getToken() { return localStorage.getItem('cn_token') || ''; },
  setToken(t) { localStorage.setItem('cn_token', t); },
  clearToken() { localStorage.removeItem('cn_token'); localStorage.removeItem('cn_user'); },
  getUser() { try { return JSON.parse(localStorage.getItem('cn_user') || 'null'); } catch (e) { return null; } },
  setUser(u) { localStorage.setItem('cn_user', JSON.stringify(u)); },
};

const api = {
  async request(method, path, body) {
    const opts = { method, headers: {} };
    const token = App.getToken();
    if (token) opts.headers['Authorization'] = 'Bearer ' + token;
    if (body !== undefined) {
      opts.headers['Content-Type'] = 'application/json';
      opts.body = JSON.stringify(body);
    }
    let res;
    try {
      res = await fetch(path, opts);
    } catch (e) {
      throw new Error('网络错误：' + e.message);
    }
    if (res.status === 204) return null;
    let data = null;
    const ct = res.headers.get('content-type') || '';
    if (ct.includes('application/json')) {
      data = await res.json();
    } else {
      data = await res.text();
    }
    if (!res.ok) {
      const msg = (data && data.message) ? data.message : ('请求失败 (' + res.status + ')');
      const err = new Error(msg);
      err.code = data && data.code;
      err.status = res.status;
      throw err;
    }
    return data;
  },
  login(username, password) { return this.request('POST', '/api/auth/login', { username, password }); },
  logout() { return this.request('POST', '/api/auth/logout'); },
  me() { return this.request('GET', '/api/auth/me'); },
  listNotices(params) { const q = new URLSearchParams(params || {}).toString(); return this.request('GET', '/api/notices' + (q ? '?' + q : '')); },
  getNotice(id) { return this.request('GET', '/api/notices/' + id); },
  createNotice(b) { return this.request('POST', '/api/notices', b); },
  updateNotice(id, b) { return this.request('PUT', '/api/notices/' + id, b); },
  deleteNotice(id) { return this.request('DELETE', '/api/notices/' + id); },
  publishNotice(id) { return this.request('POST', '/api/notices/' + id + '/publish'); },
  unpublishNotice(id) { return this.request('POST', '/api/notices/' + id + '/unpublish'); },
  pinNotice(id) { return this.request('POST', '/api/notices/' + id + '/pin'); },
  myNotices(params) { const q = new URLSearchParams(params || {}).toString(); return this.request('GET', '/api/me/notices' + (q ? '?' + q : '')); },
  unreadCount() { return this.request('GET', '/api/me/unread-count'); },
  markRead(id) { return this.request('POST', '/api/notices/' + id + '/read'); },
  listUsers() { return this.request('GET', '/api/users'); },
  createUser(b) { return this.request('POST', '/api/users', b); },
  deleteUser(id) { return this.request('DELETE', '/api/users/' + id); },
  globalStats() { return this.request('GET', '/api/stats'); },
  noticeStats(id) { return this.request('GET', '/api/stats/notices/' + id); },
};

/* 工具函数 */
function fmtTime(s) {
  if (!s) return '—';
  const d = new Date(s);
  if (isNaN(d.getTime())) return s;
  const p = (n) => String(n).padStart(2, '0');
  return d.getFullYear() + '-' + p(d.getMonth() + 1) + '-' + p(d.getDate()) + ' ' + p(d.getHours()) + ':' + p(d.getMinutes());
}
function el(tag, attrs, ...children) {
  const node = document.createElement(tag);
  if (attrs) for (const [k, v] of Object.entries(attrs)) {
    if (k === 'class') node.className = v;
    else if (k === 'text') node.textContent = v;
    else if (k.startsWith('on') && typeof v === 'function') node.addEventListener(k.slice(2), v);
    else node.setAttribute(k, v);
  }
  for (const c of children) {
    if (c == null) continue;
    node.appendChild(typeof c === 'string' ? document.createTextNode(c) : c);
  }
  return node;
}
function toast(msg) {
  const t = document.getElementById('toast');
  if (!t) { alert(msg); return; }
  t.textContent = msg;
  t.hidden = false;
  clearTimeout(window.__toastTimer);
  window.__toastTimer = setTimeout(() => { t.hidden = true; }, 2200);
}

/* 通用登出 */
async function bindLogout() {
  const btn = document.getElementById('logout');
  if (btn) btn.addEventListener('click', async () => {
    try { await api.logout(); } catch (e) {}
    App.clearToken();
    location.href = '/login';
  });
}

/* 角色守卫：未登录或角色不符则跳转 */
async function requireRole(role) {
  const token = App.getToken();
  if (!token) { location.href = '/login'; return null; }
  try {
    const me = await api.me();
    if (role && me.role !== role) {
      location.href = me.role === 'admin' ? '/admin' : '/resident';
      return null;
    }
    return me;
  } catch (e) {
    App.clearToken();
    location.href = '/login';
    return null;
  }
}
