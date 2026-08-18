/* 通知详情页脚本 */
(async function () {
  const me = await requireRole(null);
  if (!me) return;
  bindLogout();

  const seg = location.pathname.split('/');
  const id = seg[seg.length - 1];
  if (!id) { location.href = me.role === 'admin' ? '/admin' : '/resident'; return; }
  document.getElementById('back-link').href = me.role === 'admin' ? '/admin' : '/resident';

  const box = document.getElementById('notice-detail');
  try {
    const n = await api.getNotice(id);
    box.innerHTML = '';
    box.appendChild(el('h1', { text: (n.pinned ? '📌 ' : '') + n.title }));
    const metaParts = [];
    if (n.category) metaParts.push('分类：' + n.category);
    metaParts.push('优先级：' + n.priority);
    if (n.author_name) metaParts.push('发布人：' + n.author_name);
    metaParts.push('发布时间：' + fmtTime(n.publish_at));
    if (n.updated_at && (!n.publish_at || new Date(n.updated_at).getTime() !== new Date(n.publish_at).getTime())) {
      metaParts.push('更新时间：' + fmtTime(n.updated_at));
    }
    box.appendChild(el('div', { class: 'meta', text: metaParts.join('  ·  ') }));
    box.appendChild(el('div', { class: 'content', text: n.content }));

    // 管理员：展示阅读统计
    if (me.role === 'admin') {
      try {
        const s = await api.noticeStats(id);
        box.appendChild(el('div', { class: 'meta', style: 'margin-top:18px', text:
          '阅读统计：已读 ' + s.read_count + ' / ' + s.resident_total + ' 居民（' + (s.read_rate * 100).toFixed(1) + '%），未读 ' + s.unread_count }));
      } catch (e) {}
    }
  } catch (e) {
    box.innerHTML = '';
    box.appendChild(el('p', { class: 'error', text: e.message }));
  }
})();
