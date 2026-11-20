'use strict';
/* ---------- Backup ---------- */
async function viewBackup(el) {
  setPageTitle('Backup');
  viewLoading(el);
  const qp=new URLSearchParams(location.hash.split('?')[1]||'');
  if(qp.get('google')) setTimeout(()=>toast(qp.get('google')==='connected'?'Google Drive terhubung':'Koneksi Google gagal',qp.get('reason')||'',qp.get('google')==='connected'?'success':'error'),300);
  let d; try{ d=await api('/api/backups'); }catch(e){el.innerHTML=errorBlock(e.message);return;}
  const list=d.backups||[], cfg=d.config||{}, drive=cfg.drive||{};
  el.innerHTML=`${pageHead('Backup','Backup lokal + Google Drive (scope drive.file). Retensi harian dan mingguan otomatis.',`<button class="btn sm primary" id="backup-run">${icon('archive')}Backup sekarang</button>`)}
  <div class="grid g4 mb-16">
    ${statCard('Backup tersimpan',list.filter(b=>b.status==='berhasil').length,`${list.filter(b=>b.status==='gagal').length} gagal`,'archive')}
    ${statCard('Ukuran terakhir',fmtBytes(list[0]?.totalSize||0),list[0]?timeAgo(list[0].startedAt):'Belum ada backup','hardDrive','info')}
    ${statCard('Jadwal',cfg.enabled?badgeText('green','Aktif'):badgeText('gray','Nonaktif'),`<span class="mono">${esc(cfg.schedule||'0 2 * * *')}</span> UTC`,'clock',cfg.enabled?'':'warn')}
    ${statCard('Google Drive',drive.connected?badgeText('green','Terhubung'):drive.configured?badgeText('amber','Perlu otorisasi'):badgeText('gray','Belum dikonfigurasi'),esc(drive.rootFolder||'SatriaPanel-Backup'),'cloud',drive.connected?'':'warn')}
  </div>
  <div class="split mb-16">
    <section class="card"><div class="card-head"><div><h3>${icon('settings')}Konfigurasi backup</h3><div class="hint">Backup menyertakan database panel, secret key, env aplikasi, folder persistent, database terkelola, dan konfigurasi cloudflared.</div></div></div><form id="backup-form" class="card-body">
      ${switchRow('Backup otomatis','Jalankan sesuai jadwal cron di bawah.','enabled',!!cfg.enabled)}
      <div class="form-grid mt-16">
        ${field('Jadwal (UTC)','<input class="input mono" name="schedule" value="'+attr(cfg.schedule||'0 2 * * *')+'">','Format cron 5 kolom. Contoh 0 2 * * * = 02:00 UTC setiap hari.')}
        ${field('Simpan harian','<input class="input mono" type="number" name="retentionDaily" min="1" max="60" value="'+attr(cfg.retentionDaily||7)+'">')}
        ${field('Simpan mingguan','<input class="input mono" type="number" name="retentionWeekly" min="0" max="20" value="'+attr(cfg.retentionWeekly||4)+'">','Satu arsip per minggu ISO.')}
        <div>${switchRow('Sertakan Redis','Menyertakan dump.rdb saat backup berjalan.','includeRedis',!!cfg.includeRedis)}</div>
      </div>
      <div class="flex end mt-16"><button class="btn primary" id="backup-save">${icon('check')}Simpan konfigurasi</button></div>
    </form></section>
    <section class="card"><div class="card-head"><h3>${icon('cloud')}Google Drive</h3>${drive.connected?badgeText('green','Aktif'):badgeText('gray','Belum aktif')}</div><div class="card-body">
      <p class="small muted" style="line-height:1.65">Panel hanya meminta scope <span class="mono">drive.file</span> — tidak dapat membaca berkas lain di Drive Anda. Folder tujuan: <span class="mono">${esc(drive.rootFolder||'SatriaPanel-Backup')}/&lt;server&gt;/&lt;tanggal&gt;</span>.</p>
      ${drive.connected?`<dl class="kv mt-16"><dt>Client ID</dt><dd class="mono tiny">${esc(drive.clientId||'—')}</dd><dt>Scope</dt><dd class="mono tiny">drive.file</dd><dt>Status</dt><dd>${badgeText('green','Refresh token tersimpan')}</dd></dl><div class="flex gap-8 mt-16"><button class="btn outline" id="drive-config">Ganti kredensial</button><button class="btn danger" id="drive-disconnect">Putuskan</button></div>`:drive.configured?`<div class="alert-strip info mb-12">${icon('info')}<div>Kredensial OAuth tersimpan. Otorisasi akun Google untuk memberi akses drive.file.</div></div><button class="btn primary" id="drive-connect">${icon('external')}Otorisasi Google Drive</button><button class="btn ghost" id="drive-config">Ganti kredensial</button>`:`<div class="alert-strip info mb-12">${icon('lock')}<div>Buat OAuth Client (Web application) di Google Cloud Console, lalu tempel Client ID/Secret. Redirect URI ditampilkan saat menyimpan.</div></div><button class="btn primary" id="drive-config">${icon('settings')}Konfigurasi Drive</button>`}
      <div class="hr"></div><div class="tiny muted" style="line-height:1.6">Backup lokal disimpan di <span class="mono">data/backups/</span>. Unggahan Drive dilakukan setelah arsip lokal selesai dan diverifikasi manifest SHA-256.</div>
    </div></section>
  </div>
  <section class="card"><div class="card-head"><div><h3>${icon('archive')}Riwayat backup</h3><div class="hint">Klik untuk melihat isi arsip, mengunduh, atau restore.</div></div><button class="btn-icon sm" id="backup-refresh">${icon('refresh')}</button></div><div class="table-wrap" id="backup-table"></div></section>`;
  const table=$('#backup-table',el);
  async function runBackup(){const btn=$('#backup-run',el);btn.disabled=true;try{await api('/api/backups',{method:'POST'});toast('Backup selesai','Arsip telah dibuat');viewBackup(el);}catch(e){toast('Backup gagal',e.detail||e.message,'error');btn.disabled=false;}}
  function draw(){
    table.innerHTML=list.length?`<table><thead><tr><th>#</th><th>Waktu</th><th>Trigger</th><th>Status</th><th>Isi</th><th>Ukuran</th><th>Drive</th><th style="text-align:right">Aksi</th></tr></thead><tbody>${list.map(b=>`<tr><td class="mono">${b.id}</td><td>${fmtDate(b.startedAt)}<div class="row-sub">${dur((new Date(b.finishedAt||Date.now())-new Date(b.startedAt))/1000)}</div></td><td>${esc(b.trigger)}</td><td>${badge(b.status)}${b.error?`<div class="row-sub" title="${attr(b.error)}">${esc(b.error.slice(0,60))}</div>`:''}</td><td class="small">${esc(b.manifestSummary||'—')}</td><td>${fmtBytes(b.totalSize)}</td><td>${b.driveFolder?`<span class="badge ${b.driveFolder.startsWith('gagal')?'amber':'green'}">${b.driveFolder.startsWith('gagal')?'Gagal':'Tersimpan'}</span>`:'<span class="faint">Lokal</span>'}</td><td><div class="table-actions"><button class="btn-icon" data-contents="${b.id}" title="Isi arsip">${icon('eye')}</button>${b.localPath?`<button class="btn-icon" data-dl="${b.id}" title="Unduh">${icon('download')}</button>`:''}<button class="btn-icon" data-restore="${b.id}" title="Restore">${icon('rotate')}</button><button class="btn-icon danger" data-del="${b.id}" title="Hapus">${icon('trash')}</button></div></td></tr>`).join('')}</tbody></table>`:emptyState('Belum ada backup','Jalankan backup pertama untuk mengamankan panel dan aplikasi.','<button class="btn sm primary" id="backup-empty">Backup sekarang</button>','archive');
    $('#backup-empty',table)?.addEventListener('click',runBackup);
    table.querySelectorAll('[data-contents]').forEach(b=>b.onclick=()=>openBackupContents(b.dataset.contents));
    table.querySelectorAll('[data-dl]').forEach(b=>b.onclick=()=>apiDownload('/api/backups/'+b.dataset.dl+'/download','backup.zip').catch(e=>toast('Unduhan gagal',e.message,'error')));
    table.querySelectorAll('[data-restore]').forEach(b=>b.onclick=()=>openBackupRestore(b.dataset.restore,()=>viewBackup(el)));
    table.querySelectorAll('[data-del]').forEach(b=>b.onclick=async()=>{if(await confirmDialog({title:'Hapus backup?',message:'Arsip lokal dan riwayatnya akan dihapus. Salinan di Drive tidak dihapus.',typed:'HAPUS',danger:true,confirmLabel:'Hapus backup'})){try{await api('/api/backups/'+b.dataset.del,{method:'DELETE'});toast('Backup dihapus');viewBackup(el);}catch(e){toast('Gagal',e.message,'error');}}});
  }
  draw();
  $('#backup-run',el).onclick=runBackup; $('#backup-refresh',el).onclick=()=>viewBackup(el);
  $('#backup-save',el).onclick=async()=>{const f=$('#backup-form',el);const body={enabled:!!f.enabled.checked,schedule:f.schedule.value,retentionDaily:Number(f.retentionDaily.value),retentionWeekly:Number(f.retentionWeekly.value),includeRedis:!!f.includeRedis.checked};const b=$('#backup-save',el);b.disabled=true;try{await api('/api/backup-config',{method:'PUT',body});toast('Konfigurasi disimpan');viewBackup(el);}catch(e){toast('Gagal menyimpan',e.detail||e.message,'error');b.disabled=false;}};
  $('#drive-config',el)?.addEventListener('click',()=>openDriveConfig(()=>viewBackup(el)));
  $('#drive-connect',el)?.addEventListener('click',async()=>{try{const d=await api('/api/integrations/google/auth');location.href=d.url;}catch(e){toast('Tidak dapat memulai OAuth',e.detail||e.message,'error');}});
  $('#drive-disconnect',el)?.addEventListener('click',async()=>{if(await confirmDialog({title:'Putuskan Google Drive?',message:'Refresh token akan dihapus. Backup lokal tetap berjalan.',typed:'PUTUSKAN',danger:true,confirmLabel:'Putuskan'})){try{await api('/api/integrations/google',{method:'PUT',body:{clientId:'',clientSecret:'',disconnect:true}});toast('Google Drive diputus');viewBackup(el);}catch(e){toast('Gagal',e.message,'error');}}});
}
function openDriveConfig(done){
  api('/api/integrations/google').then(st=>{
    const m=modal({title:'Konfigurasi Google Drive',subtitle:'OAuth Client tipe Web application · scope drive.file.',size:'lg',body:`<div class="alert-strip info mb-16">${icon('info')}<div>Redirect URI yang harus didaftarkan di Google Cloud Console:<div class="injected mt-8"><span class="copy-text" id="drive-redirect">${esc(location.origin+'/api/integrations/google/callback')}</span><button class="btn-icon" data-copy="${attr(location.origin+'/api/integrations/google/callback')}">${icon('copy')}</button></div></div></div><form id="drive-form"><div class="form-grid">${field('Client ID','<input class="input mono" name="clientId" value="'+attr(st.clientId||'')+'" placeholder="xxxxx.apps.googleusercontent.com">','',true,true)}${field('Client Secret','<input class="input" type="password" name="clientSecret" placeholder="'+(st.configured?'•••• tersimpan':'GOCSPX-...')+'">','Biarkan kosong untuk memakai secret lama.',true)}</div></form>`,footer:`<button class="btn ghost" data-close>Batal</button><button class="btn primary" id="drive-save">${icon('check')}Simpan kredensial</button>`});
    m.overlay.querySelectorAll('[data-copy]').forEach(b=>b.onclick=()=>copyText(b.dataset.copy));
    $('#drive-save',m.overlay).onclick=async()=>{const f=$('#drive-form',m.overlay); const secret=f.clientSecret.value; if(!f.clientId.value||(!secret&&!st.configured)){toast('Client ID dan Secret wajib','','error');return;} try{await api('/api/integrations/google',{method:'PUT',body:{clientId:f.clientId.value,clientSecret:secret}});m.close();toast('Kredensial disimpan','Klik Otorisasi Google Drive untuk menghubungkan akun.');done&&done();}catch(e){toast('Gagal',e.message,'error');}};
  }).catch(e=>toast('Gagal memuat',e.message,'error'));
}
async function openBackupContents(id){
  const dr=drawer({title:'Isi backup',subtitle:'Verifikasi manifest dan daftar berkas.',wide:true,body:loadingBlock()});
  try{ const d=await api('/api/backups/'+id+'/contents'); const mf=d.manifest||{}; const items=d.items||[];
    dr.body.innerHTML=`<dl class="kv mb-16"><dt>Dibuat</dt><dd>${esc(mf.createdAt||'—')}</dd><dt>Server</dt><dd>${esc(mf.server||'—')}</dd><dt>Versi panel</dt><dd>${esc(mf.version||'—')}</dd><dt>Item terverifikasi</dt><dd>${(mf.items||[]).length}</dd><dt>Lokasi</dt><dd class="mono tiny">${esc(d.backup?.localPath||'—')}</dd></dl><div class="small strong mb-8">${items.length} berkas dalam arsip</div><div class="table-wrap" style="max-height:56vh;overflow:auto"><table><thead><tr><th>Path</th><th>Ukuran</th><th>Status</th></tr></thead><tbody>${items.map(i=>{const mi=(mf.items||[]).find(x=>x.path===i.name);return `<tr><td class="mono tiny">${esc(i.name)}</td><td>${fmtBytes(i.size)}</td><td>${mi?'<span class="badge green">SHA-256</span>':'<span class="badge gray">metadata</span>'}</td></tr>`;}).join('')}</tbody></table></div>`;
  }catch(e){dr.body.innerHTML=errorBlock(e.detail||e.message);}
}
async function openBackupRestore(id,done){
  let apps={apps:[]}, dbs={databases:[]};
  try{ [apps,dbs]=await Promise.all([api('/api/apps'),api('/api/databases')]); }catch(_){}
  const m=modal({title:'Restore dari backup',subtitle:'Verifikasi checksum dilakukan sebelum menulis data.',size:'lg',body:`<form id="restore-form"><div class="form-grid">${field('Cakupan restore','<select class="select" name="scope"><option value="panel">Panel (panel.db + secret.key) — disiapkan, perlu restart</option><option value="app">Data persistent aplikasi</option><option value="database">Database SQLite</option></select>','',true)}
    <div id="restore-target"></div></div><div class="alert-strip danger mt-16">${icon('alert')}<div>Restore panel tidak langsung menimpa panel.db yang sedang berjalan; berkas diletakkan di folder <span class="mono">data/restore</span> untuk dipasang saat panel berhenti. Restore database SQLite menimpa berkas aktif setelah safety backup.</div></div></form>`,footer:`<button class="btn ghost" data-close>Batal</button><button class="btn danger" id="restore-go">${icon('rotate')}Restore</button>`});
  const form=$('#restore-form',m.overlay), target=$('#restore-target',m.overlay);
  const sync=()=>{ const s=form.scope.value; if(s==='app')target.innerHTML=field('Aplikasi','<select class="select" name="appId">'+(apps.apps||[]).map(a=>`<option value="${a.id}">${esc(a.name)}</option>`).join('')+'</select>'); else if(s==='database')target.innerHTML=field('Database SQLite','<select class="select" name="dbId">'+(dbs.databases||[]).filter(x=>x.engine==='sqlite').map(x=>`<option value="${x.id}">${esc(x.name)}</option>`).join('')+'</select>','Database server selain SQLite tidak didukung dari arsip panel.'); else target.innerHTML=''; }; form.scope.onchange=sync;sync();
  $('#restore-go',m.overlay).onclick=async()=>{ const scope=form.scope.value; const body={confirm:'PULIHKAN',scope:scope==='app'?'app:'+form.appId.value:scope==='database'?'database:'+form.dbId.value:'panel'}; if(!await confirmDialog({title:'Konfirmasi restore',message:'Restore akan mengubah data. Pastikan Anda memiliki backup terbaru.',typed:'PULIHKAN',danger:true,confirmLabel:'Mulai restore'}))return; try{const r=await api('/api/backups/'+id+'/restore',{method:'POST',body});m.close();toast('Restore selesai',r.message||'');done&&done();}catch(e){toast('Restore gagal',e.detail||e.message,'error');} };
}

/* ---------- Cloudflared & Port ---------- */
async function viewCloudflare(el) {
  setPageTitle('Cloudflared & Port');
  viewLoading(el);
  let d; try{ d=await api('/api/cloudflare'); }catch(e){el.innerHTML=errorBlock(e.message);return;}
  const maps=d.mappings||[], ports=d.ports||[];
  el.innerHTML=`${pageHead('Cloudflared & Port','Kelola hostname publik, ingress Cloudflare Tunnel, dan pemetaan port aplikasi.',`<button class="btn sm outline" id="cf-test">${icon('wifi')}Tes token</button><button class="btn sm primary" id="cf-apply">${icon('cloud')}Terapkan ingress</button>`)}
  <div class="grid g4 mb-16">
    ${statCard('Cloudflare API',d.configured?badgeText('green','Terhubung'):badgeText('gray','Belum dikonfigurasi'),esc(d.zoneName||'API token belum diisi'),'cloud')}
    ${statCard('Tunnel',d.tunnelId?`<span class="mono" style="font-size:13px">${esc(d.tunnelId.slice(0,16))}…</span>`:'—','Cloudflare Tunnel','route',d.tunnelId?'':'warn')}
    ${statCard('Hostname panel',`<span style="font-size:18px">${esc(d.panelHostname||'—')}</span>`,d.configExists?'config.yml terdeteksi':'config.yml belum ada','globe','info')}
    ${statCard('Port terpetakan',ports.length,'Pool 3100–3999','server','warn')}
  </div>
  ${!d.configured?`<div class="alert-strip mb-16">${icon('info')}<div><b>Integrasi Cloudflare belum aktif.</b> Hostname dan pemetaan tetap dapat dilihat. Isi API Token, Zone ID, dan Tunnel ID untuk sinkronisasi DNS. Panel tidak akan menulis konfigurasi cloudflared tanpa tombol Terapkan.</div></div>`:''}
  <div class="stack-md mb-16"><section class="card"><div class="card-head"><div><h3>${icon('settings')}Konfigurasi</h3><div class="hint">API Token butuh izin Zone:DNS:Edit dan Account:Cloudflare Tunnel:Edit.</div></div></div><form id="cf-form" class="card-body"><div class="form-grid">
    ${field('API Token','<input class="input" type="password" name="apiToken" placeholder="'+(d.configured?'•••• tersimpan':'token API')+'">','Biarkan kosong dan pertahankan untuk memakai token lama.',true)}
    ${field('','<label class="check-row"><input type="checkbox" name="keepToken" checked><span class="small">Pertahankan token tersimpan</span></label>','',false)}
    ${field('Zone ID','<input class="input mono" name="zoneId" value="'+attr(d.zoneId||'')+'">')}
    ${field('Nama zona','<input class="input mono" name="zoneName" value="'+attr(d.zoneName||'')+'" placeholder="satriyadi.com">')}
    ${field('Account ID','<input class="input mono" name="accountId" value="'+attr(d.accountId||'')+'">')}
    ${field('Tunnel ID','<input class="input mono" name="tunnelId" value="'+attr(d.tunnelId||'')+'">')}
    ${field('Hostname panel','<input class="input mono" name="panelHostname" value="'+attr(d.panelHostname||'')+'">')}
    ${field('Config cloudflared','<input class="input mono" name="configPath" value="'+attr(d.configPath||'')+'">','Path di server Ubuntu, default /etc/cloudflared/config.yml.')}
    ${field('Nama service','<input class="input mono" name="service" value="'+attr(d.service||'cloudflared')+'">','Dipanggil untuk reload setelah apply.')}
    <div class="full">${switchRow('Sinkronisasi otomatis DNS','Hostname aplikasi langsung aktif: CNAME dibuat/diperbarui dan konfigurasi tunnel diterapkan ke Cloudflare otomatis saat aplikasi disimpan atau dihapus.','autoSync',!!d.autoSync)}</div>
  </div><div class="flex end mt-16"><button class="btn primary" id="cf-save">${icon('check')}Simpan konfigurasi</button></div></form></section>
  <section class="card"><div class="card-head"><h3>${icon('route')}Hostname & aplikasi</h3></div><div class="table-wrap">${maps.length?`<table><thead><tr><th>Aplikasi</th><th>Hostname</th><th>Port</th><th>DNS</th><th>Status</th></tr></thead><tbody>${maps.map(m=>`<tr><td><a href="#/apps/${m.appId}">${esc(m.name)}</a></td><td class="mono small">${m.hostname?esc(m.hostname):'<span class="faint">—</span>'}</td><td><span class="port-chip">${m.port}</span></td><td>${m.dnsRecordId?'<span class="badge green">CNAME</span>':'<span class="faint small">—</span>'}</td><td>${badge(m.status)}</td></tr>`).join('')}</tbody></table>`:emptyState('Belum ada pemetaan','Tambahkan hostname pada aplikasi untuk mengarahkan domain ke port lokal.','<a class="btn sm" href="#/apps">Buka aplikasi</a>','globe')}</div></section></div>
  <section class="card"><div class="card-head"><div><h3>${icon('server')}Port pool</h3><div class="hint">Port unik per aplikasi, hanya didengarkan di 127.0.0.1.</div></div><a class="btn sm outline" href="#/apps/new">${icon('plus')}Aplikasi baru</a></div><div class="table-wrap"><table><thead><tr><th>Port</th><th>Aplikasi</th><th>Hostname</th><th>Status</th><th>Kesehatan</th></tr></thead><tbody>${ports.length?ports.map(p=>`<tr><td><span class="port-chip">${p.port}</span></td><td>${esc(p.name)}</td><td class="mono small">${esc(p.hostname||'—')}</td><td>${badge(p.status)}</td><td>${badge(p.health)}</td></tr>`).join(''):`<tr><td colspan="5">${emptyState('Belum ada port terpakai','Port dialokasikan otomatis saat aplikasi dibuat.','','server')}</td></tr>`}</tbody></table></div></section>`;
  $('#cf-save',el).onclick=async()=>{const f=$('#cf-form',el);const body={apiToken:f.apiToken.value,keepToken:!!f.keepToken.checked,zoneId:f.zoneId.value,zoneName:f.zoneName.value,accountId:f.accountId.value,tunnelId:f.tunnelId.value,panelHostname:f.panelHostname.value,configPath:f.configPath.value,service:f.service.value,autoSync:!!f.autoSync.checked}; const b=$('#cf-save',el);b.disabled=true;try{await api('/api/cloudflare',{method:'PUT',body});toast('Konfigurasi disimpan');viewCloudflare(el);}catch(e){toast('Gagal menyimpan',e.detail||e.message,'error');b.disabled=false;}};
  $('#cf-test',el).onclick=async()=>{const b=$('#cf-test',el);b.disabled=true;try{const r=await api('/api/cloudflare/test',{method:'POST'});toast('Token valid',`Status ${r.status}${r.tunnel?' · tunnel '+r.tunnel:''}`);}catch(e){toast('Tes gagal',e.detail||e.message,'error');}finally{b.disabled=false;}};
  $('#cf-apply',el).onclick=async()=>{ if(!await confirmDialog({title:'Terapkan konfigurasi ingress?',message:'Panel akan menerapkan konfigurasi ke tunnel Cloudflare lewat API (aktif otomatis, tanpa restart cloudflared), menyinkronkan DNS semua hostname, dan menyimpan salinan lokal <span class="mono">'+esc(d.configPath||'')+'</span> beserta cadangan .bak-nya. Pastikan Anda memahami perubahan ini.',typed:'TERAPKAN',confirmLabel:'Terapkan',danger:true}))return; const b=$('#cf-apply',el);b.disabled=true;try{const r=await api('/api/cloudflare/apply',{method:'POST',body:{confirm:'TERAPKAN'}});toast('Konfigurasi diterapkan',r.validation);const m=modal({title:'Konfigurasi ingress diterapkan',subtitle:esc(r.path),size:'lg',body:`<div class="alert-strip info mb-12">${icon('check')}<div>${esc(r.validation)} · DNS tersinkron: ${r.dnsSynced}${r.dnsRemoved?` · DNS lama dibersihkan: ${r.dnsRemoved}`:''}${r.dnsErrors?` · Gagal: ${esc(r.dnsErrors.join('; '))}`:''}</div></div><pre class="code-block">${esc(r.config)}</pre>`,footer:`<button class="btn" data-close>Tutup</button>`});}catch(e){toast('Apply gagal',e.detail||e.message,'error');b.disabled=false;}};
}

/* ---------- Notifikasi ---------- */
async function viewNotifications(el) {
  setPageTitle('Notifikasi');
  viewLoading(el);
  let d,ch; try{ [d,ch]=await Promise.all([api('/api/notifications?limit=200'),api('/api/notification-channels')]); }catch(e){el.innerHTML=errorBlock(e.message);return;}
  let notes=d.notifications||[]; const rules=ch.rules||{}; const tg=ch.telegram||{},wa=ch.whatsapp||{};
  el.innerHTML=`${pageHead('Notifikasi','Kabar panel serta kanal keluar Telegram dan WhatsApp.',`<button class="btn sm outline" id="mark-all">${icon('check')}Tandai semua dibaca</button><button class="btn sm" id="notif-test">${icon('send')}Tes</button>`)}
  <div class="grid g4 mb-16">
    ${statCard('Belum dibaca',d.unread||0,'Klik notifikasi untuk membaca','bell',d.unread?'warn':'')}
    ${statCard('Telegram',tg.configured?badgeText('green','Aktif'):badgeText('gray','Belum diatur'),tg.chatId?'Chat '+esc(tg.chatId):'Bot token + chat ID','send')}
    ${statCard('WhatsApp',wa.configured?badgeText('green','Aktif'):badgeText('gray','Belum diatur'),wa.url?esc(wa.url):'Gateway HTTP','send','info')}
    ${statCard('Aturan aktif',Object.values(rules).filter(Boolean).length,`${Object.keys(rules).length} kategori`,'shield')}
  </div>
  <div class="split"><section class="card"><div class="card-head"><div><h3>${icon('bell')}Kotak masuk</h3><div class="hint">Notifikasi panel tersimpan di SQLite.</div></div><div class="toolbar"><select class="select" id="notif-filter" style="width:auto"><option value="">Semua</option><option value="unread">Belum dibaca</option><option value="deploy">Deploy</option><option value="alert">Alert</option><option value="cron">Cron</option><option value="backup">Backup</option><option value="security">Keamanan</option></select><button class="btn-icon sm" id="notif-refresh">${icon('refresh')}</button></div></div><div id="notif-list"></div></section>
  <div class="stack-md"><section class="card"><div class="card-head"><h3>${icon('settings')}Kanal keluar</h3></div><div class="card-body stack-sm">
    <div class="list-row"><span class="avatar-sm">${icon('send')}</span><div class="grow"><div class="small strong">Telegram</div><div class="tiny muted">${tg.configured?'Terhubung ke chat '+esc(tg.chatId):'Belum dikonfigurasi'}</div></div><button class="btn sm" data-channel="telegram">Atur</button></div>
    <div class="list-row"><span class="avatar-sm">${icon('send')}</span><div class="grow"><div class="small strong">WhatsApp gateway</div><div class="tiny muted">${wa.configured?'Terhubung':"Belum dikonfigurasi"}</div></div><button class="btn sm" data-channel="whatsapp">Atur</button></div>
    <div class="hr" style="margin:14px 0"></div>
    <div class="small strong mb-8">Aturan notifikasi</div>
    ${Object.entries({deploy:'Deploy & rollback',cron:'Cron',backup:'Backup',alert:'Monitor & alert',security:'Keamanan',cloudflare:'Cloudflare'}).map(([k,t])=>`<div class="field-row"><div class="t">${t}</div><label class="switch"><input type="checkbox" data-rule="${k}" ${rules[k]?'checked':''}><span></span></label></div>`).join('')}
    <div class="alert-strip info mt-12">${icon('lock')}<div>Kredensial kanal disimpan terenkripsi dan tidak ditampilkan kembali.</div></div>
  </div></section>
  <section class="card"><div class="card-head"><h3>${icon('alert')}Tes notifikasi</h3></div><div class="card-body"><p class="small muted" style="line-height:1.6">Kirim pesan uji ke kanal yang aktif. Notifikasi panel selalu tersedia meski kanal eksternal belum diatur.</p><button class="btn block mt-12" id="test-telegram">${icon('send')}Tes Telegram</button><button class="btn block mt-8" id="test-whatsapp">${icon('send')}Tes WhatsApp</button></div></section>
  </div></div>`;
  function draw(){
    const f=$('#notif-filter',el)?.value;
    const list=notes.filter(n=>!f||f==='unread'&&!n.isRead||n.kind===f);
    $('#notif-list',el).innerHTML=list.length?list.map(n=>`<div class="list-row" data-note="${n.id}" style="align-items:flex-start;padding:13px 18px"><span class="avatar-sm" style="margin-top:2px;background:${n.isRead?'rgba(71,85,105,.2)':'rgba(34,197,94,.12)'};color:${n.isRead?'#94A3B8':'#86EFAC'}">${icon(n.kind==='alert'?'alert':n.kind==='deploy'?'zap':n.kind==='backup'?'archive':n.kind==='cron'?'clock':'bell')}</span><div class="grow"><div class="flex between gap-8"><div class="small ${n.isRead?'':'strong'}">${esc(n.title)}</div><span class="tiny muted nowrap">${timeAgo(n.createdAt)}</span></div><div class="tiny muted" style="margin-top:3px;line-height:1.55">${esc(n.body||'')}</div><div class="flex gap-8 mt-8"><span class="tag">${esc(n.kind)}</span>${!n.isRead?`<button class="link-btn" data-read="${n.id}">Tandai dibaca</button>`:''}<button class="link-btn danger" data-note-del="${n.id}">Hapus</button></div></div></div>`).join(''):emptyState('Tidak ada notifikasi','Semua sudah terbaca atau belum ada aktivitas.','','bell');
    $$('[data-read]',el).forEach(b=>b.onclick=async e=>{e.stopPropagation();await api('/api/notifications/'+b.dataset.read+'/read',{method:'POST'});b.closest('.list-row').querySelector('.small').classList.remove('strong');b.remove();state.unread=Math.max(0,state.unread-1);updateUnread();});
    $$('[data-note-del]',el).forEach(b=>b.onclick=async e=>{e.stopPropagation();await api('/api/notifications/'+b.dataset.noteDel,{method:'DELETE'});b.closest('.list-row').remove();});
  }
  draw();
  $('#notif-filter',el).onchange=draw; $('#notif-refresh',el).onclick=()=>viewNotifications(el);
  $('#mark-all',el).onclick=async()=>{await api('/api/notifications/read-all',{method:'POST'});state.unread=0;updateUnread();viewNotifications(el);};
  $$('[data-rule]',el).forEach(cb=>cb.onchange=async()=>{const r={};$$('[data-rule]',el).forEach(x=>r[x.dataset.rule]=x.checked);try{await api('/api/notifications/rules',{method:'PUT',body:r});toast('Aturan disimpan');}catch(e){toast('Gagal',e.message,'error');cb.checked=!cb.checked;}});
  $$('[data-channel]',el).forEach(b=>b.onclick=()=>openChannelConfig(b.dataset.channel,()=>viewNotifications(el)));
  $('#test-telegram',el).onclick=()=>testChannel('telegram'); $('#test-whatsapp',el).onclick=()=>testChannel('whatsapp');
  $('#notif-test',el).onclick=()=>testChannel('telegram');
}
function openChannelConfig(channel,done){
  const isTg=channel==='telegram';
  api('/api/notification-channels').then(c=>{
    const cfg=isTg?c.telegram:c.whatsapp;
    const m=modal({title:isTg?'Konfigurasi Telegram':'Konfigurasi WhatsApp',subtitle:'Kredensial disimpan terenkripsi.',body:`<form id="chan-form"><div class="form-grid">
      <div class="full">${field(isTg?'Bot token':'Token gateway','<input class="input" type="password" name="token" placeholder="'+(cfg.hasToken?'•••• tersimpan':'isi token')+'">')}</div>
      ${isTg?field('Chat ID','<input class="input mono" name="chatId" value="'+attr(cfg.chatId||'')+'" placeholder="-100123456789">','ID chat/grup tujuan.'):field('URL API','<input class="input mono" name="url" value="'+attr(cfg.url||'')+'" placeholder="https://gateway.example.com/send">','Menerima POST JSON {target,message}.')}
      ${!isTg?field('Nomor tujuan','<input class="input mono" name="phone" value="'+attr(cfg.phone||'')+'" placeholder="62812...">'):''}
      <div class="full">${field('','<label class="check-row"><input type="checkbox" name="keep" checked><span class="small">Pertahankan token tersimpan</span></label>','',true)}</div>
    </div><div class="flex between mt-16"><button type="button" class="btn danger" id="chan-clear">Hapus kredensial</button><span class="tiny muted">Tes mengirim pesan nyata.</span></div></form>`,footer:`<button class="btn ghost" data-close>Batal</button><button class="btn primary" id="chan-save">${icon('check')}Simpan</button>`});
    $('#chan-save',m.overlay).onclick=async()=>{const f=$('#chan-form',m.overlay); const body={channel,keep:!!f.keep.checked,token:f.token.value}; if(isTg)body.chatId=f.chatId.value; else {body.url=f.url.value;body.phone=f.phone.value;} try{await api('/api/notification-channels',{method:'PUT',body});m.close();toast('Kanal disimpan');done&&done();}catch(e){toast('Gagal menyimpan',e.detail||e.message,'error');}};
    $('#chan-clear',m.overlay).onclick=async()=>{if(await confirmDialog({title:'Hapus kredensial kanal?',message:'Notifikasi eksternal berhenti sampai dikonfigurasi ulang.',typed:'HAPUS',danger:true,confirmLabel:'Hapus'})){try{await api('/api/notification-channels',{method:'PUT',body:{channel,clear:true}});m.close();toast('Kredensial dihapus');done&&done();}catch(e){toast('Gagal',e.message,'error');}}};
  });
}
async function testChannel(channel){
  try{ await api('/api/notification-channels/test',{method:'POST',body:{channel}}); toast('Pesan uji terkirim',channel); }catch(e){toast('Tes gagal',e.detail||e.message,'error');}
}

/* ---------- Audit ---------- */
async function viewAudit(el) {
  setPageTitle('Audit');
  viewLoading(el);
  let offset=0; const limit=50;
  el.innerHTML=`${pageHead('Audit','Jejak aktivitas panel dan perubahan konfigurasi. Waktu ditampilkan dalam zona server.',`<button class="btn sm outline" id="audit-export">${icon('download')}Ekspor CSV</button>`)}
  <div class="card mb-16"><div class="card-body" style="padding:12px 14px"><div class="toolbar">
    <div class="search"><input class="input" id="audit-q" placeholder="Cari ringkasan, aksi, objek…"></div>
    <select class="select" id="audit-action" style="width:auto;min-width:170px"><option value="">Semua aksi</option></select>
    <button class="btn sm outline" id="audit-refresh">${icon('refresh')}Terapkan</button>
    <span class="tiny muted" id="audit-total" style="margin-left:auto"></span>
  </div></div></div>
  <div class="card"><div class="table-wrap" id="audit-table">${loadingBlock()}</div><div class="card-foot"><span class="tiny muted">Audit log tidak dihapus otomatis.</span><div class="flex gap-8"><button class="btn sm" id="audit-prev" disabled>${icon('chevronLeft')}Sebelumnya</button><button class="btn sm" id="audit-next">Berikutnya${icon('chevronRight')}</button></div></div></div>`;
  const load=async()=>{
    const q=$('#audit-q',el).value.trim(), action=$('#audit-action',el).value;
    $('#audit-table',el).innerHTML=loadingBlock('Memuat audit…');
    try{ const d=await api('/api/audit'+qs({q,action,limit,offset})); const logs=d.logs||[];
      $('#audit-total',el).textContent=`${fmtNum(d.total)} catatan`;
      if(d.actions&&$('#audit-action',el).options.length<=1)d.actions.forEach(a=>$('#audit-action',el).insertAdjacentHTML('beforeend',`<option value="${attr(a.action)}">${esc(a.action)} (${a.count})</option>`));
      $('#audit-table',el).innerHTML=logs.length?`<table><thead><tr><th>Waktu</th><th>Aktor</th><th>Aksi</th><th>Objek</th><th>Sumber</th><th>Ringkasan</th><th>IP</th></tr></thead><tbody>${logs.map(l=>`<tr class="clickable" data-audit="${l.id}"><td class="nowrap">${fmtDate(l.createdAt)}</td><td><span class="tag">${esc(l.actor)}</span></td><td><span class="mono small">${esc(l.action)}</span></td><td>${esc(l.objectType||'—')}${l.objectId?` <span class="mono tiny">#${esc(l.objectId)}</span>`:''}</td><td>${(l.source||'panel')==='mcp'?badgeText('blue','MCP'):badgeText('gray','Panel')}${l.tokenName?`<div class="tiny muted mono mt-4">${esc(l.tokenName)}</div>`:''}</td><td class="small" style="max-width:420px">${esc(l.summary)}</td><td class="mono tiny">${esc(l.ip||'—')}</td></tr>`).join('')}</tbody></table>`:emptyState('Tidak ada catatan','Ubah filter atau lakukan aktivitas di panel.','','shield');
      $$('[data-audit]',el).forEach(r=>r.onclick=()=>{ const l=logs.find(x=>String(x.id)===r.dataset.audit); openAuditDetail(l); });
      $('#audit-prev',el).disabled=offset===0; $('#audit-next',el).disabled=offset+limit>=d.total;
    }catch(e){ $('#audit-table',el).innerHTML=errorBlock(e.message); }
  };
  $('#audit-refresh',el).onclick=()=>{offset=0;load();};
  $('#audit-q',el).onkeydown=e=>{if(e.key==='Enter'){offset=0;load();}};
  $('#audit-action',el).onchange=()=>{offset=0;load();};
  $('#audit-prev',el).onclick=()=>{offset=Math.max(0,offset-limit);load();};
  $('#audit-next',el).onclick=()=>{offset+=limit;load();};
  $('#audit-export',el).onclick=()=>apiDownload('/api/audit?export=1&limit=500','satriapanel-audit.csv').catch(e=>toast('Ekspor gagal',e.message,'error'));
  load();
}
function openAuditDetail(l){
  modal({title:'Detail audit #'+l.id,subtitle:esc(l.action),body:`<dl class="kv"><dt>Waktu</dt><dd>${fmtDate(l.createdAt)}</dd><dt>Aktor</dt><dd>${esc(l.actor)}</dd><dt>Aksi</dt><dd class="mono">${esc(l.action)}</dd><dt>Objek</dt><dd>${esc(l.objectType||'—')} ${esc(l.objectId||'')}</dd><dt>Sumber</dt><dd>${esc((l.source||'panel')==='mcp'?'MCP':'Panel')}</dd>${l.tokenName?`<dt>Token MCP</dt><dd class="mono">${esc(l.tokenName)}</dd>`:''}<dt>IP</dt><dd class="mono">${esc(l.ip||'—')}</dd></dl><div class="hr"></div><p class="small" style="line-height:1.65">${esc(l.summary)}</p>`,footer:`<button class="btn" data-close>Tutup</button>`});
}

/* ---------- Pengaturan ---------- */
async function viewSettings(el) {
  setPageTitle('Pengaturan');
  viewLoading(el);
  let d; try{ d=await api('/api/settings'); }catch(e){el.innerHTML=errorBlock(e.message);return;}
  const g=d.general||{}, dep=d.deploy||{}, sec=d.security||{}, mon=d.monitor||{}, db=d.database||{};
  const tab=(location.hash.split('?')[1]||'tab=umum').replace('tab=','');
  const tabs=[['umum','Umum','settings'],['deploy','Deploy & port','route'],['keamanan','Keamanan','shield'],['monitor','Monitor','gauge'],['integrations','Integrasi','link'],['mcp','MCP','terminal'],['sesi','Sesi','key']];
  const active=tabs.some(t=>t[0]===tab)?tab:'umum';
  el.innerHTML=`${pageHead('Pengaturan','Konfigurasi server, deployment, keamanan, monitoring, dan integrasi.',`<span class="badge gray">SatriaPanel v${esc(g.version||'0.3.0')}</span>`)}
  <div class="tabs mb-16">${tabs.map(([k,t,ic])=>`<a class="tab ${active===k?'active':''}" href="#/settings?tab=${k}">${icon(ic)}${t}</a>`).join('')}</div><div id="settings-main"></div>`;
  const main=$('#settings-main',el);
  if(active==='umum'){
    main.innerHTML=`<div class="split"><section class="card"><div class="card-head"><h3>${icon('server')}Identitas panel</h3></div><form id="set-general" class="card-body"><div class="form-grid">
      ${field('Nama server','<input class="input" name="serverName" value="'+attr(g.serverName||'')+'">','Tampil di sidebar dan notifikasi.',false,true)}
      ${field('Hostname panel','<input class="input mono" name="panelHostname" value="'+attr(g.panelHostname||'')+'">','Domain publik panel via Cloudflare Tunnel.')}
      ${field('Base URL','<input class="input mono" name="baseUrl" value="'+attr(g.baseUrl||'')+'" placeholder="https://panel.satriyadi.com">','Dipakai untuk URL webhook GitHub dan OAuth.')}
      ${field('Zona waktu','<select class="select" name="timezone">'+(g.timezone&&!['UTC','Asia/Jakarta','Asia/Makassar','Asia/Jayapura','Asia/Singapore','Asia/Tokyo','Europe/London','America/New_York'].includes(g.timezone)?`<option selected value="${attr(g.timezone)}">Lokal server (${esc(g.timezone)})</option>`:'')+['UTC','Asia/Jakarta','Asia/Makassar','Asia/Jayapura','Asia/Singapore','Asia/Tokyo','Europe/London','America/New_York'].map(z=>`<option ${g.timezone===z?'selected':''}>${z}</option>`).join('')+'</select>','Waktu disimpan UTC, ditampilkan dalam zona ini.')}
    </div><div class="flex end mt-16"><button class="btn primary" id="save-general">${icon('check')}Simpan</button></div></form></section>
    <section class="card"><div class="card-head"><h3>${icon('info')}Tentang sistem</h3></div><div class="card-body"><dl class="kv"><dt>Versi</dt><dd class="mono">${esc(g.version||'0.3.0')}</dd><dt>Go</dt><dd class="mono">${esc(g.goVersion||'—')}</dd><dt>OS / arsitektur</dt><dd>${esc(g.os||'')} / ${esc(g.arch||'')}</dd><dt>Database</dt><dd>SQLite (panel.db)</dd><dt>Enkripsi</dt><dd>AES-256-GCM · secret.key</dd><dt>PID</dt><dd class="mono">—</dd></dl><div class="alert-strip info mt-16">${icon('lock')}<div>Kredensial aplikasi selalu disamarkan; tidak ada endpoint reveal di panel.</div></div></div></section></div>`;
    $('#save-general',main).onclick=saveSettings;
  }
  if(active==='deploy'){
    main.innerHTML=`<div class="split"><section class="card"><div class="card-head"><h3>${icon('route')}Port & rilis</h3></div><form id="set-deploy" class="card-body"><div class="form-grid">
      ${field('Awal kolam port','<input class="input mono" type="number" name="portPoolStart" value="'+attr(dep.portPoolStart)+'">')}
      ${field('Akhir kolam port','<input class="input mono" type="number" name="portPoolEnd" value="'+attr(dep.portPoolEnd)+'">')}
      ${field('Port panel','<input class="input mono" type="number" name="panelPort" value="'+attr(dep.panelPort)+'">','Port ini tidak boleh berada dalam kolam aplikasi.')}
      ${field('Rilis yang disimpan','<input class="input mono" type="number" name="releaseKeep" min="1" max="50" value="'+attr(dep.releaseKeep)+'">','Rilis aktif selalu dipertahankan.')}
      ${field('Retensi log (hari)','<input class="input mono" type="number" name="logRetentionDays" value="'+attr(dep.logRetentionDays)+'">')}
      ${field('Retensi cron (hari)','<input class="input mono" type="number" name="cronRetentionDays" value="'+attr(dep.cronRetentionDays)+'">')}
      <div class="full">${switchRow('Izinkan provisioning database','Panel dapat membuat database/user PostgreSQL/MySQL lewat sudo lokal. Jika dimatikan, hanya koneksi ke database yang sudah ada.','allowProvision',!!db.allowProvision)}</div>
    </div><div class="alert-strip info mt-16">${icon('info')}<div>Deploy memakai folder rilis baru. Bila health check gagal, panel otomatis mengaktifkan rilis sebelumnya.</div></div><div class="flex end mt-16"><button class="btn primary" id="save-deploy">${icon('check')}Simpan</button></div></form></section>
    <section class="card"><div class="card-head"><h3>${icon('info')}Perilaku deploy</h3></div><div class="card-body"><div class="timeline"><div class="tl-item ok"><div class="tl-title">Antrean satu build</div><div class="tl-sub">Deploy berjalan serial; webhook dan manual masuk antrean.</div></div><div class="tl-item ok"><div class="tl-title">Rilis terisolasi</div><div class="tl-sub">Kode baru di folder terpisah; deploy gagal tidak menghapus rilis aktif.</div></div><div class="tl-item ok"><div class="tl-title">Auto-rollback</div><div class="tl-sub">Health check 17 detik; gagal → rilis lama dijalankan kembali.</div></div><div class="tl-item ok"><div class="tl-title">Batas 5 restart</div><div class="tl-sub">Driver direct membatasi restart otomatis lalu menandai bermasalah.</div></div></div></div></section></div>`;
    $('#save-deploy',main).onclick=saveSettings;
  }
  if(active==='keamanan'){
    main.innerHTML=`<div class="split"><section class="card"><div class="card-head"><h3>${icon('shield')}Sesi & akses</h3></div><form id="set-security" class="card-body"><div class="form-grid">
      ${field('Idle timeout sesi (menit)','<input class="input mono" type="number" name="sessionIdleMinutes" min="5" max="1440" value="'+attr(sec.sessionIdleMinutes)+'">','Sesi tidak aktif diputus otomatis.')}
    </div><div class="hr"></div><button type="button" class="btn outline" id="change-password">${icon('key')}Ubah password</button><div class="flex end mt-16"><button class="btn primary" id="save-security">${icon('check')}Simpan</button></div></form></section>
    <section class="card"><div class="card-head"><h3>${icon('lock')}Kebijakan kredensial</h3></div><div class="card-body"><div class="list-row"><span class="avatar-sm">${icon('lock')}</span><div><div class="small strong">Tidak ada reveal</div><div class="tiny muted">Password database, token, dan secret webhook tidak dapat ditampilkan.</div></div></div><div class="list-row"><span class="avatar-sm">${icon('shield')}</span><div><div class="small strong">Enkripsi at rest</div><div class="tiny muted">AES-256-GCM dengan kunci terpisah 0600.</div></div></div><div class="list-row"><span class="avatar-sm">${icon('key')}</span><div><div class="small strong">CSRF token header</div><div class="tiny muted">Semua mutasi memerlukan header X-Satria.</div></div></div><div class="list-row"><span class="avatar-sm">${icon('alert')}</span><div><div class="small strong">Rate limit login</div><div class="tiny muted">5 gagal / 15 menit per IP.</div></div></div></div></section></div>`;
    $('#change-password',main).onclick=openChangePassword;
    $('#save-security',main).onclick=saveSettings;
  }
  if(active==='monitor'){
    main.innerHTML=`<div class="split"><section class="card"><div class="card-head"><h3>${icon('gauge')}Ambang peringatan</h3></div><form id="set-monitor" class="card-body"><div class="form-grid">
      ${field('Disk (%)','<input class="input mono" type="number" name="alertDisk" min="50" max="100" value="'+attr(mon.alertDisk)+'">','Alert dikirim sekali per 30 menit per kategori.')}
      ${field('RAM (%)','<input class="input mono" type="number" name="alertRam" min="50" max="100" value="'+attr(mon.alertRam)+'">')}
      ${field('Redis memori (%)','<input class="input mono" type="number" name="alertRedis" min="50" max="100" value="'+attr(mon.alertRedis)+'">')}
    </div><div class="flex end mt-16"><button class="btn primary" id="save-monitor">${icon('check')}Simpan</button></div></form></section>
    <section class="card"><div class="card-head"><h3>${icon('activity')}Metrik</h3></div><div class="card-body"><dl class="kv"><dt>Interval</dt><dd>5 detik (live)</dd><dt>Persistensi</dt><dd>1 menit ke SQLite</dd><dt>Retensi</dt><dd>30 hari</dd><dt>Transport</dt><dd>SSE /api/monitor/stream</dd></dl><a class="btn block mt-16" href="#/dashboard">${icon('dashboard')}Buka dashboard</a></div></section></div>`;
    $('#save-monitor',main).onclick=saveSettings;
  }
  if(active==='integrations'){
    main.innerHTML=`<div class="grid g2"><section class="card"><div class="card-head"><h3>${icon('github')}GitHub</h3><span id="gh-status">Memuat…</span></div><div class="card-body"><p class="small muted" style="line-height:1.6">Personal Access Token (classic atau fine-grained) dengan izin repo dan admin:repo_hook untuk webhook. Token disimpan terenkripsi.</p><button class="btn primary mt-16" id="gh-config">${icon('github')}Atur token GitHub</button><a class="btn mt-8 block" href="#/apps/new">${icon('plus')}Buat aplikasi dari repo</a></div></section>
    <section class="card"><div class="card-head"><h3>${icon('cloud')}Cloudflare</h3></div><div class="card-body"><p class="small muted" style="line-height:1.6">Kelola hostname aplikasi dan ingress tunnel. Panel hanya menulis config cloudflared saat Anda menekan Terapkan.</p><a class="btn mt-16" href="#/cloudflare">${icon('cloud')}Buka Cloudflared & Port</a></div></section>
    <section class="card"><div class="card-head"><h3>${icon('archive')}Google Drive</h3></div><div class="card-body"><p class="small muted" style="line-height:1.6">Backup otomatis ke Drive dengan scope minimal drive.file.</p><a class="btn mt-16" href="#/backup">${icon('archive')}Buka Backup</a></div></section>
    <section class="card"><div class="card-head"><h3>${icon('send')}Notifikasi</h3></div><div class="card-body"><p class="small muted" style="line-height:1.6">Kanal Telegram dan WhatsApp gateway untuk alert di luar panel.</p><a class="btn mt-16" href="#/notifications">${icon('bell')}Buka Notifikasi</a></div></section></div>`;
    api('/api/integrations/github').then(g=>{$('#gh-status',main).innerHTML=g.configured?badgeText('green','@'+esc(g.login||'')):badgeText('gray','Belum terhubung');}).catch(_=>{});
    $('#gh-config',main).onclick=()=>openGitHubConfig(()=>viewSettings(el));
  }
  if(active==='mcp'){
    main.innerHTML=`<div id="mcp-panel">${loadingBlock()}</div>`;
    renderMCPTab(main, el);
  }
  if(active==='sesi'){
    main.innerHTML=`<div class="card"><div class="card-head"><h3>${icon('key')}Sesi aktif</h3><button class="btn sm danger" id="logout-all">${icon('power')}Akhiri semua sesi</button></div><div id="sessions-list">${loadingBlock()}</div></div>`;
    api('/api/sessions').then(s=>{const list=s.sessions||[];$('#sessions-list',main).innerHTML=list.length?`<div class="table-wrap"><table><thead><tr><th>Dibuat</th><th>Terakhir</th><th>Berakhir</th><th>IP</th><th>Perangkat</th><th></th></tr></thead><tbody>${list.map(x=>`<tr><td>${fmtDate(x.createdAt)}</td><td>${timeAgo(x.lastSeenAt)}</td><td>${fmtDate(x.expiresAt)}</td><td class="mono tiny">${esc(x.ip)}</td><td class="tiny" style="max-width:330px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${esc(x.userAgent)}</td><td><button class="btn-icon danger" data-session="${x.id}" title="Cabut">${icon('close')}</button></td></tr>`).join('')}</tbody></table></div>`:`<div class="empty small">Tidak ada sesi aktif</div>`;$$('[data-session]',main).forEach(b=>b.onclick=async()=>{try{await api('/api/sessions/'+b.dataset.session,{method:'DELETE'});toast('Sesi dicabut');viewSettings(el);}catch(e){toast('Gagal',e.message,'error');}});}).catch(e=>{$('#sessions-list',main).innerHTML=errorBlock(e.message);});
    $('#logout-all',main).onclick=async()=>{if(await confirmDialog({title:'Akhiri semua sesi?',message:'Semua perangkat termasuk sesi ini akan keluar dari panel.',typed:'KELUAR',danger:true,confirmLabel:'Akhiri semua'})){try{await api('/api/auth/logout-all',{method:'POST'});location.reload();}catch(e){toast('Gagal',e.message,'error');}}};
  }
  async function saveSettings(){ const form=main.querySelector('form'); const fd=new FormData(form); const body={}; fd.forEach((v,k)=>body[k]=v); ['portPoolStart','portPoolEnd','panelPort','releaseKeep','logRetentionDays','cronRetentionDays','sessionIdleMinutes','alertDisk','alertRam','alertRedis'].forEach(k=>{if(k in body)body[k]=Number(body[k]);}); const ap=form.querySelector('[name=allowProvision]'); if(ap) body.allowProvision=ap.checked; try{await api('/api/settings',{method:'PUT',body});toast('Pengaturan disimpan');setTimeout(()=>viewSettings(el),400);}catch(e){toast('Gagal menyimpan',e.detail||e.message,'error');} }
}
async function renderMCPTab(main, rootEl) {
  const box = $('#mcp-panel', main);
  let cfg;
  try { cfg = await api('/api/mcp'); } catch (e) { box.innerHTML = errorBlock(e.message); return; }
  const tokens = cfg.tokens || [];
  const scopeInfo = cfg.scopeInfo || {};
  box.innerHTML = `<div class="grid g2 mb-16">
    <section class="card"><div class="card-head"><h3>${icon('terminal')}Endpoint MCP</h3>${cfg.enabled?badgeText('green','Aktif'):badgeText('gray','Nonaktif')}</div><div class="card-body">
      <div class="alert-strip info mb-12">${icon('info')}<div>Streamable HTTP JSON-RPC 2.0. Gunakan header <span class="mono">Authorization: Bearer &lt;TOKEN-MCP&gt;</span>. Endpoint memakai tunnel dan port panel yang sama.</div></div>
      ${field('URL endpoint','<input class="input mono" readonly value="'+attr(cfg.endpoint||'')+'">')}
      ${field('Batas laju per token (per menit)','<input class="input mono" type="number" id="mcp-rate" min="1" max="600" value="'+attr(cfg.rateLimit||60)+'">','Berlaku per token; kelebihan ditolak dan dicatat di audit.')}
      <div class="mt-12">${switchRow('Aktifkan endpoint MCP','Saat dimatikan, seluruh permintaan ditolak tanpa menghapus token.','mcpEnabled',!!cfg.enabled)}</div>
      <div class="flex end mt-16"><button class="btn primary" id="mcp-save">${icon('check')}Simpan</button></div>
    </div></section>
    <section class="card"><div class="card-head"><h3>${icon('shield')}Batas keamanan MCP</h3></div><div class="card-body"><div class="list-row"><span class="avatar-sm">${icon('lock')}</span><div><div class="small strong">Kredensial tidak pernah keluar</div><div class="tiny muted">PAT GitHub, token Cloudflare, refresh token Drive, password, dan secret tidak tersedia lewat MCP.</div></div></div><div class="list-row"><span class="avatar-sm">${icon('key')}</span><div><div class="small strong">Token terpisah</div><div class="tiny muted">Token MCP tidak dapat dipakai login panel, dan sesi panel tidak dapat memanggil MCP.</div></div></div><div class="list-row"><span class="avatar-sm">${icon('alert')}</span><div><div class="small strong">Konfirmasi destruktif</div><div class="tiny muted">Aksi berisiko memerlukan confirm=true dan pengulangan target persis.</div></div></div><div class="list-row"><span class="avatar-sm">${icon('activity')}</span><div><div class="small strong">Teraudit</div><div class="tiny muted">Setiap panggilan, penolakan, dan pencabutan token masuk audit dengan sumber MCP.</div></div></div></div></section>
  </div>
  <section class="card"><div class="card-head"><h3>${icon('key')}Token MCP</h3><button class="btn primary sm" id="mcp-new">${icon('plus')}Token baru</button></div>
    <div class="table-wrap">${tokens.length?`<table><thead><tr><th>Nama</th><th>Prefix</th><th>Scope</th><th>Dibuat</th><th>Terakhir dipakai</th><th>Status</th><th></th></tr></thead><tbody>${tokens.map(t=>`<tr><td class="strong">${esc(t.name)}</td><td class="mono tiny">${esc(t.prefix)}</td><td>${(t.scopes||[]).map(s=>`<span class="tag mono">${esc(s)}</span>`).join(' ')}</td><td class="nowrap small">${fmtDate(t.createdAt)}</td><td class="small">${t.lastUsedAt?timeAgo(t.lastUsedAt):'—'}</td><td>${t.active?badgeText('green','Aktif'):badgeText('red','Dicabut')}</td><td>${t.active?`<button class="btn sm danger" data-revoke="${t.id}" data-name="${attr(t.name)}">Cabut</button>`:''}</td></tr>`).join('')}</tbody></table>`:emptyState('Belum ada token MCP','Buat token dengan scope minimal yang dibutuhkan klien MCP.','','terminal')}</div>
    <div class="card-foot"><span class="tiny muted">Token hanya ditampilkan sekali saat dibuat. Pencabutan berlaku seketika.</span></div>
  </section>`;
  const save = $('#mcp-save', box);
  save.onclick = async () => {
    try {
      await api('/api/mcp/settings', { method: 'PUT', body: { enabled: $('#mcp-enabled', box).checked, rateLimit: Number($('#mcp-rate', box).value) } });
      toast('Pengaturan MCP disimpan');
      renderMCPTab(main, rootEl);
    } catch (e) { toast('Gagal menyimpan', e.detail || e.message, 'error'); }
  };
  $('#mcp-new', box).onclick = () => openCreateMCPToken(scopeInfo, () => renderMCPTab(main, rootEl));
  $$('[data-revoke]', box).forEach(btn => btn.onclick = async () => {
    if (!await confirmDialog({ title: 'Cabut token MCP?', message: 'Token "' + btn.dataset.name + '" langsung tidak dapat dipakai. Tindakan ini tidak dapat dibatalkan.', typed: 'CABUT', danger: true, confirmLabel: 'Cabut token' })) return;
    try { await api('/api/mcp/tokens/' + btn.dataset.revoke, { method: 'DELETE' }); toast('Token dicabut'); renderMCPTab(main, rootEl); } catch (e) { toast('Gagal', e.detail || e.message, 'error'); }
  });
}
function openCreateMCPToken(scopeInfo, done) {
  const scopes = Object.keys(scopeInfo);
  const m = modal({
    title: 'Token MCP baru', subtitle: 'Pilih scope minimal. Token hanya ditampilkan satu kali.',
    body: `<form id="mcp-token-form">${field('Nama token','<input class="input" name="name" required minlength="2" maxlength="80" placeholder="mis. Claude Desktop">')}<div class="field"><label class="label">Scope</label><div style="display:grid;gap:8px">${scopes.map((s,i)=>`<label class="list-row" style="cursor:pointer"><input type="checkbox" class="scope-check" value="${attr(s)}" ${s==='read'?'checked':''}><div><div class="small strong mono">${esc(s)}</div><div class="tiny muted">${esc(scopeInfo[s]||'')}</div></div></label>`).join('')}</div></div><div class="alert-strip mt-12">${icon('alert')}<div>Token dengan scope admin dapat melakukan aksi destruktif. Berikan hanya kepada klien tepercaya.</div></div></form>`,
    footer: `<button class="btn ghost" data-close>Batal</button><button class="btn primary" id="mcp-token-create">${icon('key')}Buat token</button>`
  });
  $('#mcp-token-create', m.overlay).onclick = async () => {
    const name = $('#mcp-token-form', m.overlay).name.value.trim();
    const selected = $$('.scope-check', m.overlay).filter(c => c.checked).map(c => c.value);
    if (!name) { toast('Nama token wajib diisi', '', 'error'); return; }
    try {
      const r = await api('/api/mcp/tokens', { method: 'POST', body: { name, scopes: selected } });
      m.close();
      openMCPTokenOnce(r.token, r.name, r.scopes, done);
    } catch (e) { toast('Gagal membuat token', e.detail || e.message, 'error'); }
  };
}
function openMCPTokenOnce(token, name, scopes, done) {
  const m = modal({
    title: 'Salin token MCP sekarang', subtitle: 'Token tidak akan ditampilkan lagi setelah dialog ditutup.',
    body: `<div class="alert-strip danger mb-12">${icon('alert')}<div>Simpan token ini di pengelola rahasia klien MCP. Panel hanya menyimpan hash dan tidak dapat menampilkan nilainya kembali.</div></div><div class="field"><label class="label">Token ${esc(name||'')}</label><textarea class="input mono" id="mcp-token-value" readonly rows="3" style="resize:vertical;line-height:1.5">${esc(token)}</textarea></div><div class="tiny muted">Scope: ${(scopes||[]).map(esc).join(', ')}</div>`,
    footer: `<button class="btn outline" id="mcp-token-copy">${icon('copy')}Salin</button><button class="btn primary" id="mcp-token-done">Selesai</button>`
  });
  $('#mcp-token-copy', m.overlay).onclick = async () => {
    try { await navigator.clipboard.writeText(token); toast('Token disalin'); } catch (_) { const el = $('#mcp-token-value', m.overlay); el.focus(); el.select(); toast('Pilih dan salin manual', '', 'error'); }
  };
  $('#mcp-token-done', m.overlay).onclick = () => { m.close(); done && done(); };
}
function openGitHubConfig(done){
  api('/api/integrations/github').then(g=>{
    const m=modal({title:'Token GitHub',subtitle:'Personal Access Token dengan scope repo; admin:repo_hook untuk webhook.',account:false,body:`<form id="gh-form">${field('Personal Access Token','<input class="input" type="password" name="token" placeholder="'+(g.configured?'•••• tersimpan':'ghp_...')+'">','Token tidak dapat ditampilkan kembali.')}${g.configured?`<div class="alert-strip info mt-12">${icon('check')}<div>Terhubung sebagai <b>@${esc(g.login||'')}</b>. Masukkan token baru untuk mengganti, atau putuskan koneksi.</div></div>`:''}<div class="alert-strip mt-12">${icon('alert')}<div>Fine-grained token memerlukan izin Contents: Read, Metadata: Read, dan Webhooks: Read/Write untuk repo yang dipilih.</div></div><button type="button" class="btn danger mt-12" id="gh-clear">Hapus token</button></form>`,footer:`<button class="btn ghost" data-close>Batal</button><button class="btn primary" id="gh-save">${icon('check')}Simpan token</button>`});
    $('#gh-save',m.overlay).onclick=async()=>{const token=$('#gh-form',m.overlay).token.value.trim();if(!token){toast('Token wajib diisi','','error');return;}try{const r=await api('/api/integrations/github',{method:'PUT',body:{token}});m.close();toast('GitHub terhubung','@'+(r.login||''));done&&done();}catch(e){toast('Token tidak valid',e.detail||e.message,'error');}};
    $('#gh-clear',m.overlay).onclick=async()=>{if(await confirmDialog({title:'Hapus token GitHub?',message:'Clone repo privat dan webhook otomatis berhenti.',typed:'HAPUS',danger:true,confirmLabel:'Hapus token'})){await api('/api/integrations/github',{method:'PUT',body:{disconnect:true}});m.close();toast('Token dihapus');done&&done();}};
  }).catch(e=>toast('Gagal memuat integrasi',e.message,'error'));
}
function openChangePassword(){
  const m=modal({title:'Ubah password',body:`<form id="pw-form">${field('Password saat ini','<input class="input" type="password" name="current" autocomplete="current-password">')}${field('Password baru','<input class="input" type="password" name="new" autocomplete="new-password">','Minimal 10 karakter. Sesi lain akan diakhiri.')}</form>`,footer:`<button class="btn ghost" data-close>Batal</button><button class="btn primary" id="pw-save">${icon('check')}Simpan password</button>`});
  $('#pw-save',m.overlay).onclick=async()=>{const f=$('#pw-form',m.overlay);try{await api('/api/auth/password',{method:'POST',body:{current:f.current.value,new:f.new.value}});m.close();toast('Password diubah','Sesi lain telah diakhiri.');}catch(e){toast('Gagal',e.detail||e.message,'error');}};
}

/* ---------- Login & setup ---------- */
function renderAuth(setup) {
  document.getElementById('root').innerHTML=`<div class="auth">
    <section class="auth-art">
      <div class="auth-brand"><span class="brand-mark">${icon('shield')}</span><div><div class="brand-name">SatriaPanel</div><div class="brand-sub">Deploy & manage</div></div></div>
      <div class="auth-copy"><h1>Kendali penuh<br><span>home server</span> Anda.</h1><p>Deploy aplikasi, kelola database, pantau resource, dan backup — dari satu panel yang ringan dan aman diakses lewat Cloudflare Tunnel.</p>
        <div class="auth-features">${[['route','Port-first & native-first'],['layers','Rilis terisolasi + rollback'],['lock','Kredensial terenkripsi'],['archive','Backup otomatis Drive'],['terminal','Terminal & file manager'],['shield','Audit log lengkap']].map(([ic,t])=>`<div class="auth-feature">${icon(ic)}<span>${t}</span></div>`).join('')}</div>
      </div>
      <div class="auth-foot"><span>SatriaPanel v${esc(state.health?.version||'0.3.0')}</span><span>${esc(state.health?.serverName||'')}</span><span>Akses privat via Cloudflare</span></div>
    </section>
    <section class="auth-panel"><div class="auth-box">
      <h2>${setup?'Selamat datang':'Selamat datang kembali'}</h2>
      <p class="sub">${setup?'Konfigurasikan panel untuk pertama kali. Buat akun admin dan tentukan nama server.':'Masuk untuk mengelola server Anda.'}</p>
      ${!setup?`<form id="login-form" autocomplete="on"><div class="field"><label class="label">Username</label><input class="input" name="username" autocomplete="username" required autofocus placeholder="admin"></div><div class="field"><label class="label">Password</label><input class="input" type="password" name="password" autocomplete="current-password" required placeholder="••••••••••"></div><div id="auth-error" class="alert-strip danger mb-12" hidden></div><button class="btn primary lg block" type="submit">${icon('user')}Masuk ke panel</button></form>
      <div class="auth-footer">${esc(state.health?.serverName||'Home server')} · ${esc(state.health?.timezone||'UTC')}<br>Akses dilindungi Cloudflare Access di produksi.</div>`
      :`<form id="setup-form" autocomplete="off"><div class="field"><label class="label">Nama server</label><input class="input" name="serverName" value="${attr(state.health?.serverName||'')}" placeholder="Home Server Satria"></div><div class="form-grid"><div class="field"><label class="label">Username admin</label><input class="input" name="username" required minlength="3" maxlength="32" autocomplete="username" placeholder="wisnu"></div><div class="field"><label class="label">Password</label><input class="input" type="password" name="password" required minlength="10" autocomplete="new-password" placeholder="minimal 10 karakter"><div class="password-strength"><i id="pw-strength"></i></div></div></div><div class="field"><label class="label">Base URL panel <span class="faint">(opsional)</span></label><input class="input mono" name="baseUrl" placeholder="https://panel.satriyadi.com"><div class="hint">Dipakai untuk URL webhook GitHub. Dapat diubah nanti.</div></div><div id="auth-error" class="alert-strip danger mb-12" hidden></div><button class="btn primary lg block" type="submit">${icon('shield')}Buat panel</button></form>
      <div class="demo-hint">${icon('info')} Data contoh aktif karena panel dijalankan dengan flag <span class="mono">-demo</span>.</div>`}
    </div></section>
  </div>`;
  const errBox=()=>$('#auth-error');
  if(setup){
    const form=$('#setup-form');
    form.password.oninput=e=>{const v=e.target.value;let score=0;if(v.length>=10)score++;if(/[A-Z]/.test(v))score++;if(/[0-9]/.test(v))score++;if(/[^A-Za-z0-9]/.test(v))score++;const el=$('#pw-strength');el.style.width=(score*25)+'%';el.style.background=score<2?'#EF4444':score<3?'#F59E0B':'#22C55E';};
    form.onsubmit=async e=>{e.preventDefault();const fd=new FormData(form);const eb=errBox();eb.hidden=true;const btn=form.querySelector('button[type=submit]');btn.disabled=true;try{await api('/api/setup',{method:'POST',body:{username:fd.get('username'),password:fd.get('password'),serverName:fd.get('serverName'),baseUrl:fd.get('baseUrl')}});state.me=await api('/api/auth/me');location.hash='#/dashboard';await bootApp();}catch(err){eb.innerHTML=icon('alert')+'<div>'+esc(err.detail||err.message)+'</div>';eb.hidden=false;btn.disabled=false;}};
  } else {
    const form=$('#login-form');
    form.onsubmit=async e=>{e.preventDefault();const fd=new FormData(form);const eb=errBox();eb.hidden=true;const btn=form.querySelector('button[type=submit]');btn.disabled=true;try{await api('/api/auth/login',{method:'POST',body:{username:fd.get('username'),password:fd.get('password')}});state.me=await api('/api/auth/me');await bootApp();}catch(err){eb.innerHTML=icon('alert')+'<div>'+esc(err.detail||err.message)+'</div>';eb.hidden=false;btn.disabled=false;}};
  }
}

Object.assign(window,{viewBackup,viewCloudflare,viewNotifications,viewAudit,viewSettings,renderAuth,openGitHubConfig});
