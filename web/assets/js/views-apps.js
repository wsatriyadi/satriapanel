'use strict';
/* ---------- Dashboard ---------- */
async function viewDashboard(el) {
  setPageTitle('Dashboard');
  viewLoading(el, 'Memuat ringkasan server…');
  let d;
  try { d = await api('/api/dashboard'); }
  catch (e) { el.innerHTML = errorBlock(e.message, `<button class="btn" id="apps-retry">Muat ulang</button>`); document.getElementById('apps-retry')?.addEventListener('click',()=>location.reload()); return; }
  state.dashboard = d; state.server = d.server;
  const s = d.stats || {}, sv = d.server || {};
  let series = (d.series || []).slice(-120);
  const apps = d.apps || [];
  const running = apps.filter(a => a.running || a.status === 'berjalan').length;
  const unhealthy = apps.filter(a => a.healthStatus === 'gagal' || a.status === 'bermasalah').length;
  const recent = d.deployments || [];
  const notes = d.notifications || [];
  el.innerHTML = `
  ${pageHead('Dashboard', `Ringkasan ${esc(state.me?.serverName || 'server')} · ${esc(sv.platform || sv.os || '')}`, `<button class="btn outline sm" id="dash-refresh">${icon('refresh')}Muat ulang</button><a class="btn primary sm" href="#/apps/new">${icon('plus')}Aplikasi baru</a>`)}
  <div class="grid g4 mb-16">
    ${statCard('Aplikasi', `${running}<span class="faint" style="font-size:15px;font-weight:500"> / ${apps.length}</span>`, `${unhealthy?`<span style="color:#FCA5A5">${unhealthy} perlu perhatian</span>`:'Semua sehat'}`, 'app', '')}
    ${statCard('CPU', `<span id="st-cpu">${pct(sv.cpuPercent)}</span>`, `${esc(sv.cpuCores||'—')} core · load ${Number(sv.load1||0).toFixed(2)}`, 'cpu', 'info')}
    ${statCard('RAM', `<span id="st-ram">${pct(sv.memPercent)}</span>`, `${fmtBytes(sv.memUsed)} dari ${fmtBytes(sv.memTotal)}`, 'memory', 'warn')}
    ${statCard('Disk', `<span id="st-disk">${pct(sv.diskPercent)}</span>`, `${fmtBytes(sv.diskUsed)} dari ${fmtBytes(sv.diskTotal)}`, 'hardDrive', '')}
  </div>
  <div class="split mb-16">
    <section class="card">
      <div class="card-head"><div><h3>${icon('activity')}Performa server</h3><div class="hint">CPU dan RAM · diperbarui otomatis setiap 5 detik</div></div><div class="flex gap-12"><span class="live-indicator"><i class="live-dot"></i><span id="live-label">Live</span></span><div class="chart-legend"><span><i class="legend-dot" style="background:#22C55E"></i>CPU</span><span><i class="legend-dot" style="background:#38BDF8"></i>RAM</span></div></div></div>
      <div class="card-body" style="padding:14px 16px 8px"><div id="dash-chart">${lineChart(series)}</div></div>
      <div class="card-foot"><span class="tiny muted mono" id="chart-updated">${series.length?`${series.length} titik · ${esc(fmtTime(series[series.length-1]?.at))}`:'Menunggu data'}</span><a class="link-btn" href="#/settings">Ambang alert →</a></div>
    </section>
    <section class="card">
      <div class="card-head"><h3>${icon('server')}Server</h3><span class="badge green"><i class="dot"></i>Online</span></div>
      <div class="card-body">
        <div class="flex gap-16" style="align-items:center">
          ${donutChart(sv.diskPercent, 'Disk', Number(sv.diskPercent)>=90?'#EF4444':Number(sv.diskPercent)>=75?'#F59E0B':'#22C55E')}
          <div class="grow"><div class="small strong mono">${esc(sv.hostname||'—')}</div><div class="tiny muted" style="margin-top:3px">${esc(sv.os||'')} ${esc(sv.arch||'')} · ${esc(sv.kernel||'')}</div>
          <div class="tiny muted" style="margin-top:9px">${esc(sv.cpuModel||'CPU tidak diketahui')}</div></div>
        </div>
        <div class="hr" style="margin:16px 0"></div>
        <dl class="kv">
          <dt>Platform</dt><dd>${esc(sv.platform||'—')}</dd>
          <dt>Uptime</dt><dd>${dur(sv.uptimeSec)}</dd>
          <dt>Zona waktu</dt><dd>${esc(sv.timezone||'—')}</dd>
          <dt>RAM panel</dt><dd class="mono">${fmtBytes(sv.panelRamBytes)}</dd>
          <dt>Go</dt><dd class="mono">${esc(sv.goVersion||'—')}</dd>
        </dl>
      </div>
    </section>
  </div>
  <div class="grid" style="grid-template-columns:minmax(0,1.55fr) minmax(280px,1fr)">
    <section class="card">
      <div class="card-head"><div><h3>${icon('app')}Aplikasi</h3><div class="hint">Status proses dan kesehatan port</div></div><a class="link-btn" href="#/apps">Lihat semua →</a></div>
      <div class="table-wrap">${apps.length?`<table><thead><tr><th>Aplikasi</th><th>Status</th><th>Port</th><th>Deploy terakhir</th><th></th></tr></thead><tbody>
        ${apps.slice(0,6).map(a=>`<tr class="clickable" data-app="${a.id}"><td><div class="flex gap-8">${appIcon(a)}<div><div class="row-title">${esc(a.name)}</div><div class="row-sub mono">${esc(a.slug)}</div></div></div></td><td>${badge(a.status)}</td><td><span class="port-chip">${a.port}</span></td><td>${a.lastDeployState?badge(a.lastDeployState):'<span class="faint">—</span>'}<div class="row-sub">${timeAgo(a.lastDeployAt)}</div></td><td class="table-actions">${icon('chevronRight')}</td></tr>`).join('')}
      </tbody></table>`:emptyState('Belum ada aplikasi','Deploy aplikasi pertama Anda dari repositori Git atau image Docker.','<a class="btn primary sm" href="#/apps/new">Tambah aplikasi</a>','app')}</div>
    </section>
    <div class="stack-md">
      <section class="card">
        <div class="card-head"><h3>${icon('logs')}Aktivitas deploy</h3><a class="link-btn" href="#/deployments">Semua →</a></div>
        <div class="card-body">${recent.length?`<div class="timeline">${recent.slice(0,5).map(x=>`<div class="tl-item ${x.status==='berhasil'?'ok':x.status==='gagal'||x.status==='gagal-terpulihkan'?'bad':''}"><div class="tl-title">${esc(x.appName||'Aplikasi')} ${badge(x.status)}</div><div class="tl-sub">${esc(fmtDate(x.createdAt))} · ${esc(x.trigger||'manual')}${x.durationSec?` · ${dur(x.durationSec)}`:''}</div></div>`).join('')}</div>`:emptyState('Belum ada deploy','Deploy pertama akan tampil di sini.','','logs')}</div>
      </section>
      <section class="card">
        <div class="card-head"><h3>${icon('bell')}Notifikasi</h3><a class="link-btn" href="#/notifications">Semua →</a></div>
        <div class="card-body" style="padding-top:8px;padding-bottom:8px">${notes.length?notes.slice(0,4).map(n=>`<div class="list-row"><span class="avatar-sm">${icon(n.kind==='alert'?'alert':n.kind==='deploy'?'zap':n.kind==='backup'?'archive':'bell')}</span><div class="grow"><div class="small ${n.isRead?'':'strong'}">${esc(n.title)}</div><div class="tiny muted">${timeAgo(n.createdAt)}</div></div></div>`).join(''):emptyState('Tidak ada notifikasi','Kabar deploy, backup, dan alert akan muncul di sini.','','bell')}</div>
      </section>
    </div>
  </div>
  <div class="grid g4 mt-16">
    <a class="card card-pad" href="#/deployments" style="color:inherit;text-decoration:none"><div class="flex between"><span class="stat-label">${icon('logs')}Deploy terakhir</span>${icon('chevronRight')}</div><div class="stat-value" style="font-size:18px;margin-top:12px">${recent[0]?esc(recent[0].appName||'—'):'Belum ada'}</div><div class="stat-foot">${recent[0]?fmtDate(recent[0].createdAt):'—'}</div></a>
    <a class="card card-pad" href="#/backup" style="color:inherit;text-decoration:none"><div class="flex between"><span class="stat-label">${icon('archive')}Backup</span>${icon('chevronRight')}</div><div class="stat-value" style="font-size:18px;margin-top:12px">${s.backups||0} tersimpan</div><div class="stat-foot">${s.lastBackup?fmtDate(s.lastBackup):'Belum pernah backup'}</div></a>
    <a class="card card-pad" href="#/cron" style="color:inherit;text-decoration:none"><div class="flex between"><span class="stat-label">${icon('clock')}Cron aktif</span>${icon('chevronRight')}</div><div class="stat-value" style="font-size:18px;margin-top:12px">${s.cron||0} jadwal</div><div class="stat-foot">Zona waktu UTC</div></a>
    <a class="card card-pad" href="#/notifications" style="color:inherit;text-decoration:none"><div class="flex between"><span class="stat-label">${icon('bell')}Belum dibaca</span>${icon('chevronRight')}</div><div class="stat-value" style="font-size:18px;margin-top:12px">${s.unread||0} notifikasi</div><div class="stat-foot">Klik untuk melihat</div></a>
  </div>`;
  el.querySelectorAll('[data-app]').forEach(r=>r.onclick=()=>location.hash='#/apps/'+r.dataset.app);
  $('#dash-refresh', el).onclick = () => viewDashboard(el);
  const chartEl = $('#dash-chart', el);
  let frame = 0;
  const paint = () => { if (chartEl && document.body.contains(chartEl)) { chartEl.innerHTML = lineChart(series); } };
  openSSE('/api/monitor/stream', {
    snapshot: p => { if (p.server) { state.server=p.server; updateLiveStats(p.server); } },
    metrics: p => {
      if (p.server) { state.server=p.server; updateLiveStats(p.server); }
      if (p.point) {
        series.push(p.point); if (series.length>160) series=series.slice(-160);
        if (!frame) frame = requestAnimationFrame(()=>{ frame=0; paint(); const cu=$('#chart-updated',el); if(cu) cu.textContent=`${series.length} titik · ${fmtTime(p.point.at)}`; });
      }
    },
    'app.status': () => { clearTimeout(state._dashReload); state._dashReload=setTimeout(()=>viewDashboard(el),1200); },
    'deploy.end': () => { clearTimeout(state._dashReload); state._dashReload=setTimeout(()=>viewDashboard(el),1600); },
    notification: p => { state.unread=(state.unread||0)+1; updateUnread(); }
  });
  function updateLiveStats(v) {
    const c=$('#st-cpu',el), r=$('#st-ram',el), dsk=$('#st-disk',el);
    if(c)c.textContent=pct(v.cpuPercent); if(r)r.textContent=pct(v.memPercent); if(dsk)dsk.textContent=pct(v.diskPercent);
  }
  onCleanup(()=>{ clearTimeout(state._dashReload); });
}

/* ---------- Aplikasi list ---------- */
async function viewApps(el) {
  setPageTitle('Aplikasi');
  viewLoading(el);
  let data;
  try { data = await api('/api/apps'); } catch(e) { el.innerHTML=errorBlock(e.message); return; }
  const apps = data.apps || [];
  const runtimes=[...new Set(apps.map(a=>a.runtime))].sort();
  el.innerHTML = `${pageHead('Aplikasi', `${apps.length} aplikasi terdaftar · port-first, native-first.`, `<a class="btn primary" href="#/apps/new">${icon('plus')}Tambah aplikasi</a>`)}
  <div class="card mb-16"><div class="card-body" style="padding:12px 14px"><div class="toolbar"><div class="search">${field('','<div class="input-group"><input class="input" id="app-search" placeholder="Cari nama, slug, atau hostname…" style="min-height:36px"><span style="display:grid;place-items:center;width:34px;border:1px solid var(--border-soft);border-left:0;border-radius:0 8px 8px 0;color:#64748B">'+icon('search')+'</span></div>')}
    <select class="select" id="app-status" style="width:auto;min-width:130px"><option value="">Semua status</option><option>berjalan</option><option>berhenti</option><option>bermasalah</option><option>deploy</option></select>
    <select class="select" id="app-runtime" style="width:auto;min-width:130px"><option value="">Semua runtime</option>${runtimes.map(r=>`<option>${esc(r)}</option>`).join('')}</select>
    <span class="tiny muted" id="app-count" style="margin-left:auto"></span></div></div>
    <div class="table-wrap" id="apps-table"></div>
  </div>
  <div class="grid g3">
    <div class="card card-pad flex gap-12"><span class="stat-icon">${icon('route')}</span><div><div class="small strong">Port pool</div><div class="tiny muted">3100–3999 · otomatis</div></div></div>
    <div class="card card-pad flex gap-12"><span class="stat-icon info">${icon('layers')}</span><div><div class="small strong">Rilis terisolasi</div><div class="tiny muted">Deploy gagal tidak memutus rilis aktif</div></div></div>
    <a class="card card-pad flex gap-12" href="#/ports" style="color:inherit"><span class="stat-icon warn">${icon('cloud')}</span><div><div class="small strong">Cloudflare Tunnel</div><div class="tiny muted">Kelola hostname & ingress</div></div></a>
  </div>`;
  const table = $('#apps-table', el);
  function draw() {
    const q = ($('#app-search',el)?.value||'').toLowerCase();
    const st = $('#app-status',el)?.value||'', rt = $('#app-runtime',el)?.value||'';
    const list = apps.filter(a=>(!q||[a.name,a.slug,a.hostname].some(v=>(v||'').toLowerCase().includes(q))) && (!st||a.status===st) && (!rt||a.runtime===rt));
    $('#app-count',el).textContent = `${list.length} dari ${apps.length}`;
    if (!apps.length) { table.innerHTML = emptyState('Belum ada aplikasi','Tambahkan aplikasi pertama: hubungkan repo GitHub atau gunakan image Docker.','<a class="btn primary sm" href="#/apps/new">Tambah aplikasi</a>','app'); return; }
    table.innerHTML = `<table><thead><tr><th>Aplikasi</th><th>Status</th><th>Port</th><th>Runtime</th><th>Hostname</th><th>Deploy</th><th style="text-align:right">Aksi</th></tr></thead><tbody>
      ${list.map(a=>`<tr class="clickable" data-id="${a.id}">
        <td><div class="flex gap-8">${appIcon(a)}<div><div class="row-title">${esc(a.name)} ${a.needsRestart?'<span class="tag" style="color:#FCD34D">perlu restart</span>':''}</div><div class="row-sub mono">${esc(a.slug)}</div></div></div></td>
        <td>${badge(a.status)}<div class="row-sub">${badge(a.healthStatus)}</div></td>
        <td><span class="port-chip">${a.port}</span></td>
        <td><div class="small">${esc(a.runtime)} ${a.mode==='docker'?'· Docker':a.mode==='compose'?'· Compose':''}</div><div class="row-sub mono">${esc(a.activeCommit?String(a.activeCommit).slice(0,8):'belum ada rilis')}</div></td>
        <td>${a.hostname?`<a href="https://${attr(a.hostname)}" target="_blank" rel="noopener" data-stop>${esc(a.hostname)}</a>`:'<span class="faint">—</span>'}</td>
        <td>${a.lastDeployState?badge(a.lastDeployState):'<span class="faint">—</span>'}<div class="row-sub">${timeAgo(a.lastDeployAt)}</div></td>
        <td><div class="table-actions"><button class="btn-icon" data-deploy="${a.id}" title="Deploy">${icon('play')}</button><a class="btn-icon" href="#/apps/${a.id}/edit" title="Edit">${icon('edit')}</a><a class="btn-icon" href="#/apps/${a.id}" title="Detail">${icon('chevronRight')}</a></div></td>
      </tr>`).join('')}</tbody></table>`;
    table.querySelectorAll('tr[data-id]').forEach(r=>r.onclick=e=>{ if(e.target.closest('.table-actions')||e.target.closest('[data-stop]')) return; location.hash='#/apps/'+r.dataset.id; });
    table.querySelectorAll('[data-deploy]').forEach(b=>b.onclick=async()=>{ b.disabled=true; try { const d=await api('/api/apps/'+b.dataset.deploy+'/deploy',{method:'POST'}); toast('Deploy diantrekan','Deployment #'+d.deployment.id+' · antrean diproses satu per satu'); setTimeout(()=>location.hash='#/deployments',500);} catch(e){ toast('Deploy gagal',e.detail||e.message,'error'); } finally { b.disabled=false; }});
  }
  ['app-search','app-status','app-runtime'].forEach(id=>{ const n=$('#'+id,el); n.oninput=draw; n.onchange=draw; });
  draw();
}

/* ---------- App form ---------- */
async function viewAppForm(el, id) {
  const editing = !!id;
  setPageTitle(editing?'Edit aplikasi':'Aplikasi baru');
  viewLoading(el);
  let a = {};
  if (editing) {
    try { a=(await api('/api/apps/'+id)).app; } catch(e){ el.innerHTML=errorBlock(e.message); return; }
  }
  let gh = {configured:false};
  try { gh = await api('/api/integrations/github'); } catch(_) {}
  const def = {sourceType:'github',branch:'main',runtime:'node',mode:'native',driver:'auto',healthPath:'/',persistentDirs:[]};
  a = Object.assign({},def,a);
  const sel = (v,opts)=>opts.map(o=>`<option value="${attr(o[0])}" ${String(v)===String(o[0])?'selected':''}>${esc(o[1])}</option>`).join('');
  el.innerHTML = `${pageHead(editing?'Edit aplikasi':'Tambah aplikasi', editing?`Perbarui konfigurasi ${esc(a.name)}.`:'Hubungkan repositori atau image, lalu tentukan cara aplikasi dijalankan.', `<a class="btn ghost" href="${editing?'#/apps/'+id:'#/apps'}">${icon('arrowLeft')}Kembali</a>`, editing?`<span class="breadcrumb"><a href="#/apps">Aplikasi</a><span>›</span>${esc(a.name)}</span>`:'<span class="breadcrumb"><a href="#/apps">Aplikasi</a><span>›</span>Tambah</span>')}
  <form id="app-form" autocomplete="off">
  <div class="split">
    <div class="stack-md">
      <section class="card"><div class="card-head"><h3>${icon('app')}Identitas</h3><span class="tiny muted">Wajib</span></div><div class="card-body">
        <div class="form-grid">
          ${field('Nama aplikasi','<input class="input" name="name" maxlength="64" required placeholder="Contoh: Toko Satria" value="'+attr(a.name||'')+'">','Nama yang tampil di panel.',false,true)}
          ${field('Slug / ID','<input class="input mono" name="slug" maxlength="48" placeholder="otomatis dari nama" value="'+attr(a.slug||'')+'" '+(editing?'disabled':'')+'>','Dipakai untuk nama proses, folder, dan prefix Redis.',false,false)}
          ${field('Hostname publik','<input class="input mono" name="hostname" placeholder="toko.satriyadi.com" value="'+attr(a.hostname||'')+'">','Kosongkan bila belum memakai Cloudflare Tunnel.')}
          ${field('Health path','<input class="input mono" name="healthPath" placeholder="/" value="'+attr(a.healthPath||'/')+'">','HTTP GET ke 127.0.0.1:port; status &lt; 500 dianggap sehat.')}
        </div>
      </div></section>
      <section class="card"><div class="card-head"><h3>${icon('github')}Sumber kode</h3><span class="badge ${gh.configured?'green':'gray'}"><i class="dot"></i>${gh.configured?'GitHub @'+esc(gh.login||''):'GitHub belum terhubung'}</span></div><div class="card-body">
        <div class="form-grid">
          ${field('Jenis sumber','<select class="select" name="sourceType">'+sel(a.sourceType,[['github','GitHub (webhook + PAT)'],['git_https','Git HTTPS'],['git_ssh','Git SSH'],['image','Image Docker']])+'</select>')}
          ${field('Branch','<input class="input mono" name="branch" value="'+attr(a.branch||'main')+'" placeholder="main">')}
          <div class="full" id="repo-field">${field('URL repositori','<input class="input mono" name="repoUrl" placeholder="https://github.com/user/repo" value="'+attr(a.repoUrl||'')+'"><div class="hint" style="margin-top:6px" id="repo-hint">'+(gh.configured?'Terhubung sebagai @'+esc(gh.login||'')+' — token dipakai untuk clone repositori privat.':'Hubungkan Personal Access Token di Pengaturan → Integrasi agar bisa memilih repo dan memasang webhook otomatis.')+'</div><div class="flex gap-8" style="margin-top:8px">'+(gh.configured?'<button type="button" class="btn sm outline" id="pick-repo">'+icon('github')+'Pilih repositori</button><button type="button" class="btn sm outline" id="detect-repo">'+icon('zap')+'Deteksi otomatis</button>':'<a class="btn sm outline" href="#/settings?tab=integrations">'+icon('link')+'Hubungkan GitHub</a>')+'</div>')}</div>
          <div class="full" id="image-field">${field('Image Docker','<input class="input mono" name="image" placeholder="nginx:alpine atau ghcr.io/user/app:latest" value="'+attr(a.image||'')+'">','Dipakai bila sumber = Image Docker.')}</div>
        </div>
        <div class="alert-strip info mt-16">${icon('info')}<div>Repositori privat memakai token GitHub yang tersimpan terenkripsi. Deploy mengambil <b>salinan baru</b> ke folder rilis; kode lama tidak ditimpa.</div></div>
      </div></section>
      <section class="card"><div class="card-head"><h3>${icon('terminal')}Perintah & runtime</h3></div><div class="card-body">
        <div class="form-grid">
          ${field('Runtime','<select class="select" name="runtime">'+sel(a.runtime,[['node','Node.js'],['php','PHP'],['python','Python'],['go','Go'],['static','Static / HTML']])+'</select>')}
          ${field('Versi runtime','<input class="input mono" name="runtimeVersion" placeholder="opsional, mis. 20" value="'+attr(a.runtimeVersion||'')+'">')}
          ${field('Mode menjalankan','<select class="select" name="mode">'+sel(a.mode,[['native','Native (PM2 / systemd / proses)'],['docker','Docker'],['compose','Docker Compose']])+'</select>')}
          ${field('Driver proses','<select class="select" name="driver">'+sel(a.driver,[['auto','Otomatis'],['direct','Direct process'],['pm2','PM2'],['systemd','systemd'],['supervisor','Supervisor']])+'</select>')}
          <div class="full">${field('Perintah install','<input class="input mono" name="installCmd" placeholder="npm ci" value="'+attr(a.installCmd||'')+'">','Dijalankan di folder rilis baru.')}</div>
          <div class="full">${field('Perintah build','<input class="input mono" name="buildCmd" placeholder="npm run build --if-present" value="'+attr(a.buildCmd||'')+'">')}</div>
          <div class="full" id="start-field">${field('Perintah start','<input class="input mono" name="startCmd" placeholder="npm start" value="'+attr(a.startCmd||'')+'">','Gunakan <span class="mono">$PORT</span> bila aplikasi membaca port dari lingkungan.')}</div>
          <div id="internal-port-field">${field('Port internal kontainer','<input class="input mono" type="number" name="internalPort" min="1" max="65535" value="'+attr(a.internalPort||'')+'">','Port di dalam container yang dipetakan ke port host.')}</div>
          <div id="compose-field">${field('Nama service Compose','<input class="input mono" name="composeService" placeholder="web" value="'+attr(a.composeService||'')+'">','Kosongkan bila hanya satu service.')}</div>
        </div>
      </div></section>
    </div>
    <div class="stack-md">
      <section class="card"><div class="card-head"><h3>${icon('route')}Jaringan</h3></div><div class="card-body">
        ${field('Port host','<div class="input-group"><input class="input mono" type="number" name="port" min="1024" max="65535" value="'+attr(a.port||'')+'" placeholder="3100"><button type="button" class="btn" id="next-port">Saran</button></div>','Port dari pool 3100–3999. Port harus kosong dan belum dipakai aplikasi lain.',false,true)}
        <div class="alert-strip mt-12">${icon('info')}<div>Panel mengikat aplikasi ke <span class="mono">127.0.0.1</span>. Akses publik melalui Cloudflare Tunnel, bukan membuka port ke internet.</div></div>
      </div></section>
      <section class="card"><div class="card-head"><h3>${icon('folder')}Data & deploy</h3></div><div class="card-body">
        ${field('Folder persistent','<textarea class="textarea mono" name="persistentDirs" rows="3" placeholder="uploads&#10;storage">'+esc((a.persistentDirs||[]).join('\n'))+'</textarea>','Satu folder per baris, relatif terhadap aplikasi. Tidak ikut tertimpa saat deploy.')}
        <div style="margin-top:14px">${switchRow('Auto deploy dari GitHub','Push ke branch memicu deploy bila webhook terpasang.','autoDeploy',!!a.autoDeploy)}</div>
      </div></section>
      ${editing?`<section class="card" style="border-color:rgba(239,68,68,.25)"><div class="card-head"><h3 style="color:#FCA5A5">${icon('alert')}Zona berbahaya</h3></div><div class="card-body"><p class="small muted" style="line-height:1.6">Hapus aplikasi menghentikan proses, mencabut webhook, dan menghapus catatan rilis. Folder kerja dapat dipertahankan.</p><button type="button" class="btn danger mt-12" id="delete-app">${icon('trash')}Hapus aplikasi</button></div></section>`:''}
      <div class="flex end gap-8"><a class="btn ghost" href="${editing?'#/apps/'+id:'#/apps'}">Batal</a><button class="btn primary" type="submit" id="save-app">${icon('check')}${editing?'Simpan perubahan':'Tambah aplikasi'}</button></div>
    </div>
  </div>
  </form>`;
  const form = $('#app-form',el);
  const syncMode = () => {
    const mode = form.mode.value, src = form.sourceType.value;
    $('#image-field',el).style.display = src==='image'?'':'none';
    $('#repo-field',el).style.display = src==='image'?'none':'';
    $('#internal-port-field',el).style.display = mode==='docker'?'':'none';
    $('#compose-field',el).style.display = mode==='compose'?'':'none';
    $('#start-field',el).style.display = mode==='native'?'':'none';
  };
  form.sourceType.onchange=syncMode; form.mode.onchange=syncMode; syncMode();
  $('#next-port',el).onclick=async()=>{ try { const d=await api('/api/ports/next'); form.port.value=d.port; toast('Port disarankan',`Port ${d.port} tersedia`); } catch(e){ toast('Tidak ada port',e.message,'error'); } };
  if (!$('#next-port',el)?.value && !a.port && !editing) { try { const d=await api('/api/ports/next'); form.port.value=d.port; } catch(_){} }
  if (gh.configured) {
    $('#pick-repo',el).onclick = () => pickGitHubRepo(repo=>{ form.repoUrl.value='https://github.com/'+repo.full_name; form.branch.value=repo.default_branch||'main'; form.sourceType.value='github'; syncMode(); });
    $('#detect-repo',el).onclick = async () => {
      const repo = repoFromURL(form.repoUrl.value); if(!repo){ toast('Isi URL repo dulu','Gunakan URL github.com/owner/repo', 'error'); return; }
      try { const d=await api('/api/github/detect'+qs({repo:repo.full_name||repo,branch:form.branch.value})); form.runtime.value=d.runtime; form.mode.value=d.mode; form.installCmd.value=d.installCmd; form.buildCmd.value=d.buildCmd; form.startCmd.value=d.startCmd; form.port.value=form.port.value||d.port; syncMode(); toast('Konfigurasi terdeteksi',`Runtime ${d.runtime} · mode ${d.mode}`); } catch(e){ toast('Deteksi gagal',e.detail||e.message,'error'); }
    };
  }
  const submitBtn = $('#save-app',el);
  form.onsubmit = async ev => {
    ev.preventDefault();
    const fd = new FormData(form);
    const body = {
      name:fd.get('name'), slug:fd.get('slug'), sourceType:fd.get('sourceType'), repoUrl:fd.get('repoUrl'), branch:fd.get('branch'),
      runtime:fd.get('runtime'), runtimeVersion:fd.get('runtimeVersion'), mode:fd.get('mode'), driver:fd.get('driver'),
      installCmd:fd.get('installCmd'), buildCmd:fd.get('buildCmd'), startCmd:fd.get('startCmd'), port:Number(fd.get('port')||0),
      hostname:fd.get('hostname'), autoDeploy:!!form.autoDeploy?.checked, persistentDirs:String(fd.get('persistentDirs')||'').split('\n').map(x=>x.trim()).filter(Boolean),
      healthPath:fd.get('healthPath'), image:fd.get('image'), internalPort:Number(fd.get('internalPort')||0), composeService:fd.get('composeService')
    };
    submitBtn.disabled=true;
    try {
      let out;
      if (editing) out=await api('/api/apps/'+id,{method:'PUT',body}); else out=await api('/api/apps',{method:'POST',body});
      toast(editing?'Aplikasi disimpan':'Aplikasi dibuat','Mengarahkan ke detail aplikasi…');
      setTimeout(()=>location.hash='#/apps/'+(out.app?.id||id),400);
    } catch(e){ toast(editing?'Gagal menyimpan':'Gagal membuat aplikasi', e.detail||e.message,'error'); submitBtn.disabled=false; }
  };
  $('#delete-app',el)?.addEventListener('click', async()=>{
    const ok = await confirmDialog({title:'Hapus '+a.name+'?',message:'Proses akan dihentikan dan webhook GitHub dicabut. Folder kerja hanya dihapus jika Anda mencentang opsi.',typed:a.name,confirmLabel:'Hapus aplikasi',danger:true,input:true, inputLabel:'Ketik '+a.name+' untuk konfirmasi'});
    if(!ok) return;
    const delFiles = confirm('Hapus juga folder kerja beserta seluruh rilis? Klik Batal untuk menyimpan folder.');
    try { await api('/api/apps/'+id+qs({deleteFiles:delFiles?1:0}),{method:'DELETE'}); toast('Aplikasi dihapus'); location.hash='#/apps'; } catch(e){ toast('Gagal menghapus',e.detail||e.message,'error'); }
  });
}
function repoFromURL(v) {
  v=String(v||'').replace(/\.git$/,''); const m=/github\.com[/:]([^/]+)\/([^/]+)/i.exec(v); return m?{owner:m[1],name:m[2],full_name:m[1]+'/'+m[2]}:null;
}
async function pickGitHubRepo(onPick) {
  const m = modal({title:'Pilih repositori GitHub',subtitle:'Daftar repositori akun yang terhubung.',body:'<div id="repo-list">'+loadingBlock('Memuat repositori…')+'</div>',size:'lg'});
  try {
    const d=await api('/api/github/repos');
    const render = q => {
      const list=(d.repos||[]).filter(r=>!q||r.full_name.toLowerCase().includes(q.toLowerCase()));
      $('#repo-list',m.overlay).innerHTML=`<input class="input mb-12" id="repo-q" placeholder="Cari repo…" value="${attr(q||'')}"><div style="max-height:430px;overflow:auto">${list.length?list.map(r=>`<button type="button" data-repo="${attr(r.full_name)}" data-branch="${attr(r.default_branch)}" style="display:flex;width:100%;text-align:left;background:none;border:0;border-bottom:1px solid var(--border-soft);padding:10px 4px;color:inherit;gap:10px;align-items:center">${icon('github')}<div class="grow"><div class="small strong mono">${esc(r.full_name)}</div><div class="tiny muted">${esc(r.default_branch)} · ${r.private?'Privat':'Publik'} · diperbarui ${timeAgo(r.updated_at)}</div></div>${icon('chevronRight')}</button>`).join(''):'<div class="empty small">Tidak ada repo</div>'}</div>`;
      $('#repo-q',m.overlay).oninput=e=>render(e.target.value);
      $$('[data-repo]',m.overlay).forEach(b=>b.onclick=()=>{ m.close(); onPick({full_name:b.dataset.repo,default_branch:b.dataset.branch}); });
    };
    render('');
  } catch(e){ $('#repo-list',m.overlay).innerHTML=errorBlock(e.detail||e.message); }
}

/* ---------- App detail ---------- */
async function viewAppDetail(el, id) {
  let data;
  try { data = await api('/api/apps/'+id); } catch(e){ setPageTitle('Aplikasi'); el.innerHTML=errorBlock(e.message,'<a class="btn" href="#/apps">Kembali</a>'); return; }
  const a=data.app, releases=data.releases||[], deployments=data.deployments||[];
  setPageTitle(a.name, a.hostname || a.slug);
  state.currentApp=data;
  const tab=(location.hash.split('?')[1]||'tab=ringkasan').replace('tab=','');
  const active = ['ringkasan','deploy','env','log','database','cron'].includes(tab)?tab:'ringkasan';
  el.innerHTML = `<div class="breadcrumb"><a href="#/apps">Aplikasi</a><span>›</span>${esc(a.name)}</div>
  <section class="card mb-16"><div class="hero-app">
    <span class="app-avatar">${esc(initials(a.name))}</span>
    <div class="grow"><div class="flex gap-8 wrap"><h1 style="font-size:21px">${esc(a.name)}</h1>${badge(a.status)}${badge(a.healthStatus,'Health: '+(statusInfo(a.healthStatus)[1]))}</div>
      <div class="app-meta"><span class="tag mono">${esc(a.slug)}</span><span class="port-chip">:${a.port}</span>${a.hostname?`<a class="tag" href="https://${attr(a.hostname)}" target="_blank" rel="noopener">${icon('globe')}${esc(a.hostname)}</a>`:''}<span class="tag">${esc(a.runtime)}</span><span class="tag">${esc(a.mode)}</span>${a.needsRestart?'<span class="badge amber"><i class="dot"></i>Perlu restart</span>':''}</div>
      <div class="tiny muted" style="margin-top:7px">Commit aktif <span class="mono">${esc(a.activeCommit?a.activeCommit.slice(0,9):'—')}</span> · deploy ${timeAgo(a.lastDeployAt)}</div>
    </div>
    <div class="action-bar">
      ${a.running?`<button class="btn sm" data-act="stop">${icon('stop')}Hentikan</button><button class="btn sm" data-act="restart">${icon('restart')}Mulai ulang</button>`:`<button class="btn sm primary" data-act="start">${icon('play')}Mulai</button>`}
      <button class="btn sm outline" data-act="deploy">${icon('rotate')}Deploy</button>
      <a class="btn sm outline" href="#/apps/${a.id}/edit">${icon('edit')}Edit</a>
      <div class="dropdown-wrap"><button class="btn-icon" id="app-more">${icon('more')}</button><div id="app-more-menu" hidden></div></div>
    </div>
  </div>
  <div class="tabs" style="padding:0 14px">${[['ringkasan','Ringkasan','dashboard'],['deploy','Deploy & Rilis','logs'],['env','Env', 'key'],['log','Log','terminal'],['database','Database','database'],['cron','Cron','clock']].map(([k,t,ic])=>`<a class="tab ${active===k?'active':''}" href="#/apps/${a.id}?tab=${k}">${icon(ic)}${t}${k==='deploy'?`<span class="count">${releases.length}</span>`:k==='env'?`<span class="count">${a.envCount}</span>`:''}</a>`).join('')}</div>
  </section>
  <div id="app-tab"></div>`;
  const content=$('#app-tab',el);
  $('#app-more',el).onclick=e=>{ e.stopPropagation(); const menu=$('#app-more-menu',el); menu.hidden=!menu.hidden; menu.className='dropdown'; menu.innerHTML=`<button data-m="logs">${icon('logs')}Log deploy terbaru</button><button data-m="webhook">${icon('github')}Pasang webhook GitHub</button><a class="dropdown" href="#/apps/${a.id}/edit" style="text-decoration:none;padding:8px 10px;display:flex;gap:9px;color:#CBD5E1">${icon('settings')}Pengaturan aplikasi</a>`; menu.querySelector('[data-m=logs]').onclick=()=>{ location.hash=`#/deployments/${deployments[0]?.id||0}`; }; menu.querySelector('[data-m=webhook]').onclick=async()=>{ try{ await api('/api/apps/'+a.id+'/webhook',{method:'POST'}); toast('Webhook dipasang'); viewAppDetail(el,id);}catch(e){toast('Gagal memasang webhook',e.detail||e.message,'error');} }; };
  document.addEventListener('click',()=>{ const mm=$('#app-more-menu',el); if(mm)mm.hidden=true; },{once:true});
  el.querySelectorAll('[data-act]').forEach(b=>b.onclick=async()=>{ const act=b.dataset.act; if(act==='deploy'){ b.disabled=true; try{ const d=await api('/api/apps/'+a.id+'/deploy',{method:'POST'}); toast('Deploy diantrekan','# '+(d.deployment?.id||'')); setTimeout(()=>location.hash='#/apps/'+a.id+'?tab=deploy',500);}catch(e){toast('Deploy gagal',e.detail||e.message,'error'); b.disabled=false;} return; } try{ await api('/api/apps/'+a.id+'/actions/'+act,{method:'POST'}); toast('Aksi '+act+' dijalankan'); setTimeout(()=>viewAppDetail(el,id),700);}catch(e){toast('Aksi gagal',e.detail||e.message,'error');} });
  if (active==='ringkasan') renderAppOverview(content,a,releases,deployments,data);
  if (active==='deploy') renderAppDeploy(content,id,a,releases,deployments);
  if (active==='env') renderAppEnv(content,id,a,data);
  if (active==='log') renderAppLog(content,id,a);
  if (active==='database') renderAppDatabase(content,a,data);
  if (active==='cron') renderAppCron(content,id,a);
}
function renderAppOverview(c,a,releases,deps,data){
  const lastSuccess=deps.find(d=>d.status==='berhasil');
  c.innerHTML=`<div class="grid g4 mb-16">
    ${statCard('Pemeriksaan kesehatan',badge(a.healthStatus), a.healthCheckedAt?`Diperiksa ${timeAgo(a.healthCheckedAt)}`:'Belum diperiksa','activity',a.healthStatus==='gagal'?'warn':'')}
    ${statCard('Port host',`<span class="mono">${a.port}</span>`,`Internal ${a.internalPort||a.port} · 127.0.0.1`,'route','info')}
    ${statCard('Rilis tersimpan',releases.length,`Aktif #${a.activeReleaseId||'—'}`,'layers','')}
    ${statCard('Env vars',a.envCount,data.database?'Database terhubung':'Tanpa database','key','warn')}
  </div>
  <div class="split">
    <div class="stack-md">
      <section class="card"><div class="card-head"><h3>${icon('info')}Informasi aplikasi</h3></div><div class="card-body"><dl class="kv">
        <dt>Repositori</dt><dd class="mono" style="word-break:break-all">${esc(a.repoUrl||a.image||'—')}</dd>
        <dt>Branch</dt><dd class="mono">${esc(a.branch||'—')}</dd>
        <dt>Commit</dt><dd class="mono">${esc(a.activeCommit||'—')}</dd>
        <dt>Runtime</dt><dd>${esc(a.runtime)} ${esc(a.runtimeVersion||'')} · ${esc(a.driver)}</dd>
        <dt>Mode</dt><dd>${esc(a.mode)}${a.mode==='docker'?` · image <span class="mono">${esc(a.image||'—')}</span>`:''}</dd>
        <dt>Folder kerja</dt><dd class="mono" style="word-break:break-all">${esc(a.workdir)}</dd>
        <dt>Persistent</dt><dd>${a.persistentDirs?.length?a.persistentDirs.map(d=>`<span class="tag mono">${esc(d)}</span>`).join(' '):'<span class="faint">Tidak ada</span>'}</dd>
        <dt>Dibuat</dt><dd>${esc(fmtDate(a.createdAt))}</dd>
      </dl></div></section>
      <section class="card"><div class="card-head"><h3>${icon('logs')}Timeline deploy</h3><a class="link-btn" href="#/apps/${a.id}?tab=deploy">Semua →</a></div><div class="card-body">${deps.length?`<div class="timeline">${deps.slice(0,6).map(d=>`<div class="tl-item ${d.status==='berhasil'?'ok':d.status==='gagal'||d.status==='gagal-terpulihkan'?'bad':''}"><div class="tl-title">${badge(d.status)} <span class="deploy-id">#${d.id}</span> <span class="tiny muted">${esc(d.trigger)}</span></div><div class="tl-sub">${esc(fmtDate(d.createdAt))}${d.durationSec?` · ${dur(d.durationSec)}`:''} ${d.error?`· ${esc(truncateText(d.error,120))}`:''}</div></div>`).join('')}</div>`:emptyState('Belum ada deploy','Klik Deploy untuk membuat rilis pertama.','','logs')}</div></section>
    </div>
    <div class="stack-md">
      <section class="card"><div class="card-head"><h3>${icon('zap')}Aksi cepat</h3></div><div class="card-body stack-sm">
        <button class="btn block" id="ov-deploy">${icon('rotate')}Deploy rilis baru</button>
        <button class="btn block" id="ov-log">${icon('terminal')}Lihat log aplikasi</button>
        <a class="btn block" href="#/files?app=${a.id}">${icon('folder')}Buka file manager</a>
        <button class="btn block" id="ov-restart">${icon('restart')}Mulai ulang proses</button>
        ${lastSuccess?`<button class="btn block" id="ov-rollback">${icon('rotate')}Rollback ke rilis #${lastSuccess.releaseId||'—'}</button>`:''}
      </div></section>
      <section class="card"><div class="card-head"><h3>${icon('link')}Webhook GitHub</h3><span class="badge ${a.webhookId?'green':'gray'}"><i class="dot"></i>${a.webhookId?'Aktif':'Belum aktif'}</span></div><div class="card-body"><div class="hint mb-8">URL penerima push:</div><div class="injected"><span class="copy-text">${esc(data.webhookUrl)}</span><button class="btn-icon" data-copy="${attr(data.webhookUrl)}">${icon('copy')}</button></div><div class="hint mt-12">Auto deploy: <b>${a.autoDeploy?'aktif':'nonaktif'}</b> · Branch: <span class="mono">${esc(a.branch)}</span></div></div></section>
      <section class="card"><div class="card-head"><h3>${icon('database')}Koneksi</h3></div><div class="card-body"><div class="list-row"><span class="avatar-sm">${icon('database')}</span><div class="grow"><div class="small">${data.database?esc(data.database.name):'Belum ada database'}</div><div class="tiny muted">${data.database?esc(data.database.engine):'Hubungkan lewat menu Database'}</div></div></div><div class="list-row"><span class="avatar-sm">${icon('redis')}</span><div class="grow"><div class="small">${data.redis?esc(data.redis.aclUser):'Belum ada Redis ACL'}</div><div class="tiny muted">${data.redis?esc(data.redis.prefix+'*'):'Alokasikan dari menu Redis'}</div></div></div></div></section>
    </div>
  </div>`;
  $('#ov-deploy',c).onclick=async()=>{try{await api('/api/apps/'+a.id+'/deploy',{method:'POST'});toast('Deploy diantrekan');location.hash=`#/apps/${a.id}?tab=deploy`;}catch(e){toast('Deploy gagal',e.detail||e.message,'error');}};
  $('#ov-log',c).onclick=()=>location.hash=`#/apps/${a.id}?tab=log`;
  $('#ov-restart',c).onclick=async()=>{try{await api('/api/apps/'+a.id+'/actions/restart',{method:'POST'});toast('Restart dijalankan');setTimeout(()=>viewAppDetail(document.getElementById('content'),a.id),700);}catch(e){toast('Gagal restart',e.detail||e.message,'error');}};
  c.querySelectorAll('[data-copy]').forEach(b=>b.onclick=()=>copyText(b.dataset.copy));
  $('#ov-rollback',c)?.addEventListener('click',()=>{
    if(!lastSuccess?.releaseId){toast('Tidak ada rilis untuk rollback','','error');return;}
    confirmDialog({title:'Rollback aplikasi?',message:`Aplikasi akan dijalankan dengan rilis #${lastSuccess.releaseId}. Rilis saat ini tetap tersimpan.`,confirmLabel:'Rollback'}).then(ok=>{if(ok) rollbackApp(a.id,lastSuccess.releaseId);});
  });
}
async function rollbackApp(appId,relId){
  try{ await api(`/api/apps/${appId}/rollback/${relId}`,{method:'POST'}); toast('Rollback berhasil','Rilis #'+relId+' aktif kembali'); setTimeout(()=>location.hash=`#/apps/${appId}?tab=deploy`,600);}catch(e){toast('Rollback gagal',e.detail||e.message,'error');}
}
function renderAppDeploy(c,id,a,releases,deps){
  c.innerHTML=`<div class="split"><section class="card"><div class="card-head"><div><h3>${icon('logs')}Deployment</h3><div class="hint">Satu build pada satu waktu · antrean otomatis</div></div><button class="btn sm primary" id="dep-now">${icon('rotate')}Deploy sekarang</button></div><div class="table-wrap">${deps.length?`<table><thead><tr><th>#</th><th>Status</th><th>Trigger</th><th>Commit</th><th>Durasi</th><th>Waktu</th><th></th></tr></thead><tbody>${deps.map(d=>`<tr><td class="mono">${d.id}</td><td>${badge(d.status)}</td><td>${esc(d.trigger)}</td><td class="mono">${esc((d.commitHash||'—').slice(0,8))}</td><td>${d.durationSec?dur(d.durationSec):'—'}</td><td>${timeAgo(d.createdAt)}</td><td class="table-actions"><button class="btn-icon" data-log="${d.id}" title="Log">${icon('terminal')}</button>${(d.status==='antre'||d.status==='berjalan')?`<button class="btn-icon danger" data-cancel="${d.id}" title="Batalkan">${icon('close')}</button>`:''}</td></tr>`).join('')}</tbody></table>`:emptyState('Belum ada deployment','Deploy pertama belum dijalankan.','','logs')}</div></section>
  <section class="card"><div class="card-head"><h3>${icon('layers')}Rilis</h3><span class="count tiny muted">${releases.length} tersimpan</span></div><div class="table-wrap">${releases.length?`<table><thead><tr><th>Rilis</th><th>Commit</th><th>Status</th><th>Dibuat</th><th></th></tr></thead><tbody>${releases.map(r=>`<tr><td class="mono">#${r.id}</td><td class="mono">${esc((r.commitHash||'—').slice(0,8))}</td><td>${badge(r.status)}</td><td>${timeAgo(r.builtAt)}</td><td class="table-actions">${r.id!==a.activeReleaseId?`<button class="btn sm ghost" data-rollback="${r.id}">Rollback</button>`:'<span class="badge green">Aktif</span>'}</td></tr>`).join('')}</tbody></table>`:emptyState('Belum ada rilis','Rilis dibuat saat deploy berhasil.','','layers')}</div></section></div>`;
  $('#dep-now',c).onclick=async()=>{try{await api('/api/apps/'+id+'/deploy',{method:'POST'});toast('Deploy diantrekan');setTimeout(()=>location.reload(),500);}catch(e){toast('Deploy gagal',e.detail||e.message,'error');}};
  c.querySelectorAll('[data-log]').forEach(b=>b.onclick=()=>openDeployLog(b.dataset.log));
  c.querySelectorAll('[data-cancel]').forEach(b=>b.onclick=async()=>{try{await api('/api/deployments/'+b.dataset.cancel+'/cancel',{method:'POST'});toast('Deploy dibatalkan');viewAppDetail(document.getElementById('content'),id);}catch(e){toast('Gagal membatalkan',e.message,'error');}});
  c.querySelectorAll('[data-rollback]').forEach(b=>b.onclick=()=>confirmDialog({title:'Rollback ke rilis #'+b.dataset.rollback+'?',message:'Rilis aktif saat ini tetap disimpan. Aplikasi diuji kesehatan setelah rollback.',confirmLabel:'Rollback'}).then(ok=>{if(ok) rollbackApp(id,b.dataset.rollback);}));
}
function openDeployLog(depId){
  const dr=drawer({title:'Log deploy #'+depId,subtitle:'Streaming log build dan health check.',wide:true,body:`<div class="flex between mb-12"><span class="live-indicator"><i class="live-dot"></i>Live</span><span class="tiny muted mono" id="dep-log-meta"></span></div><div class="log-view" id="dep-log" style="max-height:calc(100vh - 190px)"></div>`,footer:`<button class="btn sm outline" id="dep-log-download">${icon('download')}Unduh log</button><button class="btn sm" data-close2>Tutup</button>`});
  const box=$('#dep-log',dr.el);
  const add=lines=>{ const stick=box.scrollTop+box.clientHeight>=box.scrollHeight-40; box.insertAdjacentHTML('beforeend',logHTML(lines)); if(stick) box.scrollTop=box.scrollHeight; };
  const es=openSSE('/api/deployments/'+depId+'/logs/stream',{history:p=>{box.innerHTML='';add(p.lines||[]);},lines:p=>add(p.lines||[]),status:p=>{ $('#dep-log-meta',dr.el).textContent = p.live?'streaming':'selesai'; }});
  onCleanup(()=>{try{es.close();}catch(_){}});
  $('#dep-log-download',dr.el).onclick=()=>apiDownload('/api/deployments/'+depId+'/logs?download=1','deploy-'+depId+'.log').catch(e=>toast('Unduhan gagal',e.message,'error'));
}
function renderAppEnv(c,id,a,data){
  const env=data.env||[];
  const rows = env.map((v,i)=>`<tr data-env-row><td><input class="input mono" data-k value="${attr(v.key)}" title="Nama variabel"></td><td><input class="input mono" data-v type="${v.isSecret?'password':'text'}" value="${v.isSecret?'':attr(v.value)}" placeholder="${v.isSecret?'•••• tersimpan — kosongkan untuk mempertahankan':''}"></td><td style="text-align:center"><input type="checkbox" data-secret ${v.isSecret?'checked':''} title="Rahasiakan nilai"></td><td style="text-align:center"><input type="checkbox" data-keep ${v.isSecret?'checked':''} title="Pertahankan nilai rahasia lama"></td><td><button type="button" class="btn-icon danger" data-del>${icon('trash')}</button></td></tr>`).join('');
  c.innerHTML=`<div class="split"><section class="card"><div class="card-head"><div><h3>${icon('key')}Variabel lingkungan</h3><div class="hint">Nilai rahasia tidak pernah ditampilkan kembali. Simpan &amp; restart untuk menerapkan.</div></div><button class="btn sm outline" id="add-env">${icon('plus')}Tambah</button></div><div class="card-body"><div class="alert-strip info mb-12">${icon('lock')}<div>Nilai disimpan terenkripsi AES-256-GCM. Saat menyimpan, kosongkan nilai rahasia dengan “pertahankan” dicentang untuk memakai nilai lama.</div></div><div class="table-wrap"><table class="env-table"><thead><tr><th>Key</th><th>Value</th><th style="width:60px;text-align:center">Rahasia</th><th style="width:70px;text-align:center">Pertahankan</th><th></th></tr></thead><tbody id="env-rows">${rows||''}</tbody></table>${rows?'':'<div class="empty small">Belum ada variabel kustom. Variabel bawaan tetap diinjeksi otomatis.</div>'}</div></div><div class="card-foot"><span class="tiny muted">${env.length} variabel kustom</span><div class="flex gap-8"><button class="btn" id="save-env">${icon('check')}Simpan env</button></div></div></section>
  <div class="stack-md"><section class="card"><div class="card-head"><h3>${icon('zap')}Variabel bawaan</h3></div><div class="card-body stack-sm">${Object.entries(data.injected||{}).map(([k,v])=>`<div class="injected"><span>${esc(k)}</span><span class="muted">${esc(v)}</span></div>`).join('')}</div></section>
  <section class="card"><div class="card-head"><h3>Setelah mengubah env</h3></div><div class="card-body"><p class="small muted" style="line-height:1.6">Panel menandai aplikasi <b>perlu restart</b>. Deploy ulang membuat rilis baru dengan env terbaru. Restart langsung memakai env yang tersimpan.</p><button class="btn block mt-12" id="env-restart">${icon('restart')}Restart sekarang</button></div></section></div></div>`;
  const bindDel=()=>{
    c.querySelectorAll('[data-env-row]').forEach(tr=>{
      const del=tr.querySelector('[data-del]'); if(del) del.onclick=()=>tr.remove();
      const val=tr.querySelector('[data-v]'), keep=tr.querySelector('[data-keep]');
      if(val&&keep){ val.oninput=()=>{ if(val.value) keep.checked=false; }; }
    });
  };
  bindDel();
  $('#add-env',c).onclick=()=>{ $('#env-rows',c).insertAdjacentHTML('beforeend',`<tr data-env-row><td><input class="input mono" data-k placeholder="NAMA_VAR"></td><td><input class="input mono" data-v placeholder="nilai"></td><td style="text-align:center"><input type="checkbox" data-secret></td><td style="text-align:center"><input type="checkbox" data-keep></td><td><button type="button" class="btn-icon danger" data-del>${icon('trash')}</button></td></tr>`); bindDel(); };
  $('#save-env',c).onclick=async()=>{
    const vars=$$('[data-env-row]',c).map(tr=>({key:tr.querySelector('[data-k]').value.trim(),value:tr.querySelector('[data-v]').value,isSecret:tr.querySelector('[data-secret]').checked,keep:tr.querySelector('[data-keep]').checked})).filter(v=>v.key);
    const btn=$('#save-env',c); btn.disabled=true;
    try{ await api('/api/apps/'+id+'/env',{method:'PUT',body:{vars}}); toast('Env disimpan','Aplikasi perlu restart agar perubahan berlaku.'); viewAppDetail(document.getElementById('content'),id); }catch(e){toast('Gagal menyimpan env',e.detail||e.message,'error');btn.disabled=false;}
  };
  $('#env-restart',c).onclick=async()=>{try{await api('/api/apps/'+id+'/actions/restart',{method:'POST'});toast('Restart dijalankan');}catch(e){toast('Gagal restart',e.detail||e.message,'error');}};
}
function renderAppLog(c,id,a){
  c.innerHTML=`<section class="card"><div class="card-head"><div><h3>${icon('terminal')}Log aplikasi</h3><div class="hint mono">${esc(a.slug)}.log · stdout &amp; stderr</div></div><div class="flex gap-8"><span class="live-indicator"><i class="live-dot"></i>Live</span><button class="btn sm outline" id="log-download">${icon('download')}Unduh</button><button class="btn sm outline" id="log-reload">${icon('refresh')}Muat ulang</button></div></div><div class="card-body"><div class="log-view" id="app-log" style="max-height:calc(100vh - 290px);min-height:360px"></div></div><div class="card-foot"><span class="tiny muted" id="log-meta">Menghubungkan stream…</span><span class="tiny muted">Log berputar otomatis pada 12 MB</span></div></section>`;
  const box=$('#app-log',c); let lines=0;
  const add=arr=>{ const stick=box.scrollTop+box.clientHeight>=box.scrollHeight-40; box.insertAdjacentHTML('beforeend',logHTML(arr)); lines+=arr.length; $('#log-meta',c).textContent=`${lines} baris ditampilkan · ${fmtTime(new Date().toISOString())}`; if(stick)box.scrollTop=box.scrollHeight; };
  const es=openSSE('/api/apps/'+id+'/logs/stream',{history:p=>{box.innerHTML='';add(p.lines||[]);},lines:p=>add(p.lines||[])});
  onCleanup(()=>{try{es.close();}catch(_){}});
  $('#log-reload',c).onclick=()=>{ box.innerHTML=''; api('/api/apps/'+id+'/logs?lines=300').then(d=>add(d.lines||[])).catch(e=>add(['[gagal] '+e.message])); };
  $('#log-download',c).onclick=()=>apiDownload('/api/apps/'+id+'/logs?download=1','app-'+a.slug+'.log').catch(e=>toast('Unduhan gagal',e.message,'error'));
  api('/api/apps/'+id+'/logs?lines=300').then(d=>{ if(!lines) add(d.lines||[]); }).catch(_=>{});
}
function renderAppDatabase(c,a,data){
  const db=data.database, r=data.redis;
  c.innerHTML=`<div class="grid g2"><section class="card"><div class="card-head"><h3>${icon('database')}Database terhubung</h3>${db?badgeText('green',db.engine):''}</div><div class="card-body">${db?`<dl class="kv"><dt>Nama</dt><dd class="mono">${esc(db.name)}</dd><dt>Engine</dt><dd>${esc(db.engine)}</dd><dt>Host</dt><dd class="mono">${esc(db.host||db.filePath||'—')}${db.port?':'+db.port:''}</dd><dt>Username</dt><dd class="mono">${esc(db.username||'—')}</dd></dl><div class="alert-strip info mt-16">${icon('info')}<div><span class="mono">DATABASE_URL</span> diinjeksi otomatis ke lingkungan aplikasi.</div></div><a class="btn mt-16" href="#/databases/${db.id}">${icon('code')}Buka editor database</a>`:emptyState('Belum ada database','Hubungkan aplikasi ke PostgreSQL, MySQL, atau SQLite lewat menu Database.','<a class="btn primary sm" href="#/databases">Buka database</a>','database')}</div></section>
  <section class="card"><div class="card-head"><h3>${icon('redis')}Redis ACL</h3>${r?badgeText('green','Terhubung'):''}</div><div class="card-body">${r?`<dl class="kv"><dt>ACL user</dt><dd class="mono">${esc(r.aclUser)}</dd><dt>Prefix</dt><dd class="mono">${esc(r.prefix)}*</dd><dt>Dibuat</dt><dd>${fmtDate(r.createdAt)}</dd></dl><div class="alert-strip info mt-16">${icon('lock')}<div>Password ACL hanya diinjeksi lewat <span class="mono">REDIS_URL</span>; tidak dapat ditampilkan kembali.</div></div><a class="btn mt-16" href="#/redis">${icon('redis')}Kelola Redis</a>`:emptyState('Belum ada alokasi Redis','Buat user ACL dengan prefix terisolasi untuk aplikasi ini.','<a class="btn primary sm" href="#/redis">Buka Redis</a>','redis')}</div></section></div>`;
}
function renderAppCron(c,id,a){
  c.innerHTML=`<section class="card"><div class="card-head"><div><h3>${icon('clock')}Cron aplikasi</h3><div class="hint">Perintah dijalankan di folder rilis aktif.</div></div><a class="btn sm primary" href="#/cron?app=${id}">${icon('plus')}Tambah cron</a></div><div id="app-cron-list">${loadingBlock()}</div></section>`;
  api('/api/cron').then(d=>{ const jobs=(d.jobs||[]).filter(j=>j.appId===id); const box=$('#app-cron-list',c); box.innerHTML=jobs.length?`<div class="table-wrap"><table><thead><tr><th>Nama</th><th>Jadwal</th><th>Status</th><th>Terakhir</th><th></th></tr></thead><tbody>${jobs.map(j=>`<tr><td><div class="row-title">${esc(j.name)}</div><div class="row-sub mono">${esc(j.command)}</div></td><td><span class="mono">${esc(j.schedule)}</span><div class="cron-human">${esc(j.human)}</div></td><td>${j.enabled?badgeText('green','Aktif'):badgeText('gray','Nonaktif')}</td><td>${j.lastStatus?badge(j.lastStatus):'<span class="faint">—</span>'}<div class="row-sub">${timeAgo(j.lastRun)}</div></td><td class="table-actions"><a class="btn-icon" href="#/cron?job=${j.id}">${icon('edit')}</a></td></tr>`).join('')}</tbody></table></div>`:emptyState('Belum ada cron','Tambahkan cron untuk tugas berkala aplikasi ini.','<a class="btn sm" href="#/cron?app='+id+'">Tambah cron</a>','clock'); }).catch(e=>{ $('#app-cron-list',c).innerHTML=errorBlock(e.message); });
}
function truncateText(s,n){ return s&&s.length>n?s.slice(0,n)+'…':s; }
Object.assign(window,{viewDashboard,viewApps,viewAppForm,viewAppDetail,openDeployLog});
