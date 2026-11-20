/* SatriaPanel — inti antarmuka. Tanpa dependensi jaringan. */
'use strict';
const state = {
  me: null, health: null, settings: null, server: null, unread: 0,
  stream: null, terminal: null, terminalWS: null, pollTimer: null, clockTimer: null,
  route: '', timezone: undefined, appsCache: null, dashboard: null,
};
const App = { cleanup: [], view: null, title: 'Dashboard' };

const $ = (sel, root = document) => root.querySelector(sel);
const $$ = (sel, root = document) => Array.from(root.querySelectorAll(sel));
function esc(v) { return String(v ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])); }
function attr(v) { return esc(v).replace(/\n/g, '&#10;'); }
function qs(obj) { const p = new URLSearchParams(); Object.entries(obj || {}).forEach(([k,v]) => { if (v !== undefined && v !== null && v !== '') p.set(k,v); }); const s=p.toString(); return s ? '?'+s : ''; }
function debounce(fn, ms=250) { let t; return (...a) => { clearTimeout(t); t=setTimeout(()=>fn(...a),ms); }; }
function sleep(ms) { return new Promise(r=>setTimeout(r,ms)); }

/* ---------- ikon SVG (gaya Lucide) ---------- */
const ICONS = {
  dashboard:'<rect x="3" y="3" width="7" height="9" rx="1"/><rect x="14" y="3" width="7" height="5" rx="1"/><rect x="14" y="12" width="7" height="9" rx="1"/><rect x="3" y="16" width="7" height="5" rx="1"/>',
  app:'<rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/>',
  box:'<path d="M21 8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16Z"/><path d="m3.3 7 8.7 5 8.7-5"/><path d="M12 22V12"/>',
  database:'<ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M3 5v14a9 3 0 0 0 18 0V5"/><path d="M3 12a9 3 0 0 0 18 0"/>',
  redis:'<path d="M12 2 2 7l10 5 10-5-10-5Z"/><path d="m2 12 10 5 10-5"/><path d="m2 17 10 5 10-5"/>',
  docker:'<path d="M4 9h4v4H4zM9 9h4v4H9zM14 9h4v4h-4zM9 4h4v4H9zM14 4h4v4h-4z"/><path d="M3 15c2 3 6 4 10 3 4-.6 6-3 7-7 1 .2 2 0 2.5-.6-.5 2.5-2 4-4 4.6"/>',
  clock:'<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
  folder:'<path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z"/>',
  terminal:'<rect x="3" y="4" width="18" height="16" rx="2"/><path d="m7 9 3 3-3 3M13 15h4"/>',
  archive:'<rect x="3" y="4" width="18" height="5" rx="1"/><path d="M5 9v9a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V9M10 13h4"/>',
  cloud:'<path d="M17.5 19a4.5 4.5 0 0 0 .5-8.96A6 6 0 0 0 6.3 8.5 4.5 4.5 0 0 0 6.5 19Z"/>',
  bell:'<path d="M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9"/><path d="M13.7 21a2 2 0 0 1-3.4 0"/>',
  shield:'<path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10Z"/><path d="m9 12 2 2 4-4"/>',
  settings:'<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09a1.65 1.65 0 0 0-1.08-1.51 1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 3.6 15a1.65 1.65 0 0 0-1.51-1H2a2 2 0 1 1 0-4h.09a1.65 1.65 0 0 0 1.51-1.08 1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 8 3.6a1.65 1.65 0 0 0 1-1.51V2a2 2 0 1 1 4 0v.09A1.65 1.65 0 0 0 14 3.6a1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 18.4 8c.3.65.9 1.05 1.6 1.05H21a2 2 0 1 1 0 4h-.09A1.65 1.65 0 0 0 19.4 15Z"/>',
  plus:'<path d="M12 5v14M5 12h14"/>',
  play:'<path d="m7 4 13 8-13 8Z"/>',
  stop:'<rect x="5" y="5" width="14" height="14" rx="1.5"/>',
  restart:'<path d="M3 12a9 9 0 1 0 3-6.7L3 8"/><path d="M3 3v5h5"/>',
  refresh:'<path d="M20 12a8 8 0 1 1-2.3-5.7L20 8"/><path d="M20 3v5h-5"/>',
  trash:'<path d="M3 6h18M8 6V4h8v2M6 6l1 15h10l1-15M10 11v6M14 11v6"/>',
  edit:'<path d="M12 20h9"/><path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4Z"/>',
  external:'<path d="M15 3h6v6M10 14 21 3"/><path d="M21 14v5a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5"/>',
  chevronDown:'<path d="m6 9 6 6 6-6"/>',
  chevronRight:'<path d="m9 18 6-6-6-6"/>',
  chevronLeft:'<path d="m15 18-6-6 6-6"/>',
  search:'<circle cx="11" cy="11" r="7"/><path d="m21 21-4.3-4.3"/>',
  menu:'<path d="M4 6h16M4 12h16M4 18h16"/>',
  close:'<path d="M18 6 6 18M6 6l12 12"/>',
  check:'<path d="m20 6-11 11-5-5"/>',
  alert:'<path d="M10.3 3.3 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.3a2 2 0 0 0-3.4 0Z"/><path d="M12 9v4M12 17h.01"/>',
  info:'<circle cx="12" cy="12" r="9"/><path d="M12 11v5M12 8h.01"/>',
  copy:'<rect x="9" y="9" width="12" height="12" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/>',
  download:'<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4M7 10l5 5 5-5M12 15V3"/>',
  upload:'<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4M17 8l-5-5-5 5M12 3v12"/>',
  file:'<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8Z"/><path d="M14 2v6h6M16 13H8M16 17H8M10 9H8"/>',
  server:'<rect x="2" y="3" width="20" height="7" rx="2"/><rect x="2" y="14" width="20" height="7" rx="2"/><path d="M6 6.5h.01M6 17.5h.01"/>',
  cpu:'<rect x="6" y="6" width="12" height="12" rx="1"/><path d="M9 1v3M15 1v3M9 20v3M15 20v3M1 9h3M1 15h3M20 9h3M20 15h3"/>',
  memory:'<rect x="3" y="6" width="18" height="12" rx="1"/><path d="M7 10v4M11 10v4M15 10v4M7 6V3M12 6V3M17 6V3M7 21v-3M12 21v-3M17 21v-3"/>',
  globe:'<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a15 15 0 0 1 0 18 15 15 0 0 1 0-18Z"/>',
  link:'<path d="M10 13a5 5 0 0 0 7.5.5l2-2a5 5 0 0 0-7-7l-1.1 1.1"/><path d="M14 11a5 5 0 0 0-7.5-.5l-2 2a5 5 0 0 0 7 7l1.1-1.1"/>',
  lock:'<rect x="4" y="10" width="16" height="11" rx="2"/><path d="M8 10V7a4 4 0 0 1 8 0v3"/>',
  eye:'<path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12Z"/><circle cx="12" cy="12" r="3"/>',
  user:'<circle cx="12" cy="8" r="4"/><path d="M4 21a8 8 0 0 1 16 0"/>',
  arrowLeft:'<path d="m12 19-7-7 7-7M19 12H5"/>',
  arrowRight:'<path d="M5 12h14M12 5l7 7-7 7"/>',
  more:'<circle cx="5" cy="12" r="1"/><circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/>',
  logs:'<path d="M4 4h16v16H4z"/><path d="M8 8h8M8 12h8M8 16h5"/>',
  code:'<path d="m16 18 6-6-6-6M8 6l-6 6 6 6"/>',
  key:'<circle cx="7.5" cy="15.5" r="4.5"/><path d="m10.7 12.3 9.3-9.3M17 4l3 3M14 7l3 3"/>',
  wifi:'<path d="M5 12.5a10 10 0 0 1 14 0M8.5 16a5 5 0 0 1 7 0M2 9a15 15 0 0 1 20 0M12 20h.01"/>',
  zap:'<path d="M13 2 3 14h9l-1 8 10-12h-9Z"/>',
  github:'<path d="M15 22v-4a4.8 4.8 0 0 0-1-3.5c3 0 6-2 6-5.5a4.6 4.6 0 0 0-1.2-3.2 4.3 4.3 0 0 0-.1-3.2s-1-.3-3.4 1.3a11.6 11.6 0 0 0-6.6 0C5.2 2.3 4.2 2.6 4.2 2.6a4.3 4.3 0 0 0-.1 3.2A4.6 4.6 0 0 0 3 9c0 3.5 3 5.5 6 5.5a4.8 4.8 0 0 0-1 3.5v4"/><path d="M9 19c-3 .8-3-1.5-4-2"/>',
  gitBranch:'<circle cx="6" cy="4" r="2"/><circle cx="6" cy="20" r="2"/><circle cx="18" cy="6" r="2"/><path d="M6 6v12M18 8a4 4 0 0 1-4 4H8"/>',
  package:'<path d="M12 2 3 7v10l9 5 9-5V7Z"/><path d="m3 7 9 5 9-5M12 12v10"/>',
  activity:'<path d="M3 12h4l3-8 4 16 3-8h4"/>',
  hardDrive:'<path d="M3 13h18M5 13l2-8h10l2 8v6a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2Z"/><path d="M7 17h.01M11 17h.01"/>',
  layers:'<path d="m12 2 10 5-10 5L2 7Z"/><path d="m2 12 10 5 10-5M2 17l10 5 10-5"/>',
  route:'<circle cx="6" cy="19" r="3"/><circle cx="18" cy="5" r="3"/><path d="M9 19h5a4 4 0 0 0 0-8H9a4 4 0 0 1 0-8h1"/>',
  filter:'<path d="M4 4h16l-6 8v6l-4 2v-8Z"/>',
  sun:'<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/>',
  moon:'<path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8Z"/>',
  power:'<path d="M12 2v10M18.4 6.6a9 9 0 1 1-12.8 0"/>',
  clockSmall:'<circle cx="12" cy="12" r="8"/><path d="M12 8v4l2 2"/>',
  folderOpen:'<path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2H3Z"/><path d="M3 9h18l-2 9a2 2 0 0 1-2 1.5H5A2 2 0 0 1 3 18Z"/>',
  fileText:'<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8Z"/><path d="M14 2v6h6M8 13h8M8 17h5"/>',
  rotate:'<path d="M3 12a9 9 0 1 0 9-9 9 9 0 0 0-6.4 2.6L3 8"/><path d="M3 3v5h5"/>',
  send:'<path d="m22 2-7 20-4-9-9-4Z"/><path d="M22 2 11 13"/>',
  plug:'<path d="M12 22v-5M9 8V2M15 8V2M6 8h12v4a6 6 0 0 1-12 0Z"/>',
  gauge:'<path d="M12 14l4-4"/><path d="M3.3 19a10 10 0 1 1 17.4 0"/><circle cx="12" cy="14" r="2"/>',
  list:'<path d="M8 6h13M8 12h13M8 18h13M3 6h.01M3 12h.01M3 18h.01"/>',
  globeLock:'<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a15 15 0 0 1 0 18 15 15 0 0 1 0-18Z"/>',
  gitCommit:'<circle cx="12" cy="12" r="3"/><path d="M3 12h6M15 12h6"/>',
  databaseZap:'<ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M3 5v14a9 3 0 0 0 18 0V5M3 12a9 3 0 0 0 18 0M13 17l2 2 4-4"/>',
  panelLeft:'<rect x="3" y="3" width="18" height="18" rx="2"/><path d="M9 3v18"/>',
  table:'<rect x="3" y="3" width="18" height="18" rx="1.5"/><path d="M3 9h18M3 15h18M9 3v18"/>'
};
function icon(name, cls = '', size) {
  const body = ICONS[name] || ICONS.info;
  const s = size ? ` width="${size}" height="${size}"` : '';
  return `<svg${s} class="ic ${cls}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${body}</svg>`;
}

/* ---------- API ---------- */
class ApiError extends Error { constructor(message, status, detail) { super(message); this.status = status; this.detail = detail; } }
async function api(path, opts = {}) {
  const headers = Object.assign({ 'Accept': 'application/json' }, opts.headers || {});
  let body = opts.body;
  const method = (opts.method || 'GET').toUpperCase();
  if (body && !(body instanceof FormData)) { headers['Content-Type'] = 'application/json'; body = JSON.stringify(body); }
  if (!['GET','HEAD','OPTIONS'].includes(method)) headers['X-Satria'] = '1';
  let res;
  try { res = await fetch(path, { method, headers, body, credentials: 'same-origin', signal: opts.signal }); }
  catch (e) { throw new ApiError('Tidak dapat menghubungi panel.', 0, e.message); }
  const ct = res.headers.get('Content-Type') || '';
  let data = null;
  if (ct.includes('application/json')) { try { data = await res.json(); } catch (_) {} }
  if (!res.ok) {
    if (res.status === 401 && state.me && !path.includes('/auth/')) { state.me = null; location.hash = '#/login'; }
    throw new ApiError(data?.error || `Permintaan gagal (${res.status})`, res.status, data?.detail || '');
  }
  return data;
}
async function apiDownload(path, fallbackName) {
  const res = await fetch(path, { credentials: 'same-origin' });
  if (!res.ok) {
    let msg = 'Unduhan gagal'; try { const d=await res.json(); msg=d.error||msg; } catch(_){}
    throw new ApiError(msg, res.status);
  }
  const blob = await res.blob();
  const cd = res.headers.get('Content-Disposition') || '';
  const m = /filename="?([^";]+)"?/i.exec(cd);
  const a = document.createElement('a');
  a.href = URL.createObjectURL(blob); a.download = m ? m[1] : (fallbackName || 'unduhan');
  document.body.appendChild(a); a.click(); a.remove();
  setTimeout(()=>URL.revokeObjectURL(a.href), 3000);
}

/* ---------- format ---------- */
function tz() {
  const t = state.me?.timezone || state.health?.timezone || state.settings?.general?.timezone;
  if (!t || t === 'Local' || !/^[A-Za-z]+\/[A-Za-z_+-]+$|^UTC$/.test(t)) return undefined;
  try { new Intl.DateTimeFormat('id-ID', { timeZone: t }).format(new Date()); return t; } catch { return undefined; }
}
function fmtDate(v, withTime = true) {
  if (!v) return '—';
  const d = new Date(v);
  if (isNaN(d)) return String(v);
  try {
    return new Intl.DateTimeFormat('id-ID', { timeZone: tz(), day:'2-digit', month:'short', year:'numeric', ...(withTime ? {hour:'2-digit',minute:'2-digit'} : {}) }).format(d);
  } catch { return d.toLocaleString('id-ID'); }
}
function fmtTime(v) { if (!v) return '—'; const d=new Date(v); if(isNaN(d)) return v; try { return new Intl.DateTimeFormat('id-ID',{timeZone:tz(),hour:'2-digit',minute:'2-digit',second:'2-digit'}).format(d);}catch{return d.toLocaleTimeString('id-ID');} }
function timeAgo(v) {
  if (!v) return '—';
  const t = new Date(v).getTime(); if (isNaN(t)) return v;
  const s = Math.round((Date.now()-t)/1000);
  if (s < 20) return 'baru saja';
  if (s < 60) return `${s} dtk lalu`;
  const m = Math.round(s/60); if (m < 60) return `${m} mnt lalu`;
  const h = Math.round(m/60); if (h < 24) return `${h} jam lalu`;
  const d = Math.round(h/24); if (d < 30) return `${d} hari lalu`;
  return fmtDate(v, false);
}
function dur(sec) { if (sec == null || isNaN(sec)) return '—'; if (sec < 60) return `${Math.round(sec)} dtk`; if (sec < 3600) return `${Math.floor(sec/60)} mnt ${Math.round(sec%60)} dtk`; const h=Math.floor(sec/3600); const m=Math.round((sec%3600)/60); return `${h} jam ${m} mnt`; }
function fmtBytes(n) { n = Number(n)||0; if (n < 1024) return `${n} B`; const u=['KB','MB','GB','TB']; let i=-1; do { n/=1024; i++; } while(n>=1024 && i<u.length-1); return `${n.toFixed(n>=10?1:2)} ${u[i]}`; }
function fmtNum(n) { return new Intl.NumberFormat('id-ID').format(Number(n)||0); }
function pct(n) { return `${Number(n||0).toFixed(0)}%`; }
function initials(name) { return String(name||'?').split(/\s+/).map(x=>x[0]).slice(0,2).join('').toUpperCase(); }

/* ---------- status ---------- */
const STATUS = {
  berjalan:['green','Berjalan'], berhenti:['gray','Berhenti'], deploy:['blue','Deploy'], bermasalah:['red','Bermasalah'],
  sehat:['green','Sehat'], gagal:['red','Gagal'], unknown:['gray','Belum diperiksa'], tidak_diketahui:['gray','Belum diperiksa'],
  berhasil:['green','Berhasil'], antre:['amber','Antre'], 'gagal-terpulihkan':['amber','Gagal · dipulihkan'], dibatalkan:['gray','Dibatalkan'],
  membangun:['blue','Membangun'], arsip:['gray','Arsip'], aktif:['green','Aktif'], online:['green','Online'], offline:['red','Offline'],
  terhubung:['green','Terhubung'], belum:['gray','Belum dikonfigurasi'], aktifkan:['amber','Perlu diaktifkan'], tersedia:['green','Tersedia'],
  dihentikan:['gray','Dihentikan'], menunggu:['amber','Menunggu'], berjalan2:['blue','Berjalan']
};
function statusInfo(s) { return STATUS[s] || ['gray', s ? String(s).replace(/_/g,' ') : 'Tidak diketahui']; }
function badge(s, label) { const [c,l] = statusInfo(s); return `<span class="badge ${c}"><i class="dot"></i>${esc(label || l)}</span>`; }
function badgeText(c, label) { return `<span class="badge ${c}">${esc(label)}</span>`; }

/* ---------- toast / modal / confirm ---------- */
function toast(title, detail, type='success') {
  let wrap = $('.toast-wrap');
  if (!wrap) { wrap = document.createElement('div'); wrap.className = 'toast-wrap'; document.body.appendChild(wrap); }
  const el = document.createElement('div'); el.className = `toast ${type}`;
  el.innerHTML = `${icon(type==='error'?'alert':type==='info'?'info':'check')}<div><div class="t">${esc(title)}</div>${detail?`<div class="d">${esc(detail)}</div>`:''}</div>`;
  wrap.appendChild(el);
  setTimeout(()=>{ el.style.opacity='0'; el.style.transform='translateY(6px)'; el.style.transition='.2s'; setTimeout(()=>el.remove(),220); }, 4200);
}
function modal({ title, subtitle, body, footer, size='', onClose, dismissible=true }) {
  const overlay = document.createElement('div'); overlay.className='overlay';
  overlay.innerHTML = `<div class="modal ${size}" role="dialog" aria-modal="true" aria-label="${attr(title)}">
    <div class="modal-head"><div><h3>${esc(title)}</h3>${subtitle?`<p>${subtitle}</p>`:''}</div><button class="icon-btn sm" data-close aria-label="Tutup">${icon('close')}</button></div>
    <div class="modal-body">${body}</div>${footer?`<div class="modal-foot">${footer}</div>`:''}</div>`;
  document.body.appendChild(overlay);
  const close = () => { overlay.remove(); document.removeEventListener('keydown', onKey); onClose && onClose(); };
  const onKey = e => { if (e.key === 'Escape' && dismissible) close(); };
  document.addEventListener('keydown', onKey);
  $$('[data-close]', overlay).forEach(b => b.onclick = close);
  if (dismissible) overlay.addEventListener('mousedown', e => { if (e.target === overlay) close(); });
  setTimeout(()=>{ const f=overlay.querySelector('input,select,textarea,button.primary'); f && f.focus(); }, 30);
  return { overlay, close, body: $('.modal-body', overlay), foot: $('.modal-foot', overlay) };
}
function drawer({ title, subtitle, body, footer, wide=false, onClose }) {
  const ov = document.createElement('div'); ov.className='drawer-overlay';
  const dr = document.createElement('aside'); dr.className=`drawer ${wide?'wide':''}`; dr.setAttribute('role','dialog'); dr.setAttribute('aria-modal','true');
  dr.innerHTML = `<div class="drawer-head"><div><h3>${esc(title)}</h3>${subtitle?`<div class="small muted" style="margin-top:3px">${subtitle}</div>`:''}</div><button class="icon-btn sm" data-close>${icon('close')}</button></div>
    <div class="drawer-body">${body}</div>${footer?`<div class="drawer-foot">${footer}</div>`:''}`;
  document.body.appendChild(ov); document.body.appendChild(dr);
  const close = () => { ov.remove(); dr.remove(); onClose && onClose(); };
  ov.onclick = close; $$('[data-close]', dr).forEach(b=>b.onclick=close);
  document.addEventListener('keydown', function esc(e){ if(e.key==='Escape'){ close(); document.removeEventListener('keydown', esc);} });
  return { el:dr, body:$('.drawer-body',dr), foot:$('.drawer-foot',dr), close };
}
function confirmDialog({ title, message, confirmLabel='Lanjutkan', cancelLabel='Batal', danger=false, typed='', input=false, inputLabel='' }) {
  return new Promise(resolve => {
    let settled = false;
    const finish = v => { if (settled) return; settled = true; resolve(v); };
    const body = `${message?`<p class="small" style="color:#CBD5E1;line-height:1.65">${message}</p>`:''}
      ${typed||input?`<div class="field" style="margin-top:16px"><label class="label">${esc(inputLabel || `Ketik ${typed} untuk konfirmasi`)}</label><input class="input mono" id="confirm-input" placeholder="${attr(typed||'')}" autocomplete="off"><div class="hint">Tindakan ini tidak dapat dibatalkan.</div></div>`:''}`;
    const m = modal({ title, body, size:'sm', onClose:()=>finish(false), footer:`<button class="btn ghost" data-cancel>${esc(cancelLabel)}</button><button class="btn ${danger?'danger':'primary'}" data-ok disabled>${esc(confirmLabel)}</button>` });
    const inp = $('#confirm-input', m.overlay); const ok = $('[data-ok]', m.overlay);
    if (inp) inp.oninput = () => { ok.disabled = inp.value !== typed; };
    else ok.disabled = false;
    $('[data-cancel]', m.overlay).onclick = () => { finish(false); m.close(); };
    ok.onclick = () => { finish(true); m.close(); };
    m.overlay.addEventListener('mousedown', e => { if (e.target===m.overlay) finish(false); });
    m.overlay.querySelector('[data-close]').addEventListener('click', ()=>finish(false));
  });
}
async function copyText(text, label='Disalin') {
  try { await navigator.clipboard.writeText(text); toast(label); }
  catch { const ta=document.createElement('textarea'); ta.value=text; document.body.appendChild(ta); ta.select(); document.execCommand('copy'); ta.remove(); toast(label); }
}

/* ---------- komponen ---------- */
function pageHead(title, sub, actions='', breadcrumb='') {
  return `<div class="page-head"><div>${breadcrumb?`<div class="breadcrumb">${breadcrumb}</div>`:''}<h1>${title}</h1>${sub?`<div class="sub">${sub}</div>`:''}</div>${actions?`<div class="page-actions">${actions}</div>`:''}</div>`;
}
function statCard(label, value, foot='', iconName='activity', tone='') {
  return `<div class="card stat"><div><div class="stat-label">${icon(iconName)}${esc(label)}</div><div class="stat-value">${value}</div></div><div class="stat-foot">${foot}</div></div>`;
}
function emptyState(title, text, action='', iconName='box') {
  return `<div class="empty"><div class="empty-icon">${icon(iconName)}</div><h4>${esc(title)}</h4><p>${text}</p>${action||''}</div>`;
}
function loadingBlock(text='Memuat data…') { return `<div class="loading"><span class="spinner"></span>${esc(text)}</div>`; }
function errorBlock(msg, retry='') { return `<div class="empty"><div class="empty-icon" style="color:#FCA5A5;background:rgba(239,68,68,.1)">${icon('alert')}</div><h4>Gagal memuat</h4><p>${esc(msg)}</p>${retry}</div>`; }
function field(label, html, hint='', full=false, required=false) {
  return `<div class="field ${full?'full':''}"><label class="label">${esc(label)}${required?'<span class="req">*</span>':''}</label>${html}${hint?`<div class="hint">${hint}</div>`:''}</div>`;
}
function switchRow(title, desc, name, checked, disabled=false) {
  return `<div class="field-row"><div><div class="t">${esc(title)}</div>${desc?`<div class="d">${esc(desc)}</div>`:''}</div><label class="switch"><input type="checkbox" name="${attr(name)}" ${checked?'checked':''} ${disabled?'disabled':''}><span></span></label></div>`;
}
function kvList(items) { return `<dl class="kv">${items.map(([k,v])=>`<dt>${esc(k)}</dt><dd>${v}</dd>`).join('')}</dl>`; }
function progressBar(value, tone='') { const v=Math.max(0,Math.min(100,Number(value)||0)); return `<div class="progress ${tone}"><i style="width:${v}%"></i></div>`; }
function timeBadge(iso) { return iso?`<span title="${attr(fmtDate(iso))}">${esc(timeAgo(iso))}</span>`:'<span class="faint">—</span>'; }
function appIcon(a) { return `<span class="app-avatar" style="width:32px;height:32px;border-radius:8px;font-size:12px">${esc(initials(a.name))}</span>`; }

/* ---------- charts (SVG) ---------- */
function lineChart(points, opts={}) {
  const w=800, h=190, padL=36, padR=10, padT=12, padB=24, max=100;
  const pts=(points||[]).filter(p=>p && !isNaN(new Date(p.at||p.sampledAt)));
  if (pts.length<2) return `<div class="empty" style="padding:28px"><p class="small">Data grafik belum cukup. Metrik dikumpulkan setiap 5 detik.</p></div>`;
  const xs = pts.map((p,i)=>padL+i*(w-padL-padR)/(pts.length-1));
  const y = v => padT+(100-Math.max(0,Math.min(100,Number(v)||0)))/100*(h-padT-padB);
  const path = key => pts.map((p,i)=>`${i?'L':'M'}${xs[i].toFixed(1)},${y(p[key]).toFixed(1)}`).join(' ');
  const area = key => `${path(key)} L${xs[xs.length-1].toFixed(1)},${h-padB} L${padL},${h-padB} Z`;
  const grid = [0,25,50,75,100].map(v=>`<g><line x1="${padL}" x2="${w-padR}" y1="${y(v)}" y2="${y(v)}" stroke="rgba(71,85,105,.28)" stroke-dasharray="3 4"/><text x="0" y="${y(v)+3}" fill="#64748B" font-size="9" font-family="Fira Code">${v}%</text></g>`).join('');
  const labels = [0, Math.floor(pts.length/2), pts.length-1].filter((v,i,a)=>a.indexOf(v)===i).map(i=>`<text x="${xs[i]}" y="${h-6}" fill="#64748B" font-size="9" text-anchor="${i===0?'start':i===pts.length-1?'end':'middle'}">${esc(fmtTime(pts[i].at||pts[i].sampledAt))}</text>`).join('');
  return `<div class="chart-box" style="height:${h}px"><svg class="chart-svg" viewBox="0 0 ${w} ${h}" preserveAspectRatio="none" role="img" aria-label="Grafik CPU dan RAM">
    <defs><linearGradient id="gCpu" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#22C55E" stop-opacity=".25"/><stop offset="1" stop-color="#22C55E" stop-opacity="0"/></linearGradient><linearGradient id="gRam" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#38BDF8" stop-opacity=".2"/><stop offset="1" stop-color="#38BDF8" stop-opacity="0"/></linearGradient></defs>
    ${grid}<path d="${area('cpu')}" fill="url(#gCpu)"/><path d="${area('ram')}" fill="url(#gRam)"/>
    <path d="${path('ram')}" fill="none" stroke="#38BDF8" stroke-width="1.6" stroke-linecap="round"/>
    <path d="${path('cpu')}" fill="none" stroke="#22C55E" stroke-width="1.8" stroke-linecap="round"/>
    ${pts.map((p,i)=>p.cpu>=85?`<circle cx="${xs[i]}" cy="${y(p.cpu)}" r="2.6" fill="#F59E0B"/>`:'').join('')}
    ${labels}</svg></div>`;
}
function donutChart(percent, label='Terpakai', color='#22C55E') {
  const p=Math.max(0,Math.min(100,Number(percent)||0)), r=40, c=2*Math.PI*r, off=c-(p/100)*c;
  return `<div class="donut"><svg width="96" height="96" viewBox="0 0 96 96"><circle cx="48" cy="48" r="${r}" fill="none" stroke="#2B3548" stroke-width="8"/><circle cx="48" cy="48" r="${r}" fill="none" stroke="${color}" stroke-width="8" stroke-linecap="round" stroke-dasharray="${c}" stroke-dashoffset="${off}"/></svg><div class="donut-label"><div><b>${pct(p)}</b><span>${esc(label)}</span></div></div></div>`;
}
function bars(items) {
  return items.map(i=>`<div class="bar-row"><span class="lbl" title="${attr(i.label)}">${esc(i.label)}</span><div class="bar-track"><i class="bar-fill ${i.tone||''}" style="width:${Math.max(2,Math.min(100,Number(i.value)||0))}%"></i></div><span class="val">${esc(i.text || pct(i.value))}</span></div>`).join('');
}

/* ---------- shell & nav ---------- */
const NAV = [
  ['Utama', [['dashboard','Dashboard','dashboard'],['apps','Aplikasi','app']]],
  ['Infrastruktur', [['databases','Database','database'],['redis','Redis','redis'],['docker','Docker','docker'],['cron','Cron','clock'],['files','Files','folder'],['terminal','Terminal','terminal']]],
  ['Operasional', [['deployments','Log Deploy','logs'],['backup','Backup','archive'],['cloudflare','Cloudflared & Port','cloud'],['notifications','Notifikasi','bell'],['audit','Audit','shield']]],
  ['Sistem', [['settings','Pengaturan','settings']]]
];
function navHTML(active) {
  return NAV.map(([label,items])=>`<div class="nav-label">${label}</div>${items.map(([id,text,ic])=>`<a class="nav-item ${active===id?'active':''}" href="#/${id}" data-nav="${id}">${icon(ic)}<span>${text}</span>${id==='notifications'?'<span class="nav-badge" data-unread-badge hidden>0</span>':''}</a>`).join('')}`).join('');
}
function serverFootHTML(s) {
  s = s || {};
  return `<div class="flex between"><span class="small strong">${esc(s.serverName || s.hostname || 'Server')}</span><span class="server-dot ${s.online===false?'off':''}"></span></div>
    <div class="tiny muted mono" style="margin-top:3px">${esc(s.hostname || '—')}</div>
    <div class="tiny muted" style="margin-top:9px;display:flex;justify-content:space-between"><span>CPU</span><span class="mono">${pct(s.cpuPercent)}</span></div><div class="mini-meter"><i style="width:${Math.min(100,Number(s.cpuPercent)||0)}%"></i></div>
    <div class="tiny muted" style="margin-top:7px;display:flex;justify-content:space-between"><span>RAM</span><span class="mono">${pct(s.memPercent)}</span></div><div class="mini-meter ram"><i style="width:${Math.min(100,Number(s.memPercent)||0)}%"></i></div>`;
}
function shellHTML() {
  const user = state.me?.user;
  return `<div class="shell">
    <aside class="sidebar" id="sidebar">
      <a href="#/dashboard" class="brand" style="text-decoration:none;color:inherit"><span class="brand-mark">${icon('shield')}</span><div class="brand-copy"><div class="brand-name">SatriaPanel</div><div class="brand-sub">Home Server</div></div></a>
      <nav class="nav" id="nav">${navHTML(currentNav())}</nav>
      <div class="sidebar-foot" id="server-foot">${serverFootHTML(state.server)}</div>
    </aside>
    <div class="main">
      <header class="topbar">
        <button class="icon-btn mobile-menu" id="menu-toggle" aria-label="Buka menu">${icon('menu')}</button>
        <div><div class="topbar-title" id="topbar-title">Dashboard</div><div class="topbar-sub" id="topbar-sub">${esc(state.me?.serverName || '')}</div></div>
        <div class="topbar-actions">
          <button class="search-trigger" id="command-btn">${icon('search')}<span>Cari atau lompat…</span><span class="kbd">Ctrl K</span></button>
          <span class="online-pill"><span class="server-dot"></span>Panel online</span>
          <button class="icon-btn" id="notif-btn" aria-label="Notifikasi">${icon('bell')}<span class="nav-badge danger" id="notif-dot" style="position:absolute;top:2px;right:1px;min-width:16px;height:16px;font-size:9px" hidden>0</span></button>
          <div class="dropdown-wrap">
            <button class="user-btn" id="user-btn" aria-label="Menu akun"><span class="avatar">${esc(initials(user?.username||'A'))}</span><span class="small strong hide-mobile">${esc(user?.username||'admin')}</span>${icon('chevronDown')}</button>
            <div id="user-menu" hidden></div>
          </div>
        </div>
      </header>
      <main class="content" id="content">${loadingBlock()}</main>
    </div>
  </div>
  <div class="drawer-overlay" id="sidebar-overlay" hidden></div>`;
}
function currentNav() {
  const p = (location.hash || '#/dashboard').slice(2).split('/')[0];
  return ({deployments:'deployments', deploy:'deployments'})[p] || p || 'dashboard';
}
async function renderShell() {
  if (!$('.shell')) { document.getElementById('root').innerHTML = shellHTML(); bindShell(); }
  else {
    $('#nav').innerHTML = navHTML(currentNav());
    $('#server-foot').innerHTML = serverFootHTML(state.server);
    const u = state.me?.user;
    $('.avatar').textContent = initials(u?.username||'A');
    $('.user-btn .small').textContent = u?.username || 'admin';
    $('#topbar-sub').textContent = state.me?.serverName || '';
  }
  updateUnread();
  refreshShellServer();
}
function bindShell() {
  $('#menu-toggle').onclick = () => { $('#sidebar').classList.add('open'); $('#sidebar-overlay').hidden = false; };
  $('#sidebar-overlay').onclick = closeSidebar;
  $('#notif-btn').onclick = () => location.hash = '#/notifications';
  $('#command-btn').onclick = commandPalette;
  $('#user-btn').onclick = e => { e.stopPropagation(); toggleUserMenu(); };
  document.addEventListener('click', closeUserMenu);
  document.addEventListener('keydown', e => {
    if ((e.ctrlKey||e.metaKey) && e.key.toLowerCase()==='k') { e.preventDefault(); commandPalette(); }
    if (e.key==='Escape') { closeSidebar(); closeUserMenu(); }
  });
  $$('.nav-item').forEach(a => a.addEventListener('click', closeSidebar));
}
function closeSidebar() { $('#sidebar')?.classList.remove('open'); const o=$('#sidebar-overlay'); if(o) o.hidden=true; }
function toggleUserMenu() {
  const el = $('#user-menu'); if (!el) return;
  if (!el.hidden) { el.hidden = true; return; }
  el.className='dropdown'; el.hidden=false;
  el.innerHTML = `<div style="padding:8px 10px"><div class="small strong">${esc(state.me?.user?.username||'admin')}</div><div class="tiny muted">${esc(state.me?.serverName||'')}</div></div><div class="sep"></div>
    <button data-go="settings">${icon('settings')}Pengaturan</button>
    <button data-go="audit">${icon('shield')}Audit log</button><div class="sep"></div>
    <button data-logout style="color:#FCA5A5">${icon('power')}Keluar</button>`;
  el.querySelectorAll('[data-go]').forEach(b=>b.onclick=()=>{el.hidden=true; location.hash='#/'+b.dataset.go;});
  el.querySelector('[data-logout]').onclick = logout;
}
function closeUserMenu(){ const el=$('#user-menu'); if(el) el.hidden=true; }
async function logout() {
  try { await api('/api/auth/logout',{method:'POST'}); } catch(_){}
  state.me=null; location.hash='#/login'; location.reload();
}
function updateUnread() {
  const dot = $('#notif-dot'), badge = $('[data-unread-badge]');
  const n = state.unread||0;
  if (dot) { dot.hidden = n===0; dot.textContent = n>99?'99+':n; }
  if (badge) { badge.hidden = n===0; badge.textContent = n>99?'99+':n; }
}
async function refreshShellServer() {
  try {
    const d = await api('/api/dashboard');
    state.dashboard = d; state.server = d.server;
    const foot=$('#server-foot'); if (foot) foot.innerHTML=serverFootHTML(d.server);
    state.unread = d.stats?.unread || 0; updateUnread();
  } catch(_) {}
}
function setPageTitle(title, sub) {
  App.title = title;
  const t=$('#topbar-title'); if(t) t.textContent=title;
  const s=$('#topbar-sub'); if(s && sub!==undefined) s.textContent=sub || state.me?.serverName || '';
  document.title = `${title} · SatriaPanel`;
}
function commandPalette() {
  const items = [...NAV.flatMap(([g,arr])=>arr.map(([id,text,ic])=>({id,text,group:g,ic}))),
    {id:'apps/new',text:'Tambah aplikasi baru',group:'Aksi',ic:'plus'}, {id:'terminal',text:'Buka terminal',group:'Aksi',ic:'terminal'}];
  const m = modal({ title:'Lompat ke…', subtitle:'Cari menu atau tindakan. Tekan Esc untuk menutup.', body:`<input class="input" id="cmdk" placeholder="Ketik nama menu…" autocomplete="off"><div id="cmdk-list" style="margin-top:10px;max-height:330px;overflow:auto"></div>`, size:'sm' });
  const render = q => {
    const list = items.filter(i=>!q || i.text.toLowerCase().includes(q.toLowerCase()) || i.id.includes(q.toLowerCase()));
    $('#cmdk-list', m.overlay).innerHTML = list.length ? list.map(i=>`<button class="dropdown button" data-cmd="${i.id}" style="display:flex;align-items:center;gap:9px;width:100%;padding:9px 10px;background:none;border:0;border-radius:7px;text-align:left;color:#CBD5E1">${icon(i.ic)}<span>${esc(i.text)}</span><span class="tiny faint" style="margin-left:auto">${esc(i.group)}</span></button>`).join('') : `<div class="empty small">Tidak ada hasil</div>`;
    $$('[data-cmd]', m.overlay).forEach(b=>b.onclick=()=>{ m.close(); location.hash='#/'+b.dataset.cmd; });
  };
  render('');
  $('#cmdk', m.overlay).oninput = e => render(e.target.value);
}

/* ---------- view lifecycle ---------- */
function cleanupView() {
  App.cleanup.forEach(fn => { try { fn(); } catch(_){} });
  App.cleanup = [];
  if (state.stream) { try { state.stream.close(); } catch(_){} state.stream=null; }
  if (state.terminalWS) { try { state.terminalWS.close(); } catch(_){} state.terminalWS=null; }
  if (state.terminal) { try { state.terminal.dispose(); } catch(_){} state.terminal=null; }
  clearInterval(state.pollTimer); state.pollTimer=null;
}
function onCleanup(fn) { App.cleanup.push(fn); }
async function renderView(html, after) {
  const content = $('#content');
  content.innerHTML = html;
  if (after) await after(content);
  content.scrollTop = 0;
}
function viewLoading(content, text) { content.innerHTML = loadingBlock(text); }

/* ---------- sse ---------- */
function openSSE(url, handlers) {
  if (state.stream) { try{state.stream.close();}catch(_){} }
  const es = new EventSource(url);
  state.stream = es;
  Object.entries(handlers).forEach(([event,fn]) => es.addEventListener(event, e => { try { fn(JSON.parse(e.data)); } catch(err) { console.warn(err); } }));
  es.onerror = () => {};
  return es;
}
function logHTML(lines, showNumbers=false) {
  const arr = Array.isArray(lines) ? lines : String(lines||'').split('\n');
  return arr.map((l,i)=>{
    const cls = /\b(error|gagal|failed|fatal|panic)\b/i.test(l)?'err':/\b(warn|peringatan)\b/i.test(l)?'warn':/\b(ok|berhasil|success|sehat)\b/i.test(l)?'ok':'';
    return `<div class="log-line"><span class="ln">${showNumbers?i+1:''}</span><span class="${cls}">${esc(l)}</span></div>`;
  }).join('');
}

/* expose */
Object.assign(window,{state,App,$,$$,esc,attr,qs,debounce,icon,api,apiDownload,ApiError,fmtDate,fmtTime,timeAgo,dur,fmtBytes,fmtNum,pct,initials,badge,badgeText,statusInfo,toast,modal,drawer,confirmDialog,copyText,pageHead,statCard,emptyState,loadingBlock,errorBlock,field,switchRow,kvList,progressBar,timeBadge,appIcon,lineChart,donutChart,bars,renderShell,setPageTitle,commandPalette,cleanupView,onCleanup,renderView,openSSE,logHTML,currentNav,tz});
