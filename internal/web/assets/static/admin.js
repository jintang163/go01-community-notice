/* 管理后台脚本 */
(async function () {
  const me = await requireRole('admin');
  if (!me) return;
  bindLogout();
  document.getElementById('me-name').textContent = me.display_name || me.username;

  const tbody = document.getElementById('notice-tbody');
  const utbody = document.getElementById('user-tbody');
  const modal = document.getElementById('notice-modal');
  const umodal = document.getElementById('user-modal');

  /* ---- 通知列表 ---- */
  async function loadNotices() {
    const params = {};
    const q = document.getElementById('f-q').value.trim();
    const st = document.getElementById('f-status').value;
    const cat = document.getElementById('f-category').value.trim();
    if (q) params.q = q;
    if (st) params.status = st;
    if (cat) params.category = cat;
    let list;
    try { list = await api.listNotices(params); } catch (e) { toast(e.message); return; }
    tbody.innerHTML = '';
    if (!list || list.length === 0) {
      tbody.appendChild(el('tr', {}, el('td', { colspan: '7', text: '暂无通知' })));
      return;
    }
    for (const n of list) {
      const statusTag = el('span', { class: 'tag tag-' + n.status, text: n.status === 'draft' ? '草稿' : '已发布' });
      const actions = el('div', { class: 'row-actions' });
      actions.style.display = 'flex';
      actions.style.gap = '6px';
      actions.appendChild(el('button', { class: 'btn', text: '编辑', onclick: () => editNotice(n) }));
      actions.appendChild(el('button', { class: 'btn', text: n.status === 'published' ? '下架' : '发布', onclick: () => togglePublish(n) }));
      actions.appendChild(el('button', { class: 'btn', text: n.pinned ? '取消置顶' : '置顶', onclick: () => togglePin(n) }));
      actions.appendChild(el('button', { class: 'btn btn-danger', text: '删除', onclick: () => delNotice(n) }));
      tbody.appendChild(el('tr', {},
        el('td', {}, el('a', { text: n.title, onclick: () => openStats(n) })),
        el('td', {}, statusTag),
        el('td', { text: n.category || '—' }),
        el('td', { text: String(n.priority) }),
        el('td', {}, n.pinned ? el('span', { class: 'tag tag-pin', text: '置顶' }) : '—'),
        el('td', { text: fmtTime(n.publish_at) }),
        el('td', {}, actions),
      ));
    }
  }

  function openStats(n) {
    location.href = '/notices/' + n.id;
  }

  async function togglePublish(n) {
    try {
      if (n.status === 'published') await api.unpublishNotice(n.id);
      else await api.publishNotice(n.id);
      toast('操作成功');
      loadNotices(); loadStats();
    } catch (e) { toast(e.message); }
  }
  async function togglePin(n) {
    try { await api.pinNotice(n.id); toast('操作成功'); loadNotices(); } catch (e) { toast(e.message); }
  }
  async function delNotice(n) {
    if (!confirm('确定删除通知「' + n.title + '」？此操作将级联清理其阅读记录。')) return;
    try { await api.deleteNotice(n.id); toast('已删除'); loadNotices(); loadStats(); } catch (e) { toast(e.message); }
  }

  /* ---- 编辑弹层 ---- */
  function openModal(m) { m.hidden = false; }
  function closeModal(m) { m.hidden = true; }
  function editNotice(n) {
    document.getElementById('n-id').value = n.id;
    document.getElementById('n-title').value = n.title;
    document.getElementById('n-content').value = n.content || '';
    document.getElementById('n-priority').value = n.priority;
    document.getElementById('n-category').value = n.category || '';
    document.getElementById('n-pinned').checked = !!n.pinned;
    document.getElementById('notice-modal-title').textContent = '编辑通知';
    openModal(modal);
  }
  function newNotice() {
    document.getElementById('n-id').value = '';
    document.getElementById('n-title').value = '';
    document.getElementById('n-content').value = '';
    document.getElementById('n-priority').value = '50';
    document.getElementById('n-category').value = '';
    document.getElementById('n-pinned').checked = false;
    document.getElementById('notice-modal-title').textContent = '新建通知';
    openModal(modal);
  }
  document.getElementById('new-notice').addEventListener('click', newNotice);
  document.getElementById('notice-cancel').addEventListener('click', () => closeModal(modal));
  document.getElementById('notice-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const id = document.getElementById('n-id').value;
    const body = {
      title: document.getElementById('n-title').value.trim(),
      content: document.getElementById('n-content').value.trim(),
      priority: parseInt(document.getElementById('n-priority').value, 10) || 0,
      category: document.getElementById('n-category').value.trim(),
      pinned: document.getElementById('n-pinned').checked,
    };
    try {
      if (id) await api.updateNotice(id, body);
      else await api.createNotice(Object.assign({ status: 'draft' }, body));
      toast('保存成功');
      closeModal(modal);
      loadNotices(); loadStats();
    } catch (e) { toast(e.message); }
  });

  /* ---- 用户管理 ---- */
  async function loadUsers() {
    let users;
    try { users = await api.listUsers(); } catch (e) { toast(e.message); return; }
    utbody.innerHTML = '';
    for (const u of users) {
      const isMe = u.id === me.id;
      utbody.appendChild(el('tr', {},
        el('td', { text: u.username }),
        el('td', { text: u.display_name || '—' }),
        el('td', { text: u.role === 'admin' ? '管理员' : '居民' }),
        el('td', { text: fmtTime(u.created_at) }),
        el('td', {}, isMe ? '' : el('button', { class: 'btn btn-danger', text: '删除', onclick: () => delUser(u) })),
      ));
    }
  }
  async function delUser(u) {
    if (!confirm('确定删除用户「' + u.username + '」？其阅读记录将被清理。')) return;
    try { await api.deleteUser(u.id); toast('已删除'); loadUsers(); loadStats(); } catch (e) { toast(e.message); }
  }
  document.getElementById('new-user').addEventListener('click', () => {
    document.getElementById('u-username').value = '';
    document.getElementById('u-displayname').value = '';
    document.getElementById('u-password').value = '';
    openModal(umodal);
  });
  document.getElementById('user-cancel').addEventListener('click', () => closeModal(umodal));
  document.getElementById('user-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const body = {
      username: document.getElementById('u-username').value.trim(),
      display_name: document.getElementById('u-displayname').value.trim(),
      password: document.getElementById('u-password').value,
      role: 'resident',
    };
    try { await api.createUser(body); toast('已创建居民'); closeModal(umodal); loadUsers(); loadStats(); }
    catch (e) { toast(e.message); }
  });

  /* ---- 统计 ---- */
  async function loadStats() {
    let s;
    try { s = await api.globalStats(); } catch (e) { toast(e.message); return; }
    const grid = document.getElementById('stats');
    grid.innerHTML = '';
    const items = [
      ['通知总数', s.notice_total],
      ['已发布', s.notice_published],
      ['草稿', s.notice_draft],
      ['置顶', s.notice_pinned],
      ['居民数', s.user_resident],
      ['管理员', s.user_admin],
      ['阅读总数', s.read_total],
      ['今日阅读', s.read_today],
    ];
    for (const [k, v] of items) {
      grid.appendChild(el('div', { class: 'stat' }, el('div', { class: 'k', text: k }), el('div', { class: 'v', text: String(v) })));
    }
  }

  document.getElementById('f-search').addEventListener('click', loadNotices);
  loadStats(); loadNotices(); loadUsers();
})();
