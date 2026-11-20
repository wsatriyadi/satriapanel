'use strict';
/* SatriaPanel — router & bootstrap */
let routeSeq = 0;
async function bootApp() {
  try { state.health = await api('/api/health'); } catch (e) {
    document.getElementById('root').innerHTML = `<div class="auth"><section class="auth-panel" style="grid-column:1/-1"><div class="auth-box" style="text-align:center"><div class="brand-mark" style="margin:0 auto 18px">${icon('shield')}</div><h2>Panel tidak merespons</h2><p class="sub">${esc(e.message)}</p><button class="btn primary" id="boot-retry">Muat ulang</button></div></section></div>`;
    document.getElementById('boot-retry')?.addEventListener('click',()=>location.reload());
    return;
  }
  state.timezone = state.health.timezone;
  if (!state.me && !state.health.needsSetup) {
    try { state.me = await api('/api/auth/me'); } catch (_) { state.me = null; }
  }
  if (state.health.needsSetup && !state.me) { renderAuth(true); return; }
  if (!state.me) { renderAuth(false); return; }
  if (!location.hash || location.hash === '#/login' || location.hash === '#/setup') location.hash = '#/dashboard';
  await routeView();
  startPolling();
}
async function routeView() {
  const seq = ++routeSeq;
  cleanupView();
  closeSidebar();
  if (!state.me) { renderAuth(false); return; }
  const raw = (location.hash || '#/dashboard').slice(1) || '/dashboard';
  const [pathPart] = raw.split('?');
  const parts = pathPart.split('/').filter(Boolean);
  const p = parts[0] || 'dashboard';
  const current = document.getElementById('content');
  if (current) {
    const fresh = document.createElement('main');
    fresh.className = 'content';
    fresh.id = 'content';
    fresh.innerHTML = loadingBlock();
    current.replaceWith(fresh);
  }
  await renderShell();
  if (seq !== routeSeq) return;
  const el = document.getElementById('content');
  try {
    if (p === 'dashboard') return await viewDashboard(el);
    if (p === 'apps') {
      if (parts[1] === 'new') return await viewAppForm(el, 0);
      if (parts[1] && parts[2] === 'edit') return await viewAppForm(el, Number(parts[1]));
      if (parts[1]) return await viewAppDetail(el, Number(parts[1]));
      return await viewApps(el);
    }
    if (p === 'deployments') {
      if (parts[1]) return openDeployPage(el, Number(parts[1]));
      return await viewDeployments(el);
    }
    if (p === 'databases') {
      if (parts[1]) return await viewDatabaseEditor(el, Number(parts[1]));
      return await viewDatabases(el);
    }
    if (p === 'redis') return await viewRedis(el);
    if (p === 'docker') return await viewDocker(el);
    if (p === 'cron') return await viewCron(el);
    if (p === 'files') return await viewFiles(el);
    if (p === 'terminal') return await viewTerminal(el);
    if (p === 'backup') return await viewBackup(el);
    if (p === 'cloudflare' || p === 'ports') return await viewCloudflare(el);
    if (p === 'notifications') return await viewNotifications(el);
    if (p === 'audit') return await viewAudit(el);
    if (p === 'settings') return await viewSettings(el);
    if (p === 'login') { location.hash = '#/dashboard'; return; }
    setPageTitle('Tidak ditemukan', 'Halaman tidak tersedia');
    el.innerHTML = pageHead('Halaman tidak ditemukan','Rute yang Anda buka tidak tersedia.') + `<div class="card">${emptyState('404 — Tidak ditemukan','Periksa URL atau kembali ke dashboard.','<a class="btn primary sm" href="#/dashboard">Kembali ke dashboard</a>','alert')}</div>`;
  } catch (e) {
    setPageTitle('Kesalahan');
    if (e.status === 401) { state.me = null; renderAuth(false); return; }
    el.innerHTML = errorBlock(e.message, `<button class="btn sm" id="view-retry">Coba lagi</button>`);
    document.getElementById('view-retry')?.addEventListener('click',()=>routeView());
  }
}
async function viewDeployments(el) {
  setPageTitle('Log Deploy','Riwayat seluruh deployment.');
  let d; try { d = await api('/api/deployments?limit=100'); } catch (e) { el.innerHTML = errorBlock(e.message); return; }
  const deps = d.deployments || [];
  el.innerHTML = `${pageHead('Log Deploy','Semua deployment lintas aplikasi. Klik untuk membuka log streaming.',`<button class="btn sm outline" id="dep-refresh">${icon('refresh')}Muat ulang</button>`)}
  <div class="grid g4 mb-16">
    ${statCard('Total deploy',deps.length,'100 terakhir','logs')}
    ${statCard('Berhasil',deps.filter(x=>x.status==='berhasil').length,'Health check lulus','check')}
    ${statCard('Gagal',deps.filter(x=>x.status==='gagal'||x.status==='gagal-terpulihkan').length,'Termasuk auto-rollback','alert','warn')}
    ${statCard('Sedang berjalan',deps.filter(x=>x.status==='berjalan'||x.status==='antre').length,'Antrean satu build','activity','info')}
  </div>
  <div class="card"><div class="card-head"><div><h3>${icon('logs')}Deployment</h3><div class="hint">Deploy serial · log tersimpan di data/logs/deploy.</div></div><div class="toolbar"><input class="input" id="dep-search" placeholder="Cari aplikasi…" style="width:210px"><select class="select" id="dep-status" style="width:auto"><option value="">Semua status</option><option>berhasil</option><option>gagal</option><option>berjalan</option><option>antre</option><option>gagal-terpulihkan</option><option>dibatalkan</option></select></div></div><div class="table-wrap" id="dep-table"></div></div>`;
  const table = $('#dep-table', el);
  function draw() {
    const q = ($('#dep-search',el)?.value||'').toLowerCase(), st = $('#dep-status',el)?.value;
    const list = deps.filter(x => (!q || (x.appName||'').toLowerCase().includes(q)) && (!st || x.status === st));
    table.innerHTML = list.length ? `<table><thead><tr><th>#</th><th>Aplikasi</th><th>Trigger</th><th>Status</th><th>Commit</th><th>Mulai</th><th>Durasi</th><th></th></tr></thead><tbody>${list.map(x=>`<tr class="clickable" data-dep="${x.id}"><td class="mono">${x.id}</td><td><a href="#/apps/${x.appId}">${esc(x.appName||'—')}</a></td><td>${esc(x.trigger)}</td><td>${badge(x.status)}${x.error?`<div class="row-sub" title="${attr(x.error)}">${esc(x.error.slice(0,70))}</div>`:''}</td><td class="mono small">${esc((x.commitHash||'—').slice(0,8))}</td><td>${fmtDate(x.createdAt)}</td><td>${x.durationSec?dur(x.durationSec):'—'}</td><td><div class="table-actions"><button class="btn-icon" data-open-dep="${x.id}" title="Log">${icon('terminal')}</button></div></td></tr>`).join('')}</tbody></table>` : emptyState('Tidak ada deployment','Deploy dari halaman aplikasi untuk memulai.','<a class="btn sm primary" href="#/apps">Buka aplikasi</a>','logs');
    table.querySelectorAll('[data-dep]').forEach(r=>r.onclick=()=>openDeployLog(r.dataset.dep));
    table.querySelectorAll('[data-open-dep]').forEach(b=>b.onclick=e=>{e.stopPropagation();openDeployLog(b.dataset.openDep);});
  }
  $('#dep-search',el).oninput=draw; $('#dep-status',el).onchange=draw; $('#dep-refresh',el).onclick=()=>viewDeployments(el);
  draw();
}
function openDeployPage(el, id) {
  setPageTitle('Log Deploy #'+id);
  el.innerHTML = pageHead('Log Deploy #'+id,'Streaming log deployment.', `<a class="btn sm ghost" href="#/deployments">${icon('arrowLeft')}Kembali</a>`)+`<div class="card"><div class="card-body" id="deploy-page-log">${loadingBlock('Membuka log…')}</div></div>`;
  const box = $('#deploy-page-log', el);
  const add = (lines) => { box.insertAdjacentHTML('beforeend', logHTML(lines)); box.scrollTop = box.scrollHeight; };
  api('/api/deployments/'+id).then(d => {
    const dep = d.deployment;
    box.innerHTML = `<div class="flex between mb-12"><div class="flex gap-8">${badge(dep.status)}<span class="mono small">${esc(dep.appName||'')}</span><span class="tiny muted">${esc(dep.trigger)}</span></div><span class="tiny muted">${fmtDate(dep.createdAt)}${dep.durationSec?' · '+dur(dep.durationSec):''}</span></div><div class="log-view" id="deploy-page-lines" style="max-height:calc(100vh - 300px)"></div>`;
    const lines = $('#deploy-page-lines', el);
    const append = arr => { const stick=lines.scrollTop+lines.clientHeight>=lines.scrollHeight-40; lines.insertAdjacentHTML('beforeend',logHTML(arr)); if(stick)lines.scrollTop=lines.scrollHeight; };
    const es = openSSE('/api/deployments/'+id+'/logs/stream',{history:p=>append(p.lines||[]),lines:p=>append(p.lines||[])});
    onCleanup(()=>{try{es.close();}catch(_){}});
  }).catch(e=>{box.innerHTML=errorBlock(e.message);});
}
function startPolling() {
  clearInterval(state.pollTimer);
  state.pollTimer = setInterval(async () => {
    if (!state.me) return;
    try {
      const d = await api('/api/notifications?unread=1&limit=1');
      if (typeof d.unread === 'number') { state.unread = d.unread; updateUnread(); }
    } catch (_) {}
  }, 30000);
}

window.addEventListener('hashchange', () => { if (state.me) routeView(); else bootApp(); });
window.addEventListener('DOMContentLoaded', () => { bootApp(); });
window.routeView = routeView;
window.bootApp = bootApp;
