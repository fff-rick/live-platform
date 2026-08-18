const app = document.getElementById('app');
const toastRoot = document.getElementById('toast-root');

const state = {
  token: localStorage.getItem('live_access_token') || '',
  user: JSON.parse(localStorage.getItem('live_user') || 'null'),
  rooms: [],
  room: null,
  stats: { viewer_count: 0, like_count: 0, like_delta: 0 },
  topViewers: [],
  onlineViewers: [],
  gifts: [],
  giftsState: 'idle',
  giftsError: '',
  wallet: null,
  txs: [],
  messages: [],
  modal: null,
  authMode: 'login',
  selectedGift: null,
  centrifuge: null,
  heartbeatTimer: null,
  likeTimer: null,
  pendingLikes: 0,
  giftTimer: null,
  pendingGiftCount: 0,
  pendingGiftID: 0,
  realtime: 'offline',
  currentRoute: location.pathname,
  seenEvents: new Set(),
  seenMessages: new Set(),
};

const gradients = [
  'linear-gradient(135deg,#536976,#292e49)',
  'linear-gradient(135deg,#654ea3,#eaafc8)',
  'linear-gradient(135deg,#11998e,#38ef7d)',
  'linear-gradient(135deg,#fc4a1a,#f7b733)',
  'linear-gradient(135deg,#4776e6,#8e54e9)',
  'linear-gradient(135deg,#ee0979,#ff6a00)',
  'linear-gradient(135deg,#1d4350,#a43931)',
  'linear-gradient(135deg,#0f2027,#2c5364)',
];
const giftEmoji = ['🌸','🚀','💎','🎂','🎆','👑','🎁','⭐'];

function esc(value='') {
  return String(value).replace(/[&<>'"]/g, ch => ({'&':'&amp;','<':'&lt;','>':'&gt;',"'":'&#39;','"':'&quot;'}[ch]));
}
function fmtNum(v=0){ v=Number(v)||0; if(v>=1000000)return (v/1000000).toFixed(1)+'M'; if(v>=10000)return (v/10000).toFixed(1)+'万'; if(v>=1000)return (v/1000).toFixed(1)+'k'; return String(v); }
function fmtTime(v){ if(!v)return ''; const d=new Date(v); return d.toLocaleString('zh-CN',{month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit'}); }
function newKey(){ return crypto.randomUUID ? crypto.randomUUID() : `gift-${Date.now()}-${Math.random().toString(16).slice(2)}`; }
function wsURL(){ const scheme = location.protocol === 'https:' ? 'wss:' : 'ws:'; const localCompose=(location.hostname==='localhost'||location.hostname==='127.0.0.1')&&location.port==='8080'; const host=localCompose?`${location.hostname}:8000`:location.host; return `${scheme}//${host}/connection/websocket`; }

function toast(message,type='info'){
  const el=document.createElement('div'); el.className=`toast ${type}`; el.textContent=message; toastRoot.appendChild(el);
  setTimeout(()=>el.remove(),3200);
}

async function api(path, options={}){
  const headers = new Headers(options.headers || {});
  if(state.token) headers.set('Authorization',`Bearer ${state.token}`);
  if(options.body && !headers.has('Content-Type')) headers.set('Content-Type','application/json');
  const res = await fetch(path,{...options,headers});
  const body = await res.json().catch(()=>({}));
  if(!res.ok){
    const err = new Error(body?.error?.message || body?.message || body?.error || `HTTP ${res.status}`);
    err.status=res.status; err.body=body; err.code=body?.error?.code || body?.code;
    if(res.status===401 && state.token){ logout(false); openAuth('login'); }
    throw err;
  }
  return body;
}

function saveAuth(result){
  state.token=result.access_token; state.user=result.user;
  localStorage.setItem('live_access_token',state.token); localStorage.setItem('live_user',JSON.stringify(state.user));
}
function logout(render=true){
  leaveCurrentRoom();
  state.token=''; state.user=null; localStorage.removeItem('live_access_token'); localStorage.removeItem('live_user');
  disconnectRealtime(); state.wallet=null; state.txs=[]; if(render) renderApp();
}
function openAuth(mode='login'){ state.authMode=mode; state.modal='auth'; renderOverlay(); }
function closeModal(){ state.modal=null; renderOverlay(); }

function navigate(path){
  if(location.pathname.startsWith('/room/') && path!==location.pathname) leaveCurrentRoom();
  if(location.pathname!==path) history.pushState({},'',path);
  state.currentRoute=path; disconnectRealtime(); renderApp();
}
window.addEventListener('popstate',()=>{ leaveCurrentRoom(); state.currentRoute=location.pathname; disconnectRealtime(); renderApp(); });

function leaveCurrentRoom(){
  const roomID=state.room?.room_id; if(!roomID||!state.token)return;
  fetch(`/api/v1/rooms/${roomID}/leave`,{method:'POST',headers:{Authorization:`Bearer ${state.token}`},keepalive:true}).catch(()=>{});
}

function userAvatar(user, className='avatar'){
  const src=user?.avatar || '/svg/布偶.svg';
  return `<img class="${className}" src="${esc(src)}" alt="${esc(user?.nickname||'用户')}的头像">`;
}
function header(){
  return `<header class="topbar">
    <a class="logo" href="/" data-nav><span class="logo-mark">L</span><span>Live<em>Flow</em></span></a>
    <nav class="nav"><a class="${state.currentRoute==='/'?'active':''}" href="/" data-nav>直播首页</a><a href="/studio" data-nav>直播中心</a></nav>
    <div class="search"><input id="room-search" placeholder="搜索直播标题或主播"><button id="search-btn">⌕</button></div>
    <div class="top-spacer"></div>
    <div class="user-actions">
      ${state.user ? `<button class="btn ghost small" id="wallet-btn">◈ 钱包</button><button class="btn primary small" id="studio-btn">＋ 开播</button><div class="user-chip">${userAvatar(state.user)}<span class="name">${esc(state.user.nickname)}</span><button class="btn small" id="logout-btn">退出</button></div>` : `<button class="btn" id="register-btn">注册</button><button class="btn primary" id="login-btn">登录</button>`}
    </div>
  </header>`;
}

function homeTemplate(){
  const cards = state.rooms.length ? state.rooms.map((r,i)=>roomCard(r,i)).join('') : `<div class="empty" style="grid-column:1/-1"><strong>还没有正在直播的房间</strong>登录后点击「开播」创建你的第一个直播间。<div style="margin-top:18px"><button class="btn primary" id="empty-create">立即开播</button></div></div>`;
  return `<div class="shell">${header()}
    <section class="hero"><div class="hero-card"><div class="hero-copy"><div class="eyebrow">REALTIME · HIGH CONCURRENCY</div><h1>高并发直播互动，<br>现在真的能用了。</h1><p>Go + Centrifugo + Redis + Kafka + MySQL。弹幕、点赞、礼物、钱包、在线人数与房主管理全部接入真实后端。</p><div class="hero-badges"><span class="badge">⚡ WebSocket 实时推送</span><span class="badge">🎁 事务礼物链路</span><span class="badge">🛡 Hot Room 自适应降级</span></div></div><div class="hero-orb"></div></div></section>
    <main class="content"><div class="section-head"><div><div class="section-title"><span class="dot"></span><h2>正在直播</h2></div><div class="section-sub">实时读取后端 LIVING 房间与在线数据</div></div><button class="btn ghost small" id="refresh-rooms">↻ 刷新</button></div><div class="room-grid" id="room-grid">${cards}</div></main>
  </div>`;
}
function roomCard(r,i){
  const g=gradients[(Number(r.room_id)+i)%gradients.length];
  return `<article class="room-card" data-room="${r.room_id}"><div class="thumb" style="--thumb:${g}"><span class="live-tag">LIVE</span><span class="view-tag">◉ ${fmtNum(r.viewer_count)} · ♥ ${fmtNum(r.like_count)}</span><div class="play-symbol">▶</div></div><div class="room-info"><div class="room-title">${esc(r.title)}</div><div class="room-meta"><div class="anchor"><span class="mini-avatar">${esc((r.anchor_nickname||'主').slice(0,1))}</span><span>${esc(r.anchor_nickname||`主播 #${r.anchor_id}`)}</span></div><span>#${r.room_id}</span></div></div></article>`;
}

function roomTemplate(){
  if(!state.room) return `<div class="shell">${header()}<main class="content"><div class="empty"><strong>直播间加载中…</strong></div></main></div>`;
  const r=state.room; const isAnchor=state.user && Number(state.user.id)===Number(r.anchor_id); const living=r.status==='LIVING';
  return `<div class="shell">${header()}<main class="room-page">
    <div class="room-topline"><div class="room-heading"><div class="anchor-avatar" style="width:42px;height:42px;font-size:16px">${esc((r.anchor_nickname||'主').slice(0,1))}</div><div><h1>${esc(r.title)}</h1><div class="sub">${esc(r.anchor_nickname||`主播 #${r.anchor_id}`)} · 房间 ${r.room_id}</div></div></div><div>${living?'<span class="live-status"><i></i>直播中</span>':`<span class="status-pill ${r.status}">${r.status}</span>`}${isAnchor&&living?'<button class="btn danger small" id="stop-live" style="margin-left:10px">结束直播</button>':''}</div></div>
    <div class="room-layout">
      <section class="player"><div class="stream-scene"></div><div class="stream-grid"></div><div class="stream-center"><div class="cam">▣</div><strong>媒体流接入位</strong><span>当前项目实现直播互动层；接入 SRS / LiveKit / 云直播后在此播放媒体流</span><code>interaction channel: room:${r.room_id}:stream</code></div><div class="player-top"><span class="player-tag live">LIVE</span><span class="player-tag" id="traffic-tag">互动链路 · ${state.realtime==='online'?'已连接':'连接中'}</span></div><div class="danmaku-layer" id="danmaku-layer"></div><div class="player-controls"><span>◉ ${fmtNum(state.stats.viewer_count)} 在线</span><span>♥ ${fmtNum(state.stats.like_count)} 点赞</span><span class="right">Centrifugo · WebSocket</span></div></section>
      <aside class="chat-panel"><div class="chat-head"><strong>直播互动</strong><span class="chat-status" id="chat-status">${state.realtime==='online'?'● 实时':'○ 离线'}</span></div><div class="chat-list" id="chat-list">${chatHTML(isAnchor)}</div><div class="chat-compose"><form class="compose-box" id="chat-form"><input id="chat-input" maxlength="200" placeholder="${state.user?'发一条友善的弹幕吧':'登录后发送弹幕'}" ${!state.user||!living?'disabled':''}><button ${!state.user||!living?'disabled':''}>发送</button></form><div class="compose-hint"><span>Enter 发送 · 后端限流/敏感词校验</span><span>≤ 200 字</span></div></div></aside>
    </div>
    <section class="below-player"><div class="anchor-bar"><div class="anchor-avatar">${esc((r.anchor_nickname||'主').slice(0,1))}</div><div class="anchor-detail"><strong>${esc(r.anchor_nickname||`主播 #${r.anchor_id}`)}</strong><span>房主 #${r.anchor_id} · ${living?'正在直播':'当前未开播'}</span></div><div class="anchor-spacer"></div><div class="stat-chip"><span>在线 <b id="viewer-count">${fmtNum(state.stats.viewer_count)}</b></span><span>点赞 <b id="like-count">${fmtNum(state.stats.like_count)}</b></span></div><button class="like-btn" id="like-btn" title="点赞" ${!state.user||!living?'disabled':''}>♥</button></div>
      <div class="gift-zone"><div class="gift-zone-head"><strong>礼物互动</strong><span class="wallet-mini">我的余额 <b id="wallet-mini">${state.wallet?fmtNum(state.wallet.balance):'--'}</b> 币 <button class="btn small" id="open-wallet">钱包</button></span></div><div style="display:flex;gap:12px;align-items:flex-start;flex-wrap:wrap"><div class="gift-list" id="gift-list">${giftHTML()}</div><div class="gift-send"><input class="gift-count" id="gift-count" type="number" min="1" max="100" value="1"><button class="btn primary" id="gift-send" ${!state.user||!living?'disabled':''}>赠送</button><button class="btn ghost" id="gift-combo" ${!state.user||!living?'disabled':''}>连击</button><span class="gift-pending" id="gift-pending">${state.pendingGiftCount?`待合并 ×${state.pendingGiftCount}`:''}</span></div></div></div>
    </section>
  </main></div>`;
}
function chatHTML(isAnchor){
  if(!state.messages.length) return `<div class="empty" style="padding:34px 10px;border:0;background:transparent">实时弹幕会显示在这里</div>`;
  return state.messages.slice(-220).map(m=>{
    if(m.kind==='gift') return `<div class="chat-row gift"><span>🎁</span> <b>${esc(m.nickname||`用户 #${m.user_id}`)}</b> 赠送 ${esc(m.gift_name)} × ${m.count}</div>`;
    const mine=state.user&&Number(m.user_id)===Number(state.user.id);
    const mod=isAnchor&&!mine?`<span class="moderate-actions"><button data-mute="${m.user_id}">禁言</button><button data-ban="${m.user_id}">封禁</button></span>`:'';
    return `<div class="chat-row ${mine?'mine':''}"><span class="chat-name">${esc(m.nickname||`用户${m.user_id}`)}</span>${esc(m.content)}${mod}</div>`;
  }).join('');
}
function giftHTML(){
  if(state.giftsState==='loading') return `<span style="color:#999;font-size:12px">礼物加载中…</span>`;
  if(state.giftsState==='error') return `<span style="color:#c24b5e;font-size:12px">礼物加载失败：${esc(state.giftsError||'请稍后重试')}</span><button class="btn ghost small" id="gift-retry" type="button">重试</button>`;
  if(!state.gifts.length) return `<span style="color:#999;font-size:12px">暂时没有可赠送的礼物</span>`;
  if(!state.selectedGift) state.selectedGift=state.gifts[0]?.gift_id;
  return state.gifts.map((g,i)=>`<button class="gift-card ${Number(state.selectedGift)===Number(g.gift_id)?'selected':''}" data-gift="${g.gift_id}"><div class="gift-icon">${giftEmoji[i%giftEmoji.length]}</div><div class="gift-name">${esc(g.name)}</div><div class="gift-price">◈ ${fmtNum(g.price)}</div></button>`).join('');
}

function studioTemplate(){
  return `<div class="shell">${header()}<main class="studio-page"><div class="section-head"><div><div class="section-title"><span class="dot"></span><h2>直播中心</h2></div><div class="section-sub">创建直播间并管理开播状态</div></div></div>
    ${state.user?`<div class="studio-grid"><section class="studio-card"><h3 style="margin-top:0">创建一个直播间</h3><p style="color:#777;font-size:13px">媒体流与互动流解耦。这里先创建互动房间，创建后可以直接开播并进入直播间。</p><form id="create-room-form"><div class="field"><label>直播标题</label><input id="studio-title" maxlength="100" value="Go 高并发直播互动实战" placeholder="输入直播标题"></div><button class="btn primary" type="submit">创建并开播</button></form></section><aside class="studio-preview"><div style="font-size:52px">◉</div><strong style="font-size:20px;margin:8px 0">主播工作台</strong><span style="color:#aab1bd;font-size:12px">创建成功后将自动调用 Room Start API</span></aside></div>`:`<div class="empty"><strong>登录后才能创建直播间</strong><button class="btn primary" id="studio-login" style="margin-top:15px">登录 / 注册</button></div>`}
  </main></div>`;
}

function authModal(){
  return `<div class="modal-backdrop" id="modal-backdrop"><div class="modal"><div class="modal-head"><h3>欢迎来到 LiveFlow</h3><button class="close" id="modal-close">×</button></div><div class="modal-body"><div class="tabs"><button class="${state.authMode==='login'?'active':''}" data-auth-tab="login">登录</button><button class="${state.authMode==='register'?'active':''}" data-auth-tab="register">注册</button></div><form id="auth-form"><div class="field"><label>用户名</label><input id="auth-username" minlength="3" maxlength="32" required autocomplete="username" placeholder="3-32 位字母、数字或下划线"></div>${state.authMode==='register'?'<div class="field"><label>昵称</label><input id="auth-nickname" maxlength="32" required placeholder="你在直播间显示的名字"></div>':''}<div class="field"><label>密码</label><input id="auth-password" type="password" minlength="8" maxlength="128" required autocomplete="current-password" placeholder="至少 8 位"></div><button class="btn primary submit" type="submit">${state.authMode==='login'?'登录':'注册并登录'}</button></form></div></div></div>`;
}
function walletDrawer(){
  const txs = state.txs.length ? state.txs.map(t=>`<div class="tx"><div><div class="desc">${esc(t.biz_type)} · ${esc(t.biz_id)}</div><div class="time">${fmtTime(t.created_at)} · 余额 ${fmtNum(t.balance_after)}</div></div><div class="amount ${t.amount<0?'neg':''}">${t.amount>0?'+':''}${fmtNum(t.amount)}</div></div>`).join('') : `<div style="color:#999;font-size:13px;padding:20px 0">暂无交易流水</div>`;
  return `<div class="drawer-backdrop" id="drawer-backdrop"><aside class="drawer"><div class="drawer-head"><h3>我的钱包</h3><button class="close" id="modal-close">×</button></div><div class="balance-card"><small>虚拟币余额</small><strong>◈ ${state.wallet?fmtNum(state.wallet.balance):'--'}</strong><button class="btn small" id="dev-credit">Demo +10,000</button></div><div class="tx-title">最近流水</div>${txs}</aside></div>`;
}
function renderOverlay(){
  document.querySelectorAll('.modal-backdrop,.drawer-backdrop').forEach(x=>x.remove());
  if(state.modal==='auth') document.body.insertAdjacentHTML('beforeend',authModal());
  if(state.modal==='wallet') document.body.insertAdjacentHTML('beforeend',walletDrawer());
  bindOverlay();
}

async function renderApp(){
  state.currentRoute=location.pathname;
  if(state.currentRoute.startsWith('/room/')){
    const id=Number(state.currentRoute.split('/')[2]);
    app.innerHTML=roomTemplate(); bindCommon(); bindRoom(); await loadTopViewers(id);
    // A newly created room is already present in state, but its supplemental
    // data (stats, gift catalog and wallet) has not been loaded yet.
    if(!state.room || Number(state.room.room_id)!==id || state.giftsState!=='loaded') await loadRoom(id);
    else if(state.user && state.room.status==='LIVING' && !state.centrifuge) await connectRoom(id);
  } else if(state.currentRoute==='/studio'){
    app.innerHTML=studioTemplate(); bindCommon(); bindStudio();
  } else {
    app.innerHTML=homeTemplate(); bindCommon(); bindHome();
    await loadRooms();
  }
  renderOverlay();
}

function bindCommon(){
  document.querySelectorAll('[data-nav]').forEach(a=>a.addEventListener('click',e=>{e.preventDefault();navigate(a.getAttribute('href'));}));
  document.getElementById('login-btn')?.addEventListener('click',()=>openAuth('login'));
  document.getElementById('register-btn')?.addEventListener('click',()=>openAuth('register'));
  document.getElementById('logout-btn')?.addEventListener('click',()=>logout());
  document.getElementById('wallet-btn')?.addEventListener('click',openWallet);
  document.getElementById('studio-btn')?.addEventListener('click',()=>navigate('/studio'));
  const search=document.getElementById('room-search');
  const doSearch=()=>{ const q=(search?.value||'').trim().toLowerCase(); document.querySelectorAll('.room-card').forEach(card=>{ const r=state.rooms.find(x=>String(x.room_id)===card.dataset.room); card.style.display=!q||`${r?.title||''} ${r?.anchor_nickname||''}`.toLowerCase().includes(q)?'':'none'; }); };
  search?.addEventListener('input',doSearch); document.getElementById('search-btn')?.addEventListener('click',doSearch);
}
function bindHome(){
  document.getElementById('refresh-rooms')?.addEventListener('click',loadRooms);
  document.getElementById('empty-create')?.addEventListener('click',()=>state.user?navigate('/studio'):openAuth('login'));
  document.querySelectorAll('.room-card').forEach(c=>c.addEventListener('click',()=>navigate(`/room/${c.dataset.room}`)));
}
function bindStudio(){
  document.getElementById('studio-login')?.addEventListener('click',()=>openAuth('login'));
  document.getElementById('create-room-form')?.addEventListener('submit',async e=>{
    e.preventDefault();
    try{ const title=document.getElementById('studio-title').value.trim(); const room=await api('/api/v1/rooms',{method:'POST',body:JSON.stringify({title})}); const living=await api(`/api/v1/rooms/${room.room_id}/start`,{method:'POST'}); toast('直播间已创建并开播','success'); state.room=living; navigate(`/room/${room.room_id}`); }
    catch(err){ toast(err.message,'error'); }
  });
}
function bindOverlay(){
  document.getElementById('modal-close')?.addEventListener('click',closeModal);
  document.getElementById('modal-backdrop')?.addEventListener('click',e=>{ if(e.target.id==='modal-backdrop') closeModal(); });
  document.getElementById('drawer-backdrop')?.addEventListener('click',e=>{ if(e.target.id==='drawer-backdrop') closeModal(); });
  document.querySelectorAll('[data-auth-tab]').forEach(b=>b.addEventListener('click',()=>{state.authMode=b.dataset.authTab;renderOverlay();}));
  document.getElementById('auth-form')?.addEventListener('submit',handleAuth);
  document.getElementById('dev-credit')?.addEventListener('click',async()=>{try{state.wallet=await api('/api/v1/wallet/dev-credit',{method:'POST',body:JSON.stringify({amount:10000})}); await loadTransactions(); renderOverlay(); updateWalletUI(); toast('Demo 余额已增加','success');}catch(err){toast(err.message,'error')}});
}
async function handleAuth(e){
  e.preventDefault();
  const username=document.getElementById('auth-username').value.trim(); const password=document.getElementById('auth-password').value;
  try{
    const result = state.authMode==='register'
      ? await api('/api/v1/auth/register',{method:'POST',body:JSON.stringify({username,nickname:document.getElementById('auth-nickname').value.trim(),password})})
      : await api('/api/v1/auth/login',{method:'POST',body:JSON.stringify({username,password})});
    saveAuth(result); closeModal(); await loadWallet(); toast(`${result.user.nickname}，欢迎回来`,'success'); renderApp();
  }catch(err){toast(err.message,'error')}
}

async function loadRooms(){
  try{ const data=await api('/api/v1/rooms?status=LIVING&limit=24'); state.rooms=data.items||[]; if(location.pathname==='/'){ app.innerHTML=homeTemplate(); bindCommon(); bindHome(); } }
  catch(err){toast('直播列表加载失败：'+err.message,'error')}
}
async function loadRoom(id){
  try{
    state.room=await api(`/api/v1/rooms/${id}`); state.messages=[]; state.seenMessages.clear(); state.seenEvents.clear(); state.stats={viewer_count:0,like_count:0,like_delta:0};
    state.gifts=[]; state.giftsState='loading'; state.giftsError=''; state.selectedGift=null;
    const [statsResult, historyResult] = await Promise.allSettled([api(`/api/v1/rooms/${id}/stats`),api(`/api/v1/rooms/${id}/messages?limit=50`),loadGifts()]);
    if(statsResult.status==='fulfilled') state.stats=statsResult.value;
    if(historyResult.status==='fulfilled') loadHistory(historyResult.value.items||[]);
    if(state.user) await loadWallet();
    app.innerHTML=roomTemplate(); bindCommon(); bindRoom();
    if(state.user && state.room.status==='LIVING') await connectRoom(id); else if(!state.user) setTimeout(()=>openAuth('login'),200);
  }catch(err){ toast(err.message,'error'); app.innerHTML=`<div class="shell">${header()}<main class="content"><div class="empty"><strong>无法进入直播间</strong>${esc(err.message)}<div style="margin-top:15px"><button class="btn" onclick="history.back()">返回</button></div></div></main></div>`; bindCommon(); }
}
function loadHistory(items){
  for(const evt of items){
    if(!evt?.message_id || state.seenMessages.has(evt.message_id)) continue;
    state.seenMessages.add(evt.message_id);
    if(evt.type==='gift') state.messages.push({kind:'gift',...evt});
    else state.messages.push({kind:'danmaku',...evt});
  }
  trimMessages();
}
function renderTopViewers(){
  const player=document.querySelector('.player'); if(!player)return;
  let box=document.getElementById('top-viewers');
  if(!box){ box=document.createElement('div'); box.id='top-viewers'; box.style.cssText='position:absolute;right:16px;top:14px;z-index:9;display:flex;align-items:center'; player.appendChild(box); }
  box.innerHTML=state.topViewers.map((u,i)=>`<img title="${esc(u.nickname)}${i===0?' · 礼物榜首':''}" src="${esc(u.avatar||'/svg/布偶.svg')}" alt="${esc(u.nickname)}的头像" style="width:36px;height:36px;object-fit:cover;border-radius:50%;border:2px solid ${i===0?'#ffd76a':'#fff'};margin-left:${i?'-9px':'0'};background:#fff;box-shadow:0 2px 10px rgba(0,0,0,.35)">`).join('')+`<button id="viewer-bubble" style="margin-left:8px;border:0;border-radius:16px;background:rgba(0,0,0,.56);color:#fff;padding:7px 10px;font-size:12px">${fmtNum(state.stats.viewer_count)} 人</button>`;
  document.getElementById('viewer-bubble')?.addEventListener('click',()=>loadOnlineViewers(state.room?.room_id));
}
async function loadTopViewers(roomID){
  try{ const data=await api(`/api/v1/rooms/${roomID}/top-viewers`); state.topViewers=data.items||[]; renderTopViewers(); }catch{ state.topViewers=[]; renderTopViewers(); }
}
async function loadOnlineViewers(roomID){
  if(!roomID)return;
  try{const data=await api(`/api/v1/rooms/${roomID}/viewers`);state.onlineViewers=data.items||[];}catch(err){toast('观众列表加载失败：'+err.message,'error');return}
  const player=document.querySelector('.player'); if(!player)return;
  document.getElementById('viewer-popover')?.remove();
  const pop=document.createElement('div');pop.id='viewer-popover';pop.style.cssText='position:absolute;right:16px;top:58px;z-index:10;width:220px;max-height:300px;overflow:auto;background:rgba(17,20,28,.96);border:1px solid rgba(255,255,255,.18);border-radius:12px;padding:10px;box-shadow:0 12px 30px rgba(0,0,0,.35);color:#fff';
  pop.innerHTML=`<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:8px;font-size:12px"><b>在线观众 ${state.onlineViewers.length}</b><button id="viewer-popover-close" style="border:0;background:transparent;color:#fff;font-size:18px">×</button></div>`+state.onlineViewers.map(u=>`<div style="display:flex;align-items:center;gap:8px;padding:6px 2px"><img src="${esc(u.avatar||'/svg/布偶.svg')}" style="width:28px;height:28px;border-radius:50%;object-fit:cover"><span style="font-size:13px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${esc(u.nickname)}</span></div>`).join(''); player.appendChild(pop);document.getElementById('viewer-popover-close')?.addEventListener('click',()=>pop.remove());
}
async function loadGifts(){
  state.giftsState='loading'; state.giftsError='';
  try{
    const data=await api('/api/v1/gifts');
    state.gifts=Array.isArray(data.items)?data.items:[];
    state.selectedGift=state.gifts[0]?.gift_id||null;
    state.giftsState='loaded';
  }catch(err){
    state.gifts=[]; state.selectedGift=null; state.giftsState='error'; state.giftsError=err.message;
    throw err;
  }
}
async function loadWallet(){ if(!state.user)return; try{state.wallet=await api('/api/v1/wallet');}catch{} updateWalletUI(); }
async function loadTransactions(){ if(!state.user)return; try{const x=await api('/api/v1/wallet/transactions');state.txs=x.items||[];}catch{} }
async function openWallet(){ if(!state.user)return openAuth('login'); await Promise.all([loadWallet(),loadTransactions()]); state.modal='wallet';renderOverlay(); }
function updateWalletUI(){ const a=document.getElementById('wallet-mini');if(a&&state.wallet)a.textContent=fmtNum(state.wallet.balance); }

function bindRoom(){
  const id=Number(state.room?.room_id); if(!id)return;
  document.getElementById('chat-form')?.addEventListener('submit',async e=>{e.preventDefault();const input=document.getElementById('chat-input');const content=input.value.trim();if(!content)return;input.value='';try{const evt=await api(`/api/v1/rooms/${id}/danmaku`,{method:'POST',body:JSON.stringify({content})}); addDanmaku(evt,true); if(!evt.broadcasted) toast('热点保护中：该弹幕仅本地可见');}catch(err){toast(err.message,'error')}});
  document.getElementById('like-btn')?.addEventListener('click',queueLike);
  bindGiftSelection();
  document.getElementById('gift-retry')?.addEventListener('click',async()=>{await loadGifts().catch(()=>{});renderGiftList();});
  document.getElementById('gift-send')?.addEventListener('click',sendGift);
  document.getElementById('gift-combo')?.addEventListener('click',queueGiftCombo);
  document.getElementById('open-wallet')?.addEventListener('click',openWallet);
  document.getElementById('stop-live')?.addEventListener('click',async()=>{if(!confirm('确定结束本场直播？'))return;try{state.room=await api(`/api/v1/rooms/${id}/stop`,{method:'POST'});disconnectRealtime();toast('直播已结束','success');renderApp();}catch(err){toast(err.message,'error')}});
  document.getElementById('chat-list')?.addEventListener('click',async e=>{
    const mute=e.target.closest('[data-mute]'),ban=e.target.closest('[data-ban]');
    try{if(mute){await api(`/api/v1/rooms/${id}/mutes`,{method:'POST',body:JSON.stringify({user_id:Number(mute.dataset.mute),duration_seconds:600,reason:'主播前端管理'})});toast('已禁言 10 分钟','success')} if(ban){await api(`/api/v1/rooms/${id}/bans`,{method:'POST',body:JSON.stringify({user_id:Number(ban.dataset.ban),reason:'主播前端管理'})});toast('已封禁用户','success')}}catch(err){toast(err.message,'error')}
  });
}
function bindGiftSelection(){
  document.querySelectorAll('[data-gift]').forEach(b=>b.addEventListener('click',()=>{state.selectedGift=Number(b.dataset.gift);document.querySelectorAll('[data-gift]').forEach(x=>x.classList.toggle('selected',x===b));}));
}
function renderGiftList(){
  const list=document.getElementById('gift-list');
  if(!list)return;
  list.innerHTML=giftHTML();
  bindGiftSelection();
  document.getElementById('gift-retry')?.addEventListener('click',async()=>{await loadGifts().catch(()=>{});renderGiftList();});
}

async function connectionToken(){ const x=await api('/api/v1/realtime/token',{method:'POST'});return x.token; }
async function subscriptionToken(roomId,channel){ const join=await api(`/api/v1/rooms/${roomId}/join`,{method:'POST'}); updateStats(join.stats); return join.subscriptions.find(x=>x.channel===channel)?.token||''; }
async function connectRoom(roomId){
  disconnectRealtime();
  if(typeof window.Centrifuge!=='function'){ toast('实时 SDK 加载失败，请检查网络或将 centrifuge-js 本地化','error'); return; }
  try{
    const [join,rt]=await Promise.all([api(`/api/v1/rooms/${roomId}/join`,{method:'POST'}),api('/api/v1/realtime/token',{method:'POST'})]); updateStats(join.stats); await loadTopViewers(roomId);
    const client=new window.Centrifuge(wsURL(),{token:rt.token,getToken:connectionToken}); state.centrifuge=client;
    client.on('connected',()=>{state.realtime='online';updateRealtimeStatus();});
    client.on('disconnected',()=>{state.realtime='reconnecting';updateRealtimeStatus();});
    client.on('error',ctx=>console.warn('centrifuge',ctx));
    for(const s of join.subscriptions){
      const sub=client.newSubscription(s.channel,{token:s.token,getToken:()=>subscriptionToken(roomId,s.channel)});
      sub.on('publication',ctx=>handlePublication(ctx.data)); sub.on('error',ctx=>console.warn('subscription',s.channel,ctx)); sub.subscribe();
    }
    const personal=client.newSubscription(join.personal_channel); personal.on('publication',ctx=>handlePublication(ctx.data)); personal.subscribe(); client.connect();
    const heartbeatMs=Math.max(5000,(join.heartbeat_interval_seconds||30)*1000);
    state.heartbeatTimer=setInterval(async()=>{try{const v=await api(`/api/v1/rooms/${roomId}/heartbeat`,{method:'POST'});if(v.viewer_count!==undefined){state.stats.viewer_count=v.viewer_count;updateStatsUI();} await loadTopViewers(roomId);}catch{}},heartbeatMs);
  }catch(err){toast('实时连接失败：'+err.message,'error')}
}
function disconnectRealtime(){
  if(state.heartbeatTimer){clearInterval(state.heartbeatTimer);state.heartbeatTimer=null}
  if(state.centrifuge){try{state.centrifuge.disconnect()}catch{} state.centrifuge=null}
  state.realtime='offline';
}
function updateRealtimeStatus(){
  const tag=document.getElementById('traffic-tag'),chat=document.getElementById('chat-status');
  if(tag)tag.textContent=`互动链路 · ${state.realtime==='online'?'已连接':'重连中'}`;
  if(chat){chat.textContent=state.realtime==='online'?'● 实时':'○ 重连中';chat.style.color=state.realtime==='online'?'var(--green)':'#d99800';}
}
function handlePublication(data){
  if(!data)return; if(data.event_id){if(state.seenEvents.has(data.event_id))return;state.seenEvents.add(data.event_id);if(state.seenEvents.size>5000)state.seenEvents.delete(state.seenEvents.values().next().value)}
  if(data.type==='stats') updateStats(data.data||{});
  if(data.type==='danmaku') addDanmaku(data.data||{},false);
  if(data.type==='gift') { addGiftEvent(data.data||{}); loadTopViewers(data.room_id||state.room?.room_id); }
}
function addDanmaku(evt,mine=false){
  if(!evt?.message_id)return; if(state.seenMessages.has(evt.message_id))return; state.seenMessages.add(evt.message_id);
  state.messages.push({kind:'danmaku',...evt}); trimMessages(); renderChat(); flyText(evt.content,mine?'mine':'');
}
function addGiftEvent(evt){
  state.messages.push({kind:'gift',...evt,nickname:`用户 #${evt.user_id}`});trimMessages();renderChat();flyText(`🎁 ${evt.gift_name} × ${evt.count}`,'gift');
}
function trimMessages(){if(state.messages.length>300)state.messages.splice(0,state.messages.length-300)}
function renderChat(){ const el=document.getElementById('chat-list'); if(!el)return; const isAnchor=state.user&&state.room&&Number(state.user.id)===Number(state.room.anchor_id);el.innerHTML=chatHTML(isAnchor);el.scrollTop=el.scrollHeight; }
function flyText(text,cls=''){
  const layer=document.getElementById('danmaku-layer');if(!layer)return;const el=document.createElement('div');el.className=`fly ${cls}`;el.textContent=text;el.style.top=`${8+Math.random()*76}%`;el.style.animationDuration=`${8+Math.random()*4}s`;layer.appendChild(el);setTimeout(()=>el.remove(),12500);
}
function updateStats(v={}){ state.stats={...state.stats,...v};updateStatsUI(); }
function updateStatsUI(){ const a=document.getElementById('viewer-count'),b=document.getElementById('like-count');if(a)a.textContent=fmtNum(state.stats.viewer_count);if(b)b.textContent=fmtNum(state.stats.like_count); const controls=document.querySelector('.player-controls');if(controls)controls.innerHTML=`<span>◉ ${fmtNum(state.stats.viewer_count)} 在线</span><span>♥ ${fmtNum(state.stats.like_count)} 点赞</span><span class="right">Centrifugo · WebSocket</span>`; }

function queueLike(){
  state.pendingLikes++; const btn=document.getElementById('like-btn'); if(btn){btn.style.transform='scale(.84)';setTimeout(()=>btn.style.transform='',120)}
  if(state.pendingLikes>=100){flushLikes();return} if(!state.likeTimer)state.likeTimer=setTimeout(flushLikes,300);
}
async function flushLikes(){
  if(state.likeTimer){clearTimeout(state.likeTimer);state.likeTimer=null} const count=state.pendingLikes;state.pendingLikes=0;if(!count||!state.room)return;
  try{const v=await api(`/api/v1/rooms/${state.room.room_id}/like`,{method:'POST',body:JSON.stringify({count})});state.stats.like_count=v.total;updateStatsUI()}catch(err){toast(err.message,'error')}
}
async function sendGift(){
  if(!state.selectedGift||!state.room)return; const count=Math.max(1,Math.min(100,Number(document.getElementById('gift-count')?.value||1)));
  try{await api(`/api/v1/rooms/${state.room.room_id}/gifts`,{method:'POST',headers:{'Idempotency-Key':newKey()},body:JSON.stringify({gift_id:Number(state.selectedGift),count})});await Promise.all([loadWallet(),loadTopViewers(state.room.room_id)]);toast(`礼物 ×${count} 已送出`,'success')}catch(err){toast(err.message,'error')}
}
function queueGiftCombo(){
  const id=Number(state.selectedGift);if(!id)return;if(state.pendingGiftCount&&state.pendingGiftID!==id)flushGiftCombo();state.pendingGiftID=id;state.pendingGiftCount++;updateGiftPending();if(state.pendingGiftCount>=100){flushGiftCombo();return}if(!state.giftTimer)state.giftTimer=setTimeout(flushGiftCombo,300);
}
function updateGiftPending(){const el=document.getElementById('gift-pending');if(el)el.textContent=state.pendingGiftCount?`待合并 ×${state.pendingGiftCount}`:''}
async function flushGiftCombo(){
  if(state.giftTimer){clearTimeout(state.giftTimer);state.giftTimer=null}const count=state.pendingGiftCount,id=state.pendingGiftID;state.pendingGiftCount=0;state.pendingGiftID=0;updateGiftPending();if(!count||!state.room)return;
  try{await api(`/api/v1/rooms/${state.room.room_id}/gifts`,{method:'POST',headers:{'Idempotency-Key':newKey()},body:JSON.stringify({gift_id:id,count})});await Promise.all([loadWallet(),loadTopViewers(state.room.room_id)]);toast(`连击礼物 ×${count} 已合并提交`,'success')}catch(err){toast(err.message,'error')}
}

window.addEventListener('beforeunload',()=>{leaveCurrentRoom();disconnectRealtime();});
renderApp();
