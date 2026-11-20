'use strict';
/* ---------- Database ---------- */
async function viewDatabases(el) {
  setPageTitle('Database');
  viewLoading(el);
  let d;
  try { d=await api('/api/databases'); } catch(e){ el.innerHTML=errorBlock(e.message); return; }
  const dbs=d.databases||[], server=d.server||{};
  el.innerHTML=`${pageHead('Database', 'Kelola PostgreSQL, MySQL, dan SQLite. Editor baris, SQL, impor/ekspor, serta backup.','<button class="btn primary" id="db-add">'+icon('plus')+'Tambah database</button>')}
  <div class="grid g4 mb-16">
    ${statCard('Database',dbs.length,`${dbs.filter(x=>x.engine==='sqlite').length} SQLite · ${dbs.filter(x=>x.engine!=='sqlite').length} server`,'database')}
    ${statCard('PostgreSQL',badgeText(server.postgresql==='active'?'green':'gray',server.postgresql==='active'?'Aktif':server.postgresql||'Tidak aktif'),'Layanan sistem','databaseZap','info')}
    ${statCard('MySQL',badgeText(server.mysql==='active'?'green':'gray',server.mysql==='active'?'Aktif':server.mysql||'Tidak aktif'),'Layanan sistem','database','warn')}
    ${statCard('Penyimpanan SQLite',fmtBytes(dbs.reduce((n,x)=>n+(x.size||0),0)),'Folder '+esc(d.sqliteDir||'data/databases'),'hardDrive')}
  </div>
  <div class="card"><div class="card-head"><div><h3>${icon('database')}Koneksi database</h3><div class="hint">Database terhubung ke aplikasi akan menginjeksi DATABASE_URL.</div></div><div class="toolbar"><input class="input" id="db-search" placeholder="Cari database…" style="width:220px"><button class="btn sm outline" id="db-refresh">${icon('refresh')}</button></div></div><div class="table-wrap" id="db-table"></div></div>`;
  const table=$('#db-table',el);
  function draw(){
    const q=($('#db-search',el)?.value||'').toLowerCase();
    const list=dbs.filter(x=>!q||[x.name,x.engine,x.appName].some(v=>(v||'').toLowerCase().includes(q)));
    if(!dbs.length){table.innerHTML=emptyState('Belum ada database','Tambahkan SQLite instan atau hubungkan database server yang sudah berjalan.','<button class="btn primary sm" id="db-empty-add">Tambah database</button>','database');$('#db-empty-add',table)?.addEventListener('click',()=>openDatabaseForm(()=>viewDatabases(el)));return;}
    table.innerHTML=`<table><thead><tr><th>Nama</th><th>Engine</th><th>Lokasi</th><th>Aplikasi</th><th>Ukuran</th><th>Status</th><th style="text-align:right">Aksi</th></tr></thead><tbody>${list.map(x=>`<tr class="clickable" data-db="${x.id}"><td><div class="flex gap-8"><span class="avatar-sm">${icon('database')}</span><div><div class="row-title">${esc(x.name)}</div><div class="row-sub mono">${x.managedByPanel?'dikelola panel':'koneksi manual'}</div></div></div></td><td>${badgeText(x.engine==='postgresql'?'blue':x.engine==='mysql'?'amber':'green',x.engine)}</td><td><span class="mono small">${esc(x.engine==='sqlite'?x.filePath:(x.host+':'+x.port))}</span></td><td>${x.appName?esc(x.appName):'<span class="faint">—</span>'}</td><td>${x.size?fmtBytes(x.size):'<span class="faint">—</span>'}</td><td>${badge(x.status==='online'?'online':x.status==='offline'?'offline':'unknown',x.status==='online'?'Online':x.status==='offline'?'Offline':'Belum diuji')}</td><td><div class="table-actions"><button class="btn-icon" data-test="${x.id}" title="Tes koneksi">${icon('plug')}</button><a class="btn-icon" href="#/databases/${x.id}" title="Editor">${icon('code')}</a><a class="btn-icon" href="#/databases/${x.id}?tab=backup" title="Backup">${icon('archive')}</a></div></td></tr>`).join('')}</tbody></table>`;
    table.querySelectorAll('tr[data-db]').forEach(r=>r.onclick=e=>{ if(e.target.closest('.table-actions')) return; location.hash='#/databases/'+r.dataset.db; });
    table.querySelectorAll('[data-test]').forEach(b=>b.onclick=async()=>{b.disabled=true;try{const r=await api('/api/databases/'+b.dataset.test+'/test');toast(r.ok?'Koneksi berhasil':'Koneksi gagal',r.ok?`Driver ${r.driver} · ${r.durationMs} ms`:(r.error||''),r.ok?'success':'error');}catch(e){toast('Tes gagal',e.message,'error');}finally{b.disabled=false;}});
  }
  $('#db-add',el).onclick=()=>openDatabaseForm(()=>viewDatabases(el));
  $('#db-search',el).oninput=draw; $('#db-refresh',el).onclick=()=>viewDatabases(el); draw();
}
function openDatabaseForm(done) {
  const m=modal({title:'Tambah database',subtitle:'Buat SQLite baru, atau hubungkan server PostgreSQL/MySQL.',size:'lg',body:`<form id="db-form"><div class="form-grid">
    ${field('Engine','<select class="select" name="engine"><option value="sqlite">SQLite (berkas lokal)</option><option value="postgresql">PostgreSQL</option><option value="mysql">MySQL</option></select>', '', false, true)}
    ${field('Nama database','<input class="input mono" name="name" placeholder="toko_production" required>','Nama database atau nama tampilan.',false,true)}
    <div class="full" id="db-extra"></div>
    ${field('Hubungkan ke aplikasi','<select class="select" name="appId"><option value="0">Tidak terhubung</option></select>','Satu aplikasi hanya dapat terhubung ke satu database.')}
    <div class="full" id="db-provision"></div>
  </div></form>`,footer:`<button class="btn ghost" data-close>Batal</button><button class="btn primary" id="db-save">${icon('check')}Simpan database</button>`});
  const form=$('#db-form',m.overlay), extra=$('#db-extra',m.overlay), prov=$('#db-provision',m.overlay);
  api('/api/apps').then(d=>{ const s=form.appId; (d.apps||[]).forEach(a=>s.insertAdjacentHTML('beforeend',`<option value="${a.id}">${esc(a.name)}</option>`)); }).catch(_=>{});
  const sync=()=>{
    const e=form.engine.value;
    extra.innerHTML=e==='sqlite'
      ? field('Lokasi berkas','<input class="input mono" name="filePath" placeholder="otomatis di data/databases/nama.db">','Kosongkan untuk membuat otomatis. Path relatif ditempatkan di folder database panel.',true)
      : `<div class="form-grid">${field('Host','<input class="input mono" name="host" value="127.0.0.1">','',false,true)}${field('Port','<input class="input mono" type="number" name="port" value="'+(e==='postgresql'?'5432':'3306')+'">')}${field('Username','<input class="input mono" name="username" placeholder="db_user">','',false,true)}${field('Password','<input class="input" type="password" name="password" placeholder="password database">','Disimpan terenkripsi.')}</div>`;
    prov.innerHTML=e==='sqlite'?'':`<div class="field-row"><div><div class="t">Buat database & user di server</div><div class="d">Memerlukan izin provisioning di Pengaturan dan akses sudo lokal. Jika nonaktif, hubungkan database yang sudah ada.</div></div><label class="switch"><input type="checkbox" name="createDatabase"><span></span></label></div>`;
  };
  form.engine.onchange=sync; sync();
  $('#db-save',m.overlay).onclick=async()=>{
    if(!form.reportValidity())return;
    const fd=new FormData(form);
    const body={engine:fd.get('engine'),name:fd.get('name'),host:fd.get('host')||'',port:Number(fd.get('port')||0),username:fd.get('username')||'',password:fd.get('password')||'',filePath:fd.get('filePath')||'',appId:Number(fd.get('appId')||0),managedByPanel:true,createDatabase:!!form.createDatabase?.checked};
    const b=$('#db-save',m.overlay); b.disabled=true;
    try{ const r=await api('/api/databases',{method:'POST',body}); m.close(); toast('Database ditambahkan',r.database?.name); done&&done(); }catch(e){toast('Gagal menambah database',e.detail||e.message,'error');b.disabled=false;}
  };
}
async function viewDatabaseEditor(el,id) {
  let data;
  try{ data=await api('/api/databases/'+id); }catch(e){ setPageTitle('Database'); el.innerHTML=errorBlock(e.message,'<a class="btn" href="#/databases">Kembali</a>'); return; }
  const db=data.database; setPageTitle(db.name,'Database · '+db.engine);
  const tab=(location.hash.split('?')[1]||'tab=data').replace('tab=','');
  const active=['data','sql','history','backup'].includes(tab)?tab:'data';
  el.innerHTML=`
  <div class="breadcrumb"><a href="#/databases">Database</a><span>›</span>${esc(db.name)}</div>
  ${pageHead(esc(db.name), `${esc(db.engine)} · ${esc(db.engine==='sqlite'?db.filePath:(db.host+':'+db.port))} · ${badge(data.status==='online'?'online':'offline',data.status==='online'?'Online':'Offline')}`, `<button class="btn sm outline" id="db-test">${icon('plug')}Tes</button><button class="btn sm outline" id="db-backup-now">${icon('archive')}Backup</button><button class="btn sm outline" id="db-import">${icon('upload')}Impor</button><button class="btn sm danger" id="db-delete">${icon('trash')}Hapus</button>`)}
  ${data.status!=='online'?`<div class="alert-strip danger mb-16">${icon('alert')}<div><b>Tidak dapat terhubung.</b> ${esc(data.detail||'')} <button class="link-btn" id="db-test2">Tes ulang</button></div></div>`:''}
  <div class="tabs mb-16">${[['data','Data','table'],['sql','SQL','code'],['history','Riwayat','list'],['backup','Backup','archive']].map(([k,t,ic])=>`<a class="tab ${active===k?'active':''}" href="#/databases/${id}?tab=${k}">${icon(ic)}${t}</a>`).join('')}</div>
  <div id="db-main"></div>`;
  const main=$('#db-main',el);
  const test=async btn=>{btn.disabled=true;const r=await api('/api/databases/'+id+'/test');toast(r.ok?'Koneksi berhasil':'Koneksi gagal',r.ok?`${r.driver} · ${r.durationMs} ms`:r.error,r.ok?'success':'error');btn.disabled=false;};
  $('#db-test',el).onclick=e=>test(e.currentTarget); $('#db-test2',el)?.addEventListener('click',()=>location.reload());
  $('#db-backup-now',el).onclick=async()=>{try{await api('/api/databases/'+id+'/backups',{method:'POST'});toast('Backup dibuat');if(active==='backup')viewDatabaseEditor(el,id);}catch(e){toast('Backup gagal',e.detail||e.message,'error');}};
  $('#db-import',el).onclick=()=>openDBImport(id,()=>viewDatabaseEditor(el,id));
  $('#db-delete',el).onclick=async()=>{ if(await confirmDialog({title:'Hapus database '+db.name+'?',message:'Data tidak dihapus dari server secara otomatis. Riwayat query dan backup di panel ikut dihapus.',typed:db.name,confirmLabel:'Hapus database',danger:true,input:true,inputLabel:'Ketik nama database untuk konfirmasi'})){ try{await api('/api/databases/'+id+qs({confirm:db.name,deleteFile:db.engine==='sqlite'?1:0}),{method:'DELETE'});toast('Database dihapus');location.hash='#/databases';}catch(e){toast('Gagal menghapus',e.detail||e.message,'error');} } };
  if(active==='data') renderDBData(main,id,db);
  if(active==='sql') renderDBSQL(main,id,db);
  if(active==='history') renderDBHistory(main,id);
  if(active==='backup') renderDBBackup(main,id,db,data.backups||[]);
}
async function renderDBData(main,id,db){
  main.innerHTML=`<div class="split" style="grid-template-columns:248px minmax(0,1fr)"><section class="card" style="align-self:start"><div class="card-head"><h3>${icon('table')}Tabel</h3><button class="btn-icon sm" id="tbl-refresh">${icon('refresh')}</button></div><div id="table-list" style="padding:7px">${loadingBlock('Memuat tabel…')}</div></section><section class="card"><div class="card-head"><div><h3 id="tbl-title">Pilih tabel</h3><div class="hint" id="tbl-sub">Klik tabel di kiri untuk melihat baris.</div></div><div class="toolbar" id="tbl-tools"></div></div><div id="tbl-grid">${emptyState('Belum ada tabel dipilih','Data akan tampil setelah memilih tabel.','','table')}</div></section></div>`;
  let tables=[], table=null, limit=50, offset=0, sort='', dir='asc', filter='', result=null;
  try{ const d=await api('/api/databases/'+id+'/tables'); tables=d.tables||[]; }catch(e){ $('#table-list',main).innerHTML=errorBlock(e.message); return; }
  const drawList=()=>{ $('#table-list',main).innerHTML=tables.length?tables.map(t=>`<button class="nav-item ${table?.name===t.name?'active':''}" style="width:100%;background:${table?.name===t.name?'rgba(34,197,94,.08)':'none'};border:0;margin:1px 0" data-table="${attr(t.name)}">${icon(t.type==='view'?'eye':'table')}<span class="truncate" style="flex:1;text-align:left">${esc(t.name)}</span><span class="tiny faint mono">${t.rows>=0?t.rows:''}</span></button>`).join(''):emptyState('Tidak ada tabel','Database ini belum memiliki tabel.','','database'); $$('[data-table]',main).forEach(b=>b.onclick=()=>{ table=tables.find(t=>t.name===b.dataset.table); offset=0; filter=''; sort=''; drawList(); loadTable(); }); };
  drawList();
  const loadTable=async()=>{
    if(!table)return;
    $('#tbl-title',main).textContent=table.name;
    $('#tbl-sub',main).textContent=`${table.schema} · ${table.columns.length} kolom · kira-kira ${fmtNum(table.rows)} baris`;
    $('#tbl-tools',main).innerHTML=`<button class="btn sm primary" id="row-add">${icon('plus')}Baris</button><button class="btn sm outline" id="tbl-export">${icon('download')}CSV</button>`;
    $('#tbl-grid',main).innerHTML=loadingBlock('Memuat baris…');
    try{ const d=await api(`/api/databases/${id}/table`+qs({table:table.name,limit,offset,sort,dir,filter})); result=d.result; $('#tbl-grid',main).innerHTML=renderRowsGrid(d,table); bindGrid(); }catch(e){ $('#tbl-grid',main).innerHTML=errorBlock(e.detail||e.message,`<button class="btn sm" id="tbl-retry">Coba lagi</button>`); $('#tbl-retry',main)?.addEventListener('click',loadTable); }
    $('#tbl-export',main).onclick=()=>apiDownload(`/api/databases/${id}/export?table=${encodeURIComponent(table.name)}&format=csv`,table.name+'.csv').catch(e=>toast('Ekspor gagal',e.message,'error'));
    $('#row-add',main).onclick=()=>openRowForm(id,table.name,table,null,loadTable);
  };
  const bindGrid=()=>{
    $$('[data-sort]',main).forEach(th=>th.onclick=()=>{ if(sort===th.dataset.sort) dir=dir==='asc'?'desc':'asc'; else {sort=th.dataset.sort;dir='asc';} loadTable(); });
    $$('[data-row-edit]',main).forEach(b=>b.onclick=()=>{ const i=Number(b.dataset.rowEdit); const obj={}; result.columns.forEach((c,j)=>obj[c]=result.rows[i][j]); openRowForm(id,table.name,table,obj,loadTable); });
    $$('[data-row-del]',main).forEach(b=>b.onclick=async()=>{ const i=Number(b.dataset.rowDel); const obj={}; result.columns.forEach((c,j)=>obj[c]=result.rows[i][j]); const pk={}; table.columns.filter(c=>c.pk).forEach(c=>pk[c.name]=obj[c.name]); if(!Object.keys(pk).length){toast('Tidak ada primary key','Hapus baris lewat editor SQL.','error');return;} if(await confirmDialog({title:'Hapus baris?',message:'Baris dengan primary key <span class="mono">'+esc(JSON.stringify(pk))+'</span> akan dihapus permanen.',typed:'HAPUS',confirmLabel:'Hapus baris',danger:true})){try{await api('/api/databases/'+id+'/rows',{method:'DELETE',body:{table:table.name,pk,confirm:'HAPUS'}});toast('Baris dihapus');loadTable();}catch(e){toast('Gagal menghapus',e.detail||e.message,'error');}} });
    $$('[data-page]',main).forEach(b=>b.onclick=()=>{ const p=b.dataset.page; if(p==='next'){offset+=limit;} else {offset=Math.max(0,offset-limit);} loadTable(); });
    const f=$('#grid-filter',main); if(f)f.oninput=debounce(()=>{filter=f.value;offset=0;loadTable();},400);
    $('#tbl-refresh',main).onclick=async()=>{ try{const d=await api('/api/databases/'+id+'/tables');tables=d.tables||[];table=tables.find(t=>t.name===table?.name)||null;drawList();loadTable();}catch(e){toast('Gagal memuat tabel',e.message,'error');} };
  };
  function renderRowsGrid(d,t){
    const r=d.result, total=d.total;
    const cols=r.columns.map(c=>{const meta=t.columns.find(x=>x.name===c);return `<th class="th-sort" data-sort="${attr(c)}">${esc(c)}${meta?.pk?' <span class="tag" style="font-size:8px">PK</span>':''}${sort===c?(dir==='asc'?' ↑':' ↓'):''}</th>`;}).join('');
    const body=r.rows.length?r.rows.map((row,i)=>`<tr><td><div class="table-actions" style="justify-content:flex-start"><button class="btn-icon sm" data-row-edit="${i}" title="Edit">${icon('edit')}</button><button class="btn-icon sm danger" data-row-del="${i}" title="Hapus">${icon('trash')}</button></div></td>${row.map((v,j)=>`<td>${v===null?'<span class="faint">NULL</span>':`<span class="mono" style="font-size:11.5px">${esc(String(v).length>140?String(v).slice(0,140)+'…':v)}</span>`}</td>`).join('')}</tr>`).join(''):`<tr><td colspan="${r.columns.length+1}">${emptyState('Tabel kosong','Belum ada baris pada tabel ini.','<button class="btn sm" id="row-empty-add">Tambah baris</button>','table')}</td></tr>`;
    return `<div class="flex between gap-8" style="padding:10px 14px;border-bottom:1px solid var(--border-soft)"><div class="input-group" style="max-width:300px"><input class="input" id="grid-filter" placeholder="Filter semua kolom…" value="${attr(filter)}"><span style="display:grid;place-items:center;width:34px;border:1px solid var(--border-soft);border-left:0;border-radius:0 8px 8px 0">${icon('filter')}</span></div><span class="tiny muted">${fmtNum(total)} baris · ${r.durationMs} ms</span></div><div class="table-wrap"><table><thead><tr><th style="width:78px">Aksi</th>${cols}</tr></thead><tbody>${body}</tbody></table></div><div class="card-foot"><span class="tiny muted">Menampilkan ${offset+1}–${Math.min(offset+limit,total)} dari ${fmtNum(total)}</span><div class="flex gap-8"><button class="btn sm" data-page="prev" ${offset===0?'disabled':''}>${icon('chevronLeft')}Sebelumnya</button><button class="btn sm" data-page="next" ${offset+limit>=total?'disabled':''}>Berikutnya${icon('chevronRight')}</button></div></div>`;
  }
  // muat tabel pertama otomatis
  if(tables.length){ table=tables[0]; drawList(); loadTable(); }
}
function convertVal(v,type){
  if(v==='') return null;
  const t=(type||'').toLowerCase();
  if(/int|serial|number/.test(t)) { const n=Number(v); return isNaN(n)?v:n; }
  if(/numeric|decimal|real|double|float/.test(t)) { const n=Number(v); return isNaN(n)?v:n; }
  if(/bool/.test(t)) return v==='1'||v.toLowerCase()==='true';
  return v;
}
function openRowForm(dbId,tableName,table,row,reload){
  const isEdit=!!row; const cols=table.columns;
  const body=`<form id="row-form"><div class="form-grid">${cols.map(c=>`${field(c.name+(c.pk?' (PK)':''),`<input class="input mono" name="col_${attr(c.name)}" value="${attr(isEdit&&row[c.name]!=null?row[c.name]:'')}" placeholder="${c.nullable?'NULL / kosong':''}">`,c.type+(c.nullable?' · nullable':''),cols.length<3)}`).join('')}</div><div class="hint mt-12">Nilai kosong dikirim sebagai NULL. Perubahan langsung dieksekusi tanpa transaksi tambahan.</div></form>`;
  const m=modal({title:isEdit?'Edit baris':'Tambah baris',subtitle:tableName,size:'lg',body,footer:`${isEdit?'<button class="btn danger" id="row-form-del">Hapus baris</button>':''}<button class="btn ghost" data-close>Batal</button><button class="btn primary" id="row-form-save">${icon('check')}Simpan</button>`});
  $('#row-form-save',m.overlay).onclick=async()=>{
    const values={},pk={};
    cols.forEach(c=>{ const inp=m.overlay.querySelector(`[name="col_${CSS.escape(c.name)}"]`); if(!inp)return; values[c.name]=convertVal(inp.value,c.type); if(isEdit&&c.pk) pk[c.name]=row[c.name]; });
    try{ await api('/api/databases/'+dbId+'/rows',{method:isEdit?'PUT':'POST',body:{table:tableName,values,pk}}); m.close(); toast(isEdit?'Baris diperbarui':'Baris ditambahkan'); reload(); }catch(e){toast('Gagal menyimpan baris',e.detail||e.message,'error');}
  };
  $('#row-form-del',m.overlay)?.addEventListener('click',async()=>{ const pk={}; cols.filter(c=>c.pk).forEach(c=>pk[c.name]=row[c.name]); if(!Object.keys(pk).length){toast('Tidak ada primary key','Gunakan editor SQL.','error');return;} if(await confirmDialog({title:'Hapus baris?',message:'Baris ini akan dihapus permanen.',typed:'HAPUS',confirmLabel:'Hapus',danger:true})){try{await api('/api/databases/'+dbId+'/rows',{method:'DELETE',body:{table:tableName,pk,confirm:'HAPUS'}});m.close();toast('Baris dihapus');reload();}catch(e){toast('Gagal menghapus',e.detail||e.message,'error');}} });
}
function renderDBSQL(main,id,db){
  main.innerHTML=`<div class="card"><div class="card-head"><div><h3>${icon('code')}Editor SQL</h3><div class="hint">Ctrl+Enter untuk menjalankan. Query dibatasi 30 detik.</div></div><div class="flex gap-8"><button class="btn sm outline" id="sql-history">${icon('clock')}Riwayat</button><button class="btn sm primary" id="sql-run">${icon('play')}Jalankan</button></div></div><div class="card-body"><textarea class="textarea mono" id="sql-editor" style="min-height:150px" placeholder="SELECT * FROM ${esc(db.engine==='sqlite'?'tabel':'schema.tabel')} LIMIT 50;">SELECT * FROM ${db.engine==='sqlite'?'sqlite_master':'information_schema.tables'} LIMIT 20;</textarea><div class="alert-strip mt-12">${icon('alert')}<div><b>DELETE/UPDATE tanpa WHERE</b> memerlukan konfirmasi. Kredensial koneksi tidak pernah ditampilkan di editor.</div></div><div id="sql-result" class="mt-16"></div></div></div>`;
  const run=async()=>{
    const sql=$('#sql-editor',main).value.trim(); if(!sql)return;
    const box=$('#sql-result',main); box.innerHTML=loadingBlock('Menjalankan query…');
    const btn=$('#sql-run',main); btn.disabled=true;
    try{ const d=await api('/api/databases/'+id+'/query',{method:'POST',body:{sql}}); const r=d.result; box.innerHTML=`<div class="flex between mb-8"><span class="small strong">${r.columns?.length?'Hasil query':r.message||'Query berhasil'}</span><span class="tiny muted">${r.durationMs} ms${r.affected?` · ${r.affected} baris terpengaruh`:''}${r.truncated?' · dipotong 5000 baris':''}</span></div>${r.columns?.length?`<div class="table-wrap"><table><thead><tr>${r.columns.map(c=>`<th>${esc(c)}</th>`).join('')}</tr></thead><tbody>${r.rows.map(row=>`<tr>${row.map(v=>`<td>${v===null?'<span class="faint">NULL</span>':`<span class="mono" style="font-size:11.5px">${esc(v)}</span>`}</td>`).join('')}</tr>`).join('')}</tbody></table></div>`:`<div class="alert-strip info">${icon('check')}<div>${esc(r.message||'Query berhasil dijalankan.')}</div></div>`}`; }
    catch(e){ box.innerHTML=`<div class="alert-strip danger">${icon('alert')}<div><b>Query gagal.</b><pre class="mono" style="white-space:pre-wrap;margin:7px 0 0;font-size:11px">${esc(e.detail||e.message)}</pre></div></div>`; }
    finally{btn.disabled=false;}
  };
  $('#sql-run',main).onclick=run;
  const reuse=sessionStorage.getItem('sql-reuse'); if(reuse){ $('#sql-editor',main).value=reuse; sessionStorage.removeItem('sql-reuse'); }
  $('#sql-editor',main).onkeydown=e=>{if((e.ctrlKey||e.metaKey)&&e.key==='Enter'){e.preventDefault();run();}};
  $('#sql-history',main).onclick=()=>location.hash=`#/databases/${id}?tab=history`;
}
function renderDBHistory(main,id){
  main.innerHTML=`<div class="card"><div class="card-head"><h3>${icon('list')}Riwayat query</h3><button class="btn-icon sm" id="hist-refresh">${icon('refresh')}</button></div><div id="hist-list">${loadingBlock()}</div></div>`;
  const load=async()=>{try{const d=await api('/api/databases/'+id+'/history'); const box=$('#hist-list',main); box.innerHTML=(d.history||[]).length?`<div class="table-wrap"><table><thead><tr><th>Waktu</th><th>SQL</th><th>Status</th><th>Durasi</th><th></th></tr></thead><tbody>${d.history.map(h=>`<tr><td class="nowrap">${fmtDate(h.createdAt)}</td><td><span class="mono" style="font-size:11px">${esc(h.sql.length>120?h.sql.slice(0,120)+'…':h.sql)}</span></td><td>${badge(h.status==='berhasil'?'berhasil':'gagal',h.status)}</td><td class="mono">${h.durationMs} ms</td><td><button class="btn-icon sm" data-reuse="${attr(h.sql)}" title="Gunakan lagi">${icon('rotate')}</button></td></tr>`).join('')}</tbody></table></div>`:emptyState('Belum ada riwayat','Query yang dijalankan dari editor akan tercatat.','','list'); $$('[data-reuse]',box).forEach(b=>b.onclick=()=>{sessionStorage.setItem('sql-reuse',b.dataset.reuse);location.hash='#/databases/'+id+'?tab=sql';}); }catch(e){$('#hist-list',main).innerHTML=errorBlock(e.message);}};
  $('#hist-refresh',main).onclick=load; load();
}
function renderDBBackup(main,id,db,backups){
  main.innerHTML=`<div class="split"><section class="card"><div class="card-head"><div><h3>${icon('archive')}Backup database</h3><div class="hint">Disimpan di folder backup panel.</div></div><button class="btn sm primary" id="bk-now">${icon('plus')}Buat backup</button></div><div id="bk-list">${backups.length?`<div class="table-wrap"><table><thead><tr><th>Berkas</th><th>Ukuran</th><th>Waktu</th><th></th></tr></thead><tbody>${backups.map(b=>`<tr><td class="mono small">${esc(b.file)}</td><td>${fmtBytes(b.size)}</td><td>${fmtDate(b.createdAt)}</td><td class="table-actions"><button class="btn-icon" data-dl="${b.id}" title="Unduh">${icon('download')}</button><button class="btn-icon" data-restore="${b.id}" title="Restore">${icon('rotate')}</button></td></tr>`).join('')}</tbody></table></div>`:emptyState('Belum ada backup','Buat backup pertama untuk database ini.','','archive')}</div></section><section class="card"><div class="card-head"><h3>${icon('alert')}Catatan restore</h3></div><div class="card-body"><p class="small muted" style="line-height:1.65">Restore membuat safety backup terlebih dahulu, lalu menimpa database. Untuk PostgreSQL/MySQL dibutuhkan klien <span class="mono">pg_dump/psql</span> atau <span class="mono">mysqldump/mysql</span> di server. Hentikan penulisan aplikasi untuk hasil paling aman.</p></div></section></div>`;
  $('#bk-now',main).onclick=async()=>{try{await api('/api/databases/'+id+'/backups',{method:'POST'});toast('Backup database dibuat');viewDatabaseEditor(document.getElementById('content'),id);}catch(e){toast('Backup gagal',e.detail||e.message,'error');}};
  $$('[data-dl]',main).forEach(b=>b.onclick=()=>apiDownload('/api/db-backups/'+b.dataset.dl+'/download','database-backup').catch(e=>toast('Unduhan gagal',e.message,'error')));
  $$('[data-restore]',main).forEach(b=>b.onclick=async()=>{ if(await confirmDialog({title:'Restore database?',message:`Database <b>${esc(db.name)}</b> akan ditimpa dari backup #${b.dataset.restore}. Safety backup dibuat otomatis.`,typed:db.name,confirmLabel:'Restore database',danger:true,input:true,inputLabel:'Ketik nama database untuk konfirmasi'})){ try{await api('/api/databases/'+id+'/restore',{method:'POST',body:{backupId:Number(b.dataset.restore),confirm:db.name}});toast('Restore selesai');viewDatabaseEditor(document.getElementById('content'),id);}catch(e){toast('Restore gagal',e.detail||e.message,'error');} } });
}
function openDBImport(id,done){
  const m=modal({title:'Impor data',subtitle:'Berkas .sql atau .csv (maks 200 MB).',body:`<form id="imp-form"><div class="form-grid">${field('Berkas','<input class="input" type="file" name="file" accept=".sql,.csv" required>','Untuk CSV, baris pertama harus header.',true,true)}${field('Tabel tujuan','<input class="input mono" name="table" placeholder="nama_tabel">','Wajib untuk CSV. Untuk SQL diabaikan.',true)}</div></form>`,footer:`<button class="btn ghost" data-close>Batal</button><button class="btn primary" id="imp-run">${icon('upload')}Impor</button>`});
  $('#imp-run',m.overlay).onclick=async()=>{const form=$('#imp-form',m.overlay); const f=form.file.files[0]; if(!f){toast('Pilih berkas','','error');return;} const fd=new FormData(); fd.append('file',f); fd.append('table',form.table.value); const b=$('#imp-run',m.overlay);b.disabled=true;try{const r=await api('/api/databases/'+id+'/import',{method:'POST',body:fd});m.close();toast('Impor selesai',`${r.inserted} statement/baris diproses`);done&&done();}catch(e){toast('Impor gagal',e.detail||e.message,'error');b.disabled=false;}};
}

/* ---------- Redis ---------- */
async function viewRedis(el) {
  setPageTitle('Redis');
  viewLoading(el);
  let d; try{ d=await api('/api/redis'); }catch(e){el.innerHTML=errorBlock(e.message);return;}
  state.redis=d; const st=d.status||{}, alloc=d.allocations||[], cfg=d.config||{};
  const online=!!st.online;
  el.innerHTML=`${pageHead('Redis','Satu instance Redis dengan ACL terisolasi per aplikasi. Setiap app mendapat prefix app_&lt;slug&gt;:*.',`<button class="btn sm outline" id="redis-cfg">${icon('settings')}Konfigurasi</button>${online?`<button class="btn sm outline" id="redis-restart">${icon('restart')}Restart layanan</button>`:''}`)}
  ${!online?`<div class="alert-strip danger mb-16">${icon('alert')}<div><b>Redis belum terhubung.</b> ${esc(st.error||'Periksa layanan Redis dan kredensial koneksi.')} ${st.service?`Layanan: <span class="mono">${esc(st.service)}</span>.`:''} <button class="link-btn" id="redis-cfg2">Buka konfigurasi</button></div></div>`:''}
  <div class="grid g4 mb-16">
    ${statCard('Status',online?badgeText('green','Online'):badgeText('red','Offline'),st.version?`Redis ${esc(st.version)} · ${esc(st.mode||'')}`:'Tidak dapat terhubung','redis',online?'':'warn')}
    ${statCard('Memori',online?`${pct(st.memoryPercent)}`:'—',st.memoryMax?`${fmtBytes(st.memoryUsed)} / ${fmtBytes(st.memoryMax)}`:'maxmemory belum diatur','memory','info')}
    ${statCard('Key',online?fmtNum(st.keys):'—',`${st.clients||0} klien terhubung`,'key')}
    ${statCard('Alokasi ACL',alloc.length,`${alloc.filter(a=>a.prefix).length} prefix terisolasi`,'shield','warn')}
  </div>
  <div class="split"><section class="card"><div class="card-head"><div><h3>${icon('shield')}Alokasi aplikasi</h3><div class="hint">User ACL + password terenkripsi + prefix key.</div></div><button class="btn sm primary" id="alloc-add" ${online?'':'disabled'}>${icon('plus')}Alokasi baru</button></div><div class="table-wrap">${alloc.length?`<table><thead><tr><th>Aplikasi</th><th>ACL user</th><th>Prefix</th><th>Dibuat</th><th></th></tr></thead><tbody>${alloc.map(a=>`<tr><td><div class="row-title">${esc(a.appName||'—')}</div><div class="row-sub mono">${esc(a.slug)}</div></td><td class="mono small">${esc(a.aclUser)}</td><td><span class="mono small">${esc(a.prefix)}*</span></td><td>${fmtDate(a.createdAt)}</td><td class="table-actions"><button class="btn-icon danger" data-revoke="${a.id}" title="Cabut ACL">${icon('trash')}</button></td></tr>`).join('')}</tbody></table>`:emptyState('Belum ada alokasi','Buat ACL agar setiap aplikasi hanya bisa mengakses prefix miliknya.','<button class="btn sm" id="alloc-empty">Buat alokasi</button>','redis')}</div></section>
  <section class="card"><div class="card-head"><h3>${icon('activity')}Statistik</h3></div><div class="card-body">${online?`<dl class="kv"><dt>Versi</dt><dd class="mono">${esc(st.version||'—')}</dd><dt>Mode</dt><dd>${esc(st.mode||'—')}</dd><dt>Uptime</dt><dd>${dur(st.uptimeSec)}</dd><dt>Ops/detik</dt><dd class="mono">${esc(st.ops||'0')}</dd><dt>Hit / miss</dt><dd class="mono">${fmtNum(st.hits)} / ${fmtNum(st.misses)}</dd><dt>Koneksi</dt><dd class="mono">${esc(cfg.host)}:${esc(cfg.port)}</dd></dl><div class="hr"></div><div class="small strong mb-8">Prefix key</div><div class="injected"><span class="mono">app_&lt;slug&gt;:*</span><button class="btn-icon" data-copy="app_&lt;slug&gt;:*">${icon('copy')}</button></div>`:`<p class="small muted" style="line-height:1.7">Redis tidak terjangkau. Di Ubuntu: <span class="mono">sudo apt install redis-server</span>, lalu atur host/port/password di Konfigurasi.</p><button class="btn block mt-12" id="redis-cfg3">Buka konfigurasi</button>`}</div></section></div>
  <section class="card mt-16"><div class="card-head"><div><h3>${icon('key')}Penjelajah key</h3><div class="hint">Hanya menampilkan key sesuai prefix yang Anda masukkan.</div></div><div class="toolbar"><input class="input mono" id="key-prefix" placeholder="app_slug:*" value="${attr(alloc[0]?alloc[0].prefix+'*':'*')}" style="width:200px"><button class="btn sm" id="key-scan" ${online?'':'disabled'}>${icon('search')}Pindai</button><button class="btn sm danger" id="key-flush" ${online?'':'disabled'}>${icon('trash')}Hapus prefix</button></div></div><div id="key-results">${online?'<div class="empty small">Masukkan prefix lalu pindai.</div>':'<div class="empty small">Redis offline.</div>'}</div></section>`;
  $$('[data-copy]',el).forEach(b=>b.onclick=()=>copyText(b.dataset.copy));
  const openCfg=()=>redisConfigModal(()=>viewRedis(el));
  ['redis-cfg','redis-cfg2','redis-cfg3'].forEach(id=>$('#'+id,el)?.addEventListener('click',openCfg));
  $('#alloc-add',el)?.addEventListener('click',()=>redisAllocModal(()=>viewRedis(el)));
  $('#alloc-empty',el)?.addEventListener('click',()=>redisAllocModal(()=>viewRedis(el)));
  $('#redis-restart',el)?.addEventListener('click',()=>redisService('restart',()=>viewRedis(el)));
  $$('[data-revoke]',el).forEach(b=>b.onclick=async()=>{ if(await confirmDialog({title:'Cabut alokasi Redis?',message:'User ACL akan dihapus. Aplikasi yang masih berjalan tidak dapat mengakses Redis sampai restart dengan alokasi baru.',typed:'CABUT',confirmLabel:'Cabut',danger:true})){ try{await api('/api/redis/allocations/'+b.dataset.revoke,{method:'DELETE'});toast('Alokasi dicabut');viewRedis(el);}catch(e){toast('Gagal mencabut',e.message,'error');} } });
  $('#key-scan',el)?.addEventListener('click',()=>scanKeys(el,id));
  $('#key-flush',el)?.addEventListener('click',async()=>{ const p=$('#key-prefix',el).value.replace(/\*$/,''); if(!p){toast('Prefix kosong','','error');return;} if(await confirmDialog({title:'Hapus semua key prefix?',message:`Semua key dengan prefix <span class="mono">${esc(p)}*</span> akan dihapus permanen.`,typed:'HAPUS',confirmLabel:'Hapus key',danger:true})){ try{const r=await api('/api/redis/flush',{method:'POST',body:{prefix:p,confirm:'HAPUS'}});toast(`${r.deleted} key dihapus`);scanKeys(el,id);}catch(e){toast('Gagal menghapus',e.detail||e.message,'error');} } });
}
function redisConfigModal(done){
  api('/api/redis/config').then(c=>{
    const m=modal({title:'Konfigurasi Redis',subtitle:'Kredensial disimpan terenkripsi di panel.',body:`<form id="redis-form"><div class="form-grid">${field('Host','<input class="input mono" name="host" value="'+attr(c.host||'127.0.0.1')+'" required>')}${field('Port','<input class="input mono" type="number" name="port" value="'+attr(c.port||6379)+'" required>')}${field('Password','<input class="input" type="password" name="password" placeholder="'+(c.hasPassword?'•••• tersimpan':'opsional')+'">','Biarkan kosong dan centang pertahankan untuk memakai password lama.',true)}${field('','<label class="check-row"><input type="checkbox" name="keep" checked><span class="small">Pertahankan password yang tersimpan</span></label>', '', true)}</div></form>`,footer:`<button class="btn ghost" data-close>Batal</button><button class="btn primary" id="redis-cfg-save">${icon('check')}Simpan &amp; tes</button>`});
    $('#redis-cfg-save',m.overlay).onclick=async()=>{const f=$('#redis-form',m.overlay);const body={host:f.host.value,port:Number(f.port.value),password:f.password.value,keepPassword:f.keep.checked};try{const r=await api('/api/redis/config',{method:'PUT',body});m.close();toast('Redis terhubung',r.ping);done&&done();}catch(e){toast('Koneksi Redis gagal',e.detail||e.message,'error');}};
  }).catch(e=>toast('Gagal memuat konfigurasi',e.message,'error'));
}
function redisAllocModal(done){
  api('/api/apps').then(d=>{
    const apps=d.apps||[];
    const m=modal({title:'Alokasi Redis untuk aplikasi',subtitle:'Panel membuat ACL user dan prefix terisolasi.',body:`<form id="alloc-form">${field('Aplikasi','<select class="select" name="appId">'+apps.map(a=>`<option value="${a.id}">${esc(a.name)} (${esc(a.slug)})</option>`).join('')+'</select>','',false,true)}<div class="alert-strip info mt-12">${icon('lock')}<div>Password ACL dibuat acak, disimpan terenkripsi, dan hanya diinjeksi via <span class="mono">REDIS_URL</span>. Tidak ada tombol reveal.</div></div>${field('ACL yang akan dibuat','<div class="injected"><span class="mono" id="alloc-preview">app_&lt;slug&gt;</span></div>','',true)}</form>`,footer:`<button class="btn ghost" data-close>Batal</button><button class="btn primary" id="alloc-save">${icon('shield')}Buat alokasi</button>`});
    const sel=$('#alloc-form',m.overlay).appId;
    const sync=()=>{const a=apps.find(x=>String(x.id)===sel.value); if(a)$('#alloc-preview',m.overlay).textContent='app_'+a.slug.replace(/-/g,'_');}; sel.onchange=sync;sync();
    $('#alloc-save',m.overlay).onclick=async()=>{try{const r=await api('/api/redis/allocations',{method:'POST',body:{appId:Number(sel.value)}});m.close();toast('Alokasi dibuat',r.aclUser+' · '+r.prefix+'*');done&&done();}catch(e){toast('Gagal membuat alokasi',e.detail||e.message,'error');}};
  }).catch(e=>toast('Gagal memuat aplikasi',e.message,'error'));
}
async function redisService(action,done){
  try{ const r=await api('/api/redis/service',{method:'POST',body:{action}}); toast('Layanan Redis: '+action,r.output||''); setTimeout(done,700); }catch(e){toast('Aksi gagal',e.detail||e.message,'error');}
}
async function scanKeys(el,dbId){
  const prefix=$('#key-prefix',el).value.trim(); const box=$('#key-results',el); box.innerHTML=loadingBlock('Memindai…');
  try{ const d=await api('/api/redis/keys'+qs({prefix,count:100})); const keys=d.keys||[];
    box.innerHTML=keys.length?`<div class="table-wrap"><table><thead><tr><th>Key</th><th>Tipe</th><th>TTL</th><th>Ukuran</th><th></th></tr></thead><tbody>${keys.map(k=>`<tr><td class="mono small">${esc(k.key)}</td><td><span class="tag">${esc(k.type)}</span></td><td>${k.ttl<0?'∞':k.ttl+' dtk'}</td><td>${fmtNum(k.size)}</td><td class="table-actions"><button class="btn-icon" data-key="${attr(k.key)}" title="Lihat">${icon('eye')}</button><button class="btn-icon danger" data-keydel="${attr(k.key)}" title="Hapus">${icon('trash')}</button></td></tr>`).join('')}</tbody></table></div>${d.cursor?`<div class="card-foot"><span class="tiny muted">Scan berlanjut. Persempit prefix untuk hasil lebih baik.</span><button class="btn sm" id="key-more">Lanjut scan</button></div>`:''}`:emptyState('Tidak ada key','Prefix ini belum memiliki key.','','key');
    $$('[data-key]',box).forEach(b=>b.onclick=()=>viewRedisKey(b.dataset.key));
    $$('[data-keydel]',box).forEach(b=>b.onclick=async()=>{if(await confirmDialog({title:'Hapus key?',message:'<span class="mono">'+esc(b.dataset.keydel)+'</span> akan dihapus permanen.',typed:'HAPUS',confirmLabel:'Hapus',danger:true})){try{await api('/api/redis/key/'+encodeURIComponent(b.dataset.keydel),{method:'DELETE'});toast('Key dihapus');scanKeys(el,dbId);}catch(e){toast('Gagal',e.message,'error');}}});
  }catch(e){box.innerHTML=errorBlock(e.detail||e.message);}
}
async function viewRedisKey(key){
  try{ const d=await api('/api/redis/key/'+encodeURIComponent(key)); const m=modal({title:'Key Redis',subtitle:'<span class="mono">'+esc(key)+'</span>',size:'lg',body:`<div class="flex gap-8 mb-12"><span class="tag">${esc(d.type)}</span><span class="tag mono">TTL ${d.ttl<0?'∞':d.ttl+' dtk'}</span></div><pre class="code-block">${esc(typeof d.value==='string'?d.value:JSON.stringify(d.value,null,2))}</pre><div class="field mt-16"><label class="label">Ubah TTL (detik, 0 = permanen)</label><div class="input-group"><input class="input mono" id="key-ttl" type="number" value="${d.ttl<0?0:d.ttl}"><button class="btn" id="key-ttl-save">Simpan</button></div></div>`,footer:`<button class="btn danger" id="key-del">${icon('trash')}Hapus key</button><button class="btn" data-close>Tutup</button>`});
    $('#key-ttl-save',m.overlay).onclick=async()=>{try{await api('/api/redis/key/'+encodeURIComponent(key),{method:'PUT',body:{ttl:Number($('#key-ttl',m.overlay).value)}});toast('TTL disimpan');m.close();}catch(e){toast('Gagal',e.message,'error');}};
    $('#key-del',m.overlay).onclick=async()=>{if(await confirmDialog({title:'Hapus key?',message:'Key dihapus permanen.',typed:'HAPUS',confirmLabel:'Hapus',danger:true})){try{await api('/api/redis/key/'+encodeURIComponent(key),{method:'DELETE'});m.close();toast('Key dihapus');}catch(e){toast('Gagal',e.message,'error');}}};
  }catch(e){toast('Gagal memuat key',e.detail||e.message,'error');}
}

/* ---------- Docker ---------- */
async function viewDocker(el) {
  setPageTitle('Docker');
  viewLoading(el);
  let d; try{ d=await api('/api/docker'); }catch(e){el.innerHTML=errorBlock(e.message);return;}
  const cont=d.containers||[], imgs=d.images||[], vols=d.volumes||[], nets=d.networks||[];
  el.innerHTML=`${pageHead('Docker','Container, image, volume, dan network. Docker bersifat pelengkap; aplikasi native tetap utama.',`<button class="btn sm outline" id="dock-refresh">${icon('refresh')}Muat ulang</button><button class="btn sm danger" id="dock-prune">${icon('trash')}Prune</button>`)}
  ${!d.available?`<div class="card"><div class="card-body">${emptyState('Docker tidak tersedia',`Daemon Docker tidak dapat dihubungi. ${esc(d.error||'')}`,'<button class="btn primary" id="dock-retry">Coba lagi</button>','docker')}<pre class="code-block mt-16">sudo systemctl enable --now docker
sudo usermod -aG docker $USER</pre></div></div>`:`
  <div class="grid g4 mb-16">
    ${statCard('Container',cont.length,`${cont.filter(c=>c.state==='running').length} berjalan`,'docker')}
    ${statCard('Image',imgs.length,`${imgs.filter(i=>i.managed).length} dikelola panel`,'package','info')}
    ${statCard('Volume',vols.length,'Persisten','hardDrive','warn')}
    ${statCard('Versi Docker',`<span style="font-size:19px">${esc(d.version||'—')}</span>`,'Server engine','server')}
  </div>
  <div class="tabs mb-16">${[['containers','Container',cont.length],['images','Image',imgs.length],['volumes','Volume',vols.length],['networks','Network',nets.length]].map(([k,t,n],i)=>`<button class="tab ${i===0?'active':''}" data-dtab="${k}">${t}<span class="count">${n}</span></button>`).join('')}</div>
  <div id="docker-content"></div>`}`;
  $('#dock-refresh',el).onclick=()=>viewDocker(el); $('#dock-retry',el)?.addEventListener('click',()=>viewDocker(el)); $('#dock-prune',el)?.addEventListener('click',()=>openDockerPrune(()=>viewDocker(el)));
  if(!d.available)return;
  const box=$('#docker-content',el);
  const draw=(tab)=>{
    if(tab==='containers') box.innerHTML=`<div class="card"><div class="card-head"><h3>${icon('docker')}Container</h3><span class="tiny muted">Container dengan prefix sp- dikelola SatriaPanel</span></div><div class="table-wrap">${cont.length?`<table><thead><tr><th>Nama</th><th>Image</th><th>Status</th><th>Port</th><th>Dibuat</th><th style="text-align:right">Aksi</th></tr></thead><tbody>${cont.map(c=>`<tr><td><div class="flex gap-8">${icon('docker')}<div><div class="row-title mono">${esc(c.name)}</div><div class="row-sub">${c.managed?'<span class="tag">panel</span>':''}</div></div></div></td><td class="mono small">${esc(c.image)}</td><td>${badge(c.state==='running'?'berjalan':c.state==='exited'?'berhenti':c.state)}<div class="row-sub">${esc(c.status)}</div></td><td class="mono tiny">${esc(c.ports||'—')}</td><td>${esc(c.created)}</td><td><div class="table-actions"><button class="btn-icon" data-clog="${attr(c.id)}" title="Log">${icon('terminal')}</button>${c.state==='running'?`<button class="btn-icon" data-cact="stop" data-cid="${attr(c.id)}" title="Stop">${icon('stop')}</button><button class="btn-icon" data-cact="restart" data-cid="${attr(c.id)}" title="Restart">${icon('restart')}</button>`:`<button class="btn-icon" data-cact="start" data-cid="${attr(c.id)}" title="Start">${icon('play')}</button>`}<button class="btn-icon danger" data-cact="remove" data-cid="${attr(c.id)}" title="Hapus">${icon('trash')}</button></div></td></tr>`).join('')}</tbody></table>`:emptyState('Tidak ada container','Container yang dijalankan panel akan muncul di sini.','','docker')}</div></div>`;
    if(tab==='images') box.innerHTML=`<div class="card"><div class="card-head"><h3>${icon('package')}Image</h3></div><div class="table-wrap">${imgs.length?`<table><thead><tr><th>Repository</th><th>Tag</th><th>ID</th><th>Ukuran</th><th>Dibuat</th><th></th></tr></thead><tbody>${imgs.map(i=>`<tr><td class="mono small">${esc(i.repository)} ${i.managed?'<span class="tag">panel</span>':''}</td><td class="mono">${esc(i.tag)}</td><td class="mono tiny">${esc(i.id)}</td><td>${esc(i.size)}</td><td class="tiny">${esc(i.created)}</td><td><button class="btn-icon danger" data-imgdel="${attr(i.repository+':'+i.tag)}">${icon('trash')}</button></td></tr>`).join('')}</tbody></table>`:'<div class="empty small">Tidak ada image</div>'}</div></div>`;
    if(tab==='volumes') box.innerHTML=`<div class="card"><div class="card-head"><h3>${icon('hardDrive')}Volume</h3></div><div class="table-wrap">${vols.length?`<table><thead><tr><th>Nama</th><th>Driver</th><th>Scope</th></tr></thead><tbody>${vols.map(v=>`<tr><td class="mono">${esc(v.name)}</td><td>${esc(v.driver)}</td><td>${esc(v.scope)}</td></tr>`).join('')}</tbody></table>`:'<div class="empty small">Tidak ada volume</div>'}</div></div>`;
    if(tab==='networks') box.innerHTML=`<div class="card"><div class="card-head"><h3>${icon('layers')}Network</h3></div><div class="table-wrap">${nets.length?`<table><thead><tr><th>Nama</th><th>Driver</th><th>Scope</th><th>ID</th></tr></thead><tbody>${nets.map(n=>`<tr><td class="mono">${esc(n.name)}</td><td>${esc(n.driver)}</td><td>${esc(n.scope)}</td><td class="mono tiny">${esc(n.id)}</td></tr>`).join('')}</tbody></table>`:'<div class="empty small">Tidak ada network</div>'}</div></div>`;
    box.querySelectorAll('[data-clog]').forEach(b=>b.onclick=()=>openDockerLogs(b.dataset.clog));
    box.querySelectorAll('[data-cact]').forEach(b=>b.onclick=async()=>{const act=b.dataset.cact; const name=cont.find(c=>c.id===b.dataset.cid)?.name||b.dataset.cid; if(act==='remove'&&!await confirmDialog({title:'Hapus container?',message:`Container <span class="mono">${esc(name)}</span> akan dihapus paksa.`,typed:'HAPUS',danger:true,confirmLabel:'Hapus container'}))return; try{await api('/api/docker/containers/'+encodeURIComponent(b.dataset.cid)+'/'+act,{method:'POST'});toast('Aksi Docker: '+act);setTimeout(()=>viewDocker(el),500);}catch(e){toast('Aksi gagal',e.detail||e.message,'error');}});
    box.querySelectorAll('[data-imgdel]').forEach(b=>b.onclick=async()=>{if(await confirmDialog({title:'Hapus image?',message:'<span class="mono">'+esc(b.dataset.imgdel)+'</span>',typed:'HAPUS',danger:true,confirmLabel:'Hapus image'})){try{await api('/api/docker/images',{method:'POST',body:{action:'remove',id:b.dataset.imgdel,force:true}});toast('Image dihapus');viewDocker(el);}catch(e){toast('Gagal menghapus',e.detail||e.message,'error');}}});
  };
  $$('[data-dtab]',el).forEach(b=>b.onclick=()=>{$$('[data-dtab]',el).forEach(x=>x.classList.remove('active'));b.classList.add('active');draw(b.dataset.dtab);});
  draw('containers');
}
function openDockerLogs(id){
  const dr=drawer({title:'Log container',subtitle:'<span class="mono">'+esc(id.slice(0,12))+'</span>',wide:true,body:loadingBlock()});
  api('/api/docker/containers/'+encodeURIComponent(id)+'/logs?lines=500').then(d=>{ dr.body.innerHTML=`<div class="log-view" style="max-height:calc(100vh - 150px)">${logHTML(d.lines||[])}</div>`; }).catch(e=>{dr.body.innerHTML=errorBlock(e.detail||e.message);});
}
function openDockerPrune(done){
  const m=modal({title:'Prune Docker',subtitle:'Membersihkan resource tidak terpakai. Tindakan tidak dapat dibatalkan.',body:`<div class="form-grid">${field('Target','<select class="select" name="target"><option value="images">Image tidak terpakai</option><option value="containers">Container berhenti</option><option value="volumes">Volume tidak terpakai</option><option value="networks">Network tidak terpakai</option><option value="builder">Build cache</option></select>','',true)}</div><div class="alert-strip danger mt-12">${icon('alert')}<div>Volume prune dapat menghapus data persisten. Pastikan tidak ada aplikasi yang membutuhkannya.</div></div>`,footer:`<button class="btn ghost" data-close>Batal</button><button class="btn danger" id="prune-go">${icon('trash')}Prune</button>`});
  $('#prune-go',m.overlay).onclick=async()=>{const target=$('select[name=target]',m.overlay).value;if(!await confirmDialog({title:'Konfirmasi prune',message:'Jalankan <span class="mono">docker '+target+' prune</span>?',typed:'HAPUS',confirmLabel:'Jalankan',danger:true}))return;try{const r=await api('/api/docker/prune',{method:'POST',body:{action:'prune',target}});m.close();toast('Prune selesai',r.output||'');done&&done();}catch(e){toast('Prune gagal',e.detail||e.message,'error');}};
}

/* ---------- Cron ---------- */
async function viewCron(el) {
  setPageTitle('Cron');
  viewLoading(el);
  let d,meta; try{ [d,meta]=await Promise.all([api('/api/cron'),api('/api/cron/meta')]); }catch(e){el.innerHTML=errorBlock(e.message);return;}
  const jobs=d.jobs||[];
  el.innerHTML=`${pageHead('Cron','Penjadwal internal panel. Waktu dievaluasi dalam UTC; tampilan mengikuti zona server.',`<button class="btn sm primary" id="cron-add">${icon('plus')}Cron baru</button>`)}
  <div class="grid g4 mb-16">${statCard('Total jadwal',jobs.length,`${jobs.filter(j=>j.enabled).length} aktif`,'clock')}${statCard('Cron bawaan',jobs.filter(j=>j.builtinKey).length,'Tidak dapat dihapus','shield','info')}${statCard('Berjalan sekarang',jobs.filter(j=>j.running).length,'Maks 1 instance per job','activity','warn')}${statCard('Gagal terakhir',jobs.filter(j=>j.lastStatus==='gagal').length,'Periksa riwayat','alert',jobs.some(j=>j.lastStatus==='gagal')?'warn':'')}</div>
  <div class="card"><div class="card-head"><div><h3>${icon('clock')}Jadwal</h3><div class="hint">Ekspresi 5 kolom: menit jam tanggal bulan hari.</div></div><div class="toolbar"><input class="input" id="cron-search" placeholder="Cari cron…" style="width:210px"><select class="select" id="cron-filter" style="width:auto"><option value="">Semua</option><option value="enabled">Aktif</option><option value="disabled">Nonaktif</option><option value="builtin">Bawaan</option><option value="app">Aplikasi</option></select></div></div><div class="table-wrap" id="cron-table"></div></div>`;
  const table=$('#cron-table',el);
  function draw(){
    const q=($('#cron-search',el)?.value||'').toLowerCase(), f=$('#cron-filter',el)?.value;
    const list=jobs.filter(j=>(!q||[j.name,j.command,j.appName].some(v=>(v||'').toLowerCase().includes(q))) && (!f || f==='enabled'&&j.enabled || f==='disabled'&&!j.enabled || f==='builtin'&&j.builtinKey || f==='app'&&j.kind==='app'));
    table.innerHTML=list.length?`<table><thead><tr><th>Nama</th><th>Jadwal</th><th>Target</th><th>Aktif</th><th>Terakhir</th><th>Berikutnya</th><th style="text-align:right">Aksi</th></tr></thead><tbody>${list.map(j=>`<tr><td><div class="flex gap-8">${j.builtinKey?`<span class="badge purple">${esc(j.builtinKey)}</span>`:`<span class="avatar-sm">${icon('clock')}</span>`}<div><div class="row-title">${esc(j.name)}</div><div class="row-sub mono">${esc(j.command||'job bawaan panel')}</div></div></div></td><td><span class="mono small">${esc(j.schedule)}</span><div class="cron-human">${esc(j.human)}</div></td><td>${j.appName?`<span class="tag">${esc(j.appName)}</span>`:'<span class="faint small">Panel</span>'}</td><td><label class="switch"><input type="checkbox" data-toggle="${j.id}" ${j.enabled?'checked':''} ${j.builtinKey==='health_check'?'':''}><span></span></label></td><td>${j.lastStatus?badge(j.lastStatus):'<span class="faint">—</span>'}<div class="row-sub">${timeAgo(j.lastRun)}</div></td><td class="tiny">${j.nextRun?fmtDate(j.nextRun):'<span class="faint">—</span>'}</td><td><div class="table-actions"><button class="btn-icon" data-run="${j.id}" title="Jalankan sekarang" ${j.running?'disabled':''}>${icon('play')}</button><button class="btn-icon" data-history="${j.id}" title="Riwayat">${icon('logs')}</button><button class="btn-icon" data-edit="${j.id}" title="Edit">${icon('edit')}</button>${j.builtinKey?'':`<button class="btn-icon danger" data-del="${j.id}" title="Hapus">${icon('trash')}</button>`}</div></td></tr>`).join('')}</tbody></table>`:emptyState('Tidak ada cron','Buat jadwal pertama untuk tugas berkala.','<button class="btn sm primary" id="cron-empty-add">Cron baru</button>','clock');
    $('#cron-empty-add',table)?.addEventListener('click',()=>openCronForm(null,meta,()=>viewCron(el)));
    table.querySelectorAll('[data-edit]').forEach(b=>b.onclick=()=>openCronForm(jobs.find(j=>j.id===Number(b.dataset.edit)),meta,()=>viewCron(el)));
    table.querySelectorAll('[data-run]').forEach(b=>b.onclick=async()=>{b.disabled=true;try{const r=await api('/api/cron/'+b.dataset.run+'/run',{method:'POST'});toast(r.error?'Cron selesai dengan error':'Cron berhasil dijalankan',r.error||'',r.error?'error':'success');setTimeout(()=>viewCron(el),700);}catch(e){toast('Cron gagal dijalankan',e.message,'error');b.disabled=false;}});
    table.querySelectorAll('[data-history]').forEach(b=>b.onclick=()=>openCronHistory(b.dataset.history));
    table.querySelectorAll('[data-del]').forEach(b=>b.onclick=async()=>{if(await confirmDialog({title:'Hapus cron?',message:'Jadwal dan riwayat run akan dihapus.',typed:'HAPUS',danger:true,confirmLabel:'Hapus'})){try{await api('/api/cron/'+b.dataset.del,{method:'DELETE'});toast('Cron dihapus');viewCron(el);}catch(e){toast('Gagal',e.detail||e.message,'error');}}});
    table.querySelectorAll('[data-toggle]').forEach(cb=>cb.onchange=async()=>{const j=jobs.find(x=>x.id===Number(cb.dataset.toggle));try{await api('/api/cron/'+j.id,{method:'PUT',body:{name:j.name,command:j.command,schedule:j.schedule,enabled:cb.checked,timeoutSeconds:j.timeoutSeconds,workdir:j.workdir}});toast('Jadwal '+(cb.checked?'diaktifkan':'dinonaktifkan'));setTimeout(()=>viewCron(el),400);}catch(e){toast('Gagal',e.detail||e.message,'error');cb.checked=!cb.checked;}});
  }
  // buka form dari deep link
  const qp=new URLSearchParams(location.hash.split('?')[1]||'');
  if(qp.get('job')) setTimeout(()=>openCronForm(jobs.find(j=>j.id===Number(qp.get('job'))),meta,()=>viewCron(el)),50);
  else if(qp.get('app')) setTimeout(()=>openCronForm({appId:Number(qp.get('app'))},meta,()=>viewCron(el)),50);
  $('#cron-add',el).onclick=()=>openCronForm(null,meta,()=>viewCron(el));
  $('#cron-search',el).oninput=draw;$('#cron-filter',el).onchange=draw;draw();
}
function openCronForm(job,meta,done){
  job=job||{}; const editing=!!job.id;
  const m=modal({title:editing?'Edit cron':'Cron baru',subtitle:editing?job.builtinKey?'Job bawaan: perintah dikunci, jadwal & status dapat diubah.':'Ubah jadwal atau perintah.':'Jadwalkan perintah panel atau aplikasi.',size:'lg',body:`<form id="cron-form"><div class="form-grid">
    ${field('Nama','<input class="input" name="name" required value="'+attr(job.name||'')+'" placeholder="Contoh: Bersihkan cache">','',false,true)}
    ${field('Aplikasi','<select class="select" name="appId" '+(editing&&job.builtinKey?'disabled':'')+'><option value="0">Panel / sistem</option>'+((meta.apps||[]).map(a=>`<option value="${a.id}" ${job.appId===a.id?'selected':''}>${esc(a.name)}</option>`).join(''))+'</select>','Kosongkan untuk job sistem.')}
    <div class="full">${field('Perintah','<input class="input mono" name="command" value="'+attr(job.command||'')+'" placeholder="npm run cache:clear" '+(job.builtinKey?'disabled':'')+'>',job.builtinKey?'Perintah job bawaan dikelola panel.':'Dijalankan lewat shell di folder rilis aktif (jika aplikasi dipilih).',true)}
    <div class="full">${field('Jadwal cron','<div class="input-group"><input class="input mono" name="schedule" required value="'+attr(job.schedule||'0 2 * * *')+'"><button type="button" class="btn" id="cron-preset-btn">Preset</button></div>','Format 5 kolom. Panel mengevaluasi dalam UTC.','',true)}
    <div class="full" id="cron-presets"></div>
    ${field('Timeout (detik)','<input class="input mono" type="number" name="timeoutSeconds" min="10" value="'+attr(job.timeoutSeconds||300)+'">','Proses dihentikan bila melewati batas.')}
    ${field('Status','<label class="check-row" style="margin-top:6px"><input type="checkbox" name="enabled" '+(job.enabled!==false?'checked':'')+'><span class="small">Aktifkan jadwal</span></label>')}
    <div class="full" id="cron-preview"></div>
  </div></form>`,footer:`<button class="btn ghost" data-close>Batal</button><button class="btn primary" id="cron-save">${icon('check')}${editing?'Simpan':'Buat cron'}</button>`});
  const form=$('#cron-form',m.overlay), preview=$('#cron-preview',m.overlay);
  const presets=meta.presets||[];
  const togglePresets=()=>{ const box=$('#cron-presets',m.overlay); box.innerHTML=box.innerHTML? '' : `<div class="pill-row">${presets.map(p=>`<button type="button" class="btn sm outline" data-preset="${attr(p.value)}">${esc(p.label)}</button>`).join('')}</div>`; box.querySelectorAll('[data-preset]').forEach(b=>b.onclick=()=>{form.schedule.value=b.dataset.preset;togglePresets();updatePreview();}); };
  $('#cron-preset-btn',m.overlay).onclick=togglePresets;
  const updatePreview=debounce(async()=>{ const sched=form.schedule.value.trim(); if(!sched){preview.innerHTML='';return;} try{const d=await api('/api/cron/preview',{method:'POST',body:{schedule:sched}}); preview.innerHTML=`<div class="injected"><span>${esc(d.human)}</span><span class="muted">${d.next.map(x=>fmtDate(x)).join(' · ')}</span></div>`;}catch(e){preview.innerHTML=`<div class="alert-strip danger">${icon('alert')}<div>Jadwal tidak valid: ${esc(e.detail||e.message)}</div></div>`;}},500);
  form.schedule.oninput=updatePreview; if(job.schedule)updatePreview();
  $('#cron-save',m.overlay).onclick=async()=>{ if(!form.reportValidity())return; const fd=new FormData(form); const body={name:fd.get('name'),command:fd.get('command')||'',schedule:fd.get('schedule'),enabled:!!form.enabled?.checked,timeoutSeconds:Number(fd.get('timeoutSeconds')||300),workdir:job.workdir||'',appId:Number(form.appId?.value||0)}; const b=$('#cron-save',m.overlay);b.disabled=true; try{ if(editing) await api('/api/cron/'+job.id,{method:'PUT',body}); else await api('/api/cron',{method:'POST',body}); m.close();toast(editing?'Cron disimpan':'Cron dibuat');done&&done(); }catch(e){toast('Gagal menyimpan cron',e.detail||e.message,'error');b.disabled=false;} };
}
async function openCronHistory(id){
  const dr=drawer({title:'Riwayat cron',subtitle:'Eksekusi terakhir dan output.',wide:true,body:loadingBlock()});
  try{ const d=await api('/api/cron/'+id); const runs=d.runs||[]; dr.body.innerHTML=`<div class="flex between mb-12"><div><div class="strong">${esc(d.job.name)}</div><div class="tiny muted mono">${esc(d.job.schedule)} · ${esc(d.job.human||'')}</div></div><button class="btn sm primary" id="ch-run">${icon('play')}Jalankan</button></div><div class="stack-sm" id="ch-runs">${runs.length?runs.map(r=>`<div class="card"><div class="card-body" style="padding:12px"><div class="flex between"><div class="flex gap-8">${badge(r.status)}<span class="small mono">${fmtDate(r.startedAt)}</span></div><span class="tiny muted">${r.durationMs?r.durationMs+' ms':''}</span></div><pre class="code-block mt-12" style="max-height:230px;font-size:10.5px">${esc(r.output||'(tanpa output)')}</pre></div></div>`).join(''):emptyState('Belum ada riwayat','Jalankan cron untuk melihat output.','','logs')}</div>`; $('#ch-run',dr.el).onclick=async()=>{try{const r=await api('/api/cron/'+id+'/run',{method:'POST'});toast(r.error?'Selesai dengan error':'Berhasil dijalankan',r.error||'');dr.close();openCronHistory(id);}catch(e){toast('Gagal',e.message,'error');}}; }catch(e){dr.body.innerHTML=errorBlock(e.message);}
}

/* ---------- Files ---------- */
async function viewFiles(el) {
  setPageTitle('Files');
  viewLoading(el);
  let meta; try{ meta=await api('/api/files/meta'); }catch(e){el.innerHTML=errorBlock(e.message);return;}
  const apps=meta.apps||[];
  let appId=Number(new URLSearchParams(location.hash.split('?')[1]||'').get('app')||localStorage.getItem('files-app')||0);
  if(!apps.find(a=>a.id===appId)) appId=apps[0]?.id||0;
  let path='';
  el.innerHTML=`${pageHead('Files','File manager terbatas pada folder aplikasi. Folder internal panel tidak dapat diakses dari sini.',`<select class="select" id="file-app" style="width:auto;min-width:190px">${apps.map(a=>`<option value="${a.id}" ${a.id===appId?'selected':''}>${esc(a.name)}</option>`).join('')}</select>`)}
  ${!apps.length?`<div class="card">${emptyState('Belum ada aplikasi','Folder file muncul setelah aplikasi dibuat.','<a class="btn primary" href="#/apps/new">Tambah aplikasi</a>','folder')}</div>`:`
  <div class="card"><div class="card-head"><div class="flex gap-8" id="file-crumb" style="min-width:0"></div><div class="toolbar"><input type="file" id="file-input" hidden multiple><button class="btn sm" id="file-upload">${icon('upload')}Unggah</button><button class="btn sm outline" id="file-mkdir">${icon('folder')}Folder</button><button class="btn sm outline" id="file-zip">${icon('archive')}Zip</button><button class="btn-icon" id="file-refresh">${icon('refresh')}</button></div></div><div class="table-wrap" id="file-list">${loadingBlock()}</div><div class="card-foot"><span class="tiny muted" id="file-info"></span><span class="tiny muted">Maks unggah 250 MB per berkas</span></div></div>`}`;
  if(!apps.length)return;
  const list=$('#file-list',el);
  const app=()=>apps.find(a=>a.id===Number($('#file-app',el).value))||apps[0];
  async function load(){
    const a=app(); localStorage.setItem('files-app',a.id);
    list.innerHTML=loadingBlock('Membaca folder…');
    try{ const d=await api('/api/files'+qs({appId:a.id,path})); const entries=d.entries||[];
      const crumbs=[{name:esc(a.slug),p:''}]; let accum=''; String(path||'').split('/').filter(Boolean).forEach(seg=>{accum=accum?accum+'/'+seg:seg;crumbs.push({name:esc(seg),p:accum});});
      $('#file-crumb',el).innerHTML=`${icon('folderOpen')}<a href="#" class="small strong" data-path="">${esc(a.slug)}</a>${crumbs.slice(1).map(c=>`<span class="faint">/</span><a href="#" class="small" data-path="${attr(c.p)}">${c.name}</a>`).join('')}`;
      $$('#file-crumb [data-path]',el).forEach(x=>x.onclick=e=>{e.preventDefault();path=x.dataset.path;load();});
      $('#file-info',el).textContent=`${entries.length} item · ${esc(app().workdir)}`;
      list.innerHTML=entries.length?`<table><thead><tr><th>Nama</th><th>Ukuran</th><th>Diubah</th><th>Mode</th><th style="text-align:right">Aksi</th></tr></thead><tbody>${entries.map(f=>`<tr class="${f.isDir?'clickable':''}" data-entry="${attr(JSON.stringify(f))}"><td><div class="flex gap-8">${icon(f.isDir?'folder':'fileText')}<div><div class="row-title mono">${esc(f.name)}</div>${f.symlink?'<div class="row-sub">symlink</div>':''}</div></div></td><td>${f.isDir?'—':fmtBytes(f.size)}</td><td>${fmtDate(f.modTime)}</td><td class="mono tiny">${esc(f.mode)}</td><td><div class="table-actions"><div class="dropdown-wrap"><button class="btn-icon" data-fmore>${icon('more')}</button><div hidden class="dropdown"><button data-open>${icon('eye')}Buka</button>${f.isDir?'':`<button data-dl>${icon('download')}Unduh</button>`}<button data-ren>${icon('edit')}Ganti nama</button><button data-move>${icon('arrowRight')}Pindah</button>${f.name.endsWith('.zip')?`<button data-unzip>${icon('archive')}Ekstrak</button>`:''}<div class="sep"></div><button data-del style="color:#FCA5A5">${icon('trash')}Hapus</button></div></div></div></td></tr>`).join('')}</tbody></table>`:emptyState('Folder kosong','Unggah berkas atau buat folder baru.','','folder');
      list.querySelectorAll('tr[data-entry]').forEach(tr=>{ const f=JSON.parse(tr.dataset.entry); tr.onclick=async e=>{if(e.target.closest('.dropdown-wrap'))return; if(f.isDir){path=f.path;load();}else openFile(f);};
        tr.querySelector('[data-fmore]').onclick=e=>{e.stopPropagation();const dd=tr.querySelector('.dropdown');$$('.dropdown',list).forEach(x=>{if(x!==dd)x.hidden=true});dd.hidden=!dd.hidden;};
        tr.querySelector('[data-open]').onclick=e=>{e.stopPropagation();f.isDir?(path=f.path,load()):openFile(f);};
        tr.querySelector('[data-dl]')?.addEventListener('click',e=>{e.stopPropagation();apiDownload('/api/files/download'+qs({appId:a.id,path:f.path}),f.name).catch(x=>toast('Unduhan gagal',x.message,'error'));});
        tr.querySelector('[data-ren]').onclick=e=>{e.stopPropagation();renameEntry(a.id,f);};
        tr.querySelector('[data-move]').onclick=e=>{e.stopPropagation();moveEntry(a.id,f,path,load);};
        tr.querySelector('[data-unzip]')?.addEventListener('click',async e=>{e.stopPropagation();try{await api('/api/files/op',{method:'POST',body:{appId:a.id,op:'unzip',path:f.path}});toast('Arsip diekstrak');load();}catch(x){toast('Gagal mengekstrak',x.detail||x.message,'error');}});
        tr.querySelector('[data-del]').onclick=e=>{e.stopPropagation();deleteEntry(a.id,f,load);};
      });
    }catch(e){ list.innerHTML=errorBlock(e.detail||e.message); }
  }
  async function openFile(f){
    try{ const d=await api('/api/files/content'+qs({appId:app().id,path:f.path})); const m=modal({title:f.name,subtitle:'<span class="mono">'+esc(f.path)+'</span>',size:'xl',body:`<div class="flex between mb-8"><span class="tiny muted">${fmtBytes(f.size)} · ${esc(f.mode)}</span><span class="tiny muted mono" id="file-mtime">${esc(d.mtime)}</span></div><textarea class="textarea mono" id="file-editor" style="min-height:55vh;font-size:12px">${esc(d.content)}</textarea>`,footer:`<button class="btn ghost" data-close>Tutup</button><button class="btn primary" id="file-save">${icon('check')}Simpan</button>`});
      $('#file-save',m.overlay).onclick=async()=>{try{await api('/api/files/content'+qs({appId:app().id,path:f.path}),{method:'PUT',body:{content:$('#file-editor',m.overlay).value,mtime:d.mtime}});toast('Berkas disimpan');m.close();load();}catch(e){toast('Gagal menyimpan',e.detail||e.message,'error'); if(e.status===409&&await confirmDialog({title:'Berkas berubah di server',message:'Muat ulang isi terbaru? Perubahan Anda saat ini akan hilang.',confirmLabel:'Muat ulang',danger:true})){m.close();openFile(f);} }};
    }catch(e){toast('Gagal membuka berkas',e.detail||e.message,'error');}
  }
  $('#file-app',el).onchange=()=>{path='';load();};
  $('#file-refresh',el).onclick=load;
  $$('#file-crumb [data-path]',el).forEach(()=>{});
  $('#file-upload',el).onclick=()=>$('#file-input',el).click();
  $('#file-input',el).onchange=async e=>{const files=Array.from(e.target.files||[]); for(const f of files){const fd=new FormData();fd.append('file',f);try{await api('/api/files/upload'+qs({appId:app().id,path}),{method:'POST',body:fd});toast('Berkas diunggah',f.name);}catch(x){toast('Unggahan gagal',x.detail||x.message,'error');}} e.target.value='';load();};
  $('#file-mkdir',el).onclick=async()=>{const m=modal({title:'Folder baru',body:field('Nama folder','<input class="input" id="mkdir-name" placeholder="uploads">'),footer:`<button class="btn ghost" data-close>Batal</button><button class="btn primary" id="mkdir-go">Buat</button>`}); $('#mkdir-go',m.overlay).onclick=async()=>{try{await api('/api/files/op',{method:'POST',body:{appId:app().id,op:'mkdir',path,name:$('#mkdir-name',m.overlay).value}});m.close();toast('Folder dibuat');load();}catch(e){toast('Gagal',e.detail||e.message,'error');}};};
  $('#file-zip',el).onclick=async()=>{ const btn=$('#file-zip',el);btn.disabled=true;try{ const res=await fetch('/api/files/zip',{method:'POST',headers:{'Content-Type':'application/json','X-Satria':'1'},credentials:'same-origin',body:JSON.stringify({appId:app().id,path})}); if(!res.ok){const d=await res.json().catch(()=>({}));throw new Error(d.error||d.detail||'Gagal membuat zip');} const b=await res.blob();const a=document.createElement('a');a.href=URL.createObjectURL(b);a.download=(path.split('/').pop()||app().slug)+'.zip';a.click();setTimeout(()=>URL.revokeObjectURL(a.href),2000);toast('Arsip dibuat');}catch(e){toast('Gagal membuat zip',e.message,'error');}finally{btn.disabled=false;} };
  load();
}
function renameEntry(appId,f){
  const m=modal({title:'Ganti nama',body:field('Nama baru','<input class="input" id="ren-name" value="'+attr(f.name)+'">'),footer:`<button class="btn ghost" data-close>Batal</button><button class="btn primary" id="ren-go">Simpan</button>`});
  $('#ren-go',m.overlay).onclick=async()=>{try{await api('/api/files/op',{method:'POST',body:{appId,op:'rename',path:f.path,name:$('#ren-name',m.overlay).value}});m.close();toast('Nama diubah');viewFiles(document.getElementById('content'));}catch(e){toast('Gagal',e.detail||e.message,'error');}};
}
function moveEntry(appId,f,current,reload){
  const m=modal({title:'Pindahkan',subtitle:f.name,body:field('Folder tujuan','<input class="input mono" id="mov-to" placeholder="path/folder" value="'+attr(current)+'">','Kosongkan untuk folder akar aplikasi.'),footer:`<button class="btn ghost" data-close>Batal</button><button class="btn primary" id="mov-go">Pindahkan</button>`});
  $('#mov-go',m.overlay).onclick=async()=>{try{await api('/api/files/op',{method:'POST',body:{appId,op:'move',path:f.path,to:$('#mov-to',m.overlay).value}});m.close();toast('Dipindahkan');reload();}catch(e){toast('Gagal',e.detail||e.message,'error');}};
}
async function deleteEntry(appId,f,reload){
  if(await confirmDialog({title:'Hapus '+f.name+'?',message:f.isDir?'Folder beserta seluruh isinya akan dihapus permanen.':'Berkas akan dihapus permanen.',typed:'HAPUS',danger:true,confirmLabel:'Hapus'})){try{await api('/api/files/op',{method:'POST',body:{appId,op:'delete',path:f.path,confirm:'HAPUS'}});toast('Dihapus');reload();}catch(e){toast('Gagal menghapus',e.detail||e.message,'error');}}
}

/* ---------- Terminal ---------- */
function viewTerminal(el) {
  setPageTitle('Terminal');
  el.innerHTML=`${pageHead('Terminal','Shell interaktif di server. Sesi, IP, dan waktu dibuka/ditutup dicatat di audit log.',`<button class="btn sm outline" id="term-reconnect">${icon('restart')}Sambungkan ulang</button><button class="btn sm outline" id="term-clear">${icon('trash')}Bersihkan</button>`)}
  <div class="alert-strip mb-12">${icon('alert')}<div><b>Perintah berjalan dengan hak akses panel.</b> Terminal ini tidak membatasi perintah. Gunakan dengan hati-hati; semua pembukaan sesi diaudit.</div></div>
  <div class="term-page"><div class="card term-card"><div class="term-head"><div class="term-dots"><i></i><i></i><i></i></div><span class="tiny mono muted" id="term-status">Menghubungkan…</span><span class="tiny faint mono" style="margin-left:auto">bash · xterm-256color</span></div><div class="term-body" id="terminal-box"></div><div class="term-foot"><span>Idle timeout 30 menit · resize otomatis</span><span id="term-size"></span></div></div></div>`;
  const box=$('#terminal-box',el);
  if(!window.Terminal){ box.innerHTML=errorBlock('Komponen xterm tidak termuat','Muat ulang halaman; aset terminal disajikan dari panel.'); return; }
  let term=null, ws=null, fit=null;
  function connect(){
    if(ws){try{ws.close();}catch(_){}}
    box.innerHTML=''; box.style.padding='4px';
    term=new Terminal({cursorBlink:true,fontFamily:"'Fira Code', ui-monospace, monospace",fontSize:13,lineHeight:1.3,scrollback:5000,theme:{background:'#080D18',foreground:'#CBD5E1',cursor:'#22C55E',selectionBackground:'rgba(34,197,94,.25)',black:'#0F172A',red:'#F87171',green:'#4ADE80',yellow:'#FBBF24',blue:'#38BDF8',magenta:'#C4B5FD',cyan:'#22D3EE',white:'#E2E8F0',brightBlack:'#64748B'}});
    fit=new FitAddon.FitAddon(); term.loadAddon(fit); term.open(box);
    setTimeout(()=>{try{fit.fit();}catch(_){}} ,60);
    const cols=term.cols, rows=term.rows;
    const proto=location.protocol==='https:'?'wss:':'ws:';
    ws=new WebSocket(`${proto}//${location.host}/api/terminal/ws?cols=${cols}&rows=${rows}`);
    state.terminalWS=ws; state.terminal=term;
    const status=$('#term-status',el);
    ws.onopen=()=>{status.innerHTML='<span class="live-indicator"><i class="live-dot"></i>Terhubung</span>';$('#term-size',el).textContent=`${term.cols}×${term.rows}`;};
    ws.onmessage=ev=>{ let m;try{m=JSON.parse(ev.data);}catch(_){return;} if(m.type==='output'||m.type==='ready')term.write(m.data||''); if(m.type==='exit'){term.write('\r\n\x1b[33m[sesi berakhir]\x1b[0m\r\n');status.textContent='Sesi berakhir';} if(m.type==='pong'){} };
    ws.onclose=()=>{status.textContent='Terputus';};
    ws.onerror=()=>{status.textContent='Kesalahan koneksi';};
    term.onData(d=>{if(ws?.readyState===1)ws.send(JSON.stringify({type:'input',data:d}));});
    term.onResize(({cols,rows})=>{$('#term-size',el).textContent=`${cols}×${rows}`;if(ws?.readyState===1)ws.send(JSON.stringify({type:'resize',cols,rows}));});
    setTimeout(()=>term.focus(),100);
  }
  const onResize=()=>{try{fit?.fit();}catch(_){}}; window.addEventListener('resize',onResize); onCleanup(()=>window.removeEventListener('resize',onResize));
  $('#term-reconnect',el).onclick=connect; $('#term-clear',el).onclick=()=>term?.clear();
  connect();
}
Object.assign(window,{viewDatabases,viewDatabaseEditor,viewRedis,viewDocker,viewCron,viewFiles,viewTerminal});
