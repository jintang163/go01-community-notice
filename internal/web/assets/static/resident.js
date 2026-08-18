/* 居民首页脚本 */
(async function () {
  const me = await requireRole('resident');
  if (!me) return;
  bindLogout();
  document.getElementById('me-name').textContent = me.display_name || me.username;

  async function loadUnread() {
    try {
      const c = await api.unreadCount();
      const badge = document.getElementById('unread-badge');
      if (c.unread > 0) { badge.textContent = c.unread + ' 未读'; badge.hidden = false; }
      else { badge.hidden = true; }
    } catch (e) {}
  }

  async function loadNotices() {
    const params = {};
    const q = document.getElementById('f-q').value.trim();
    const cat = document.getElementById('f-category').value.trim();
    if (q) params.q = q;
    if (cat) params.category = cat;
    const list = await api.myNotices(params);
    const box = document.getElementById('notice-list');
    box.innerHTML = '';
    if (!list || list.length === 0) {
      box.appendChild(el('p', { text: '暂无通知', style: 'color:var(--muted)' }));
      return;
    }
    for (const n of list) {
      const item = el('div', { class: 'notice-item' + (n.read ? '' : ' unread') });
      item.appendChild(el('span', { class: 'dot' + (n.read ? ' read' : '') }));
      const main = el('div', { class: 'ni-main' });
      main.appendChild(el('div', { class: 'ni-title' }, el('a', { href: '/notices/' + n.id, text: (n.pinned ? '📌 ' : '') + n.title })));
      main.appendChild(el('div', { class: 'ni-meta', text: (n.category ? n.category + ' · ' : '') + fmtTime(n.publish_at) + ' · 优先级 ' + n.priority }));
      item.appendChild(main);
      if (!n.read) {
        item.appendChild(el('button', { class: 'btn', text: '标记已读', onclick: async () => {
          try { await api.markRead(n.id); toast('已标记已读'); loadNotices(); loadUnread(); } catch (e) { toast(e.message); }
        }}));
      }
      box.appendChild(item);
    }
  }

  document.getElementById('f-search').addEventListener('click', loadNotices);
  loadUnread(); loadNotices();
})();
