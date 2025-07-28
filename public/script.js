/* global location, WebSocket, fetch  */

(() => {
  /* ------------------------------------------------------------ *
   *  DOM LOOK‑UPS
   * ------------------------------------------------------------ */
  const qs = new URLSearchParams(location.search);
  const $ = id => document.getElementById(id);

  const auctionIdInput = $('auctionIdInput');
  const userIdInput = $('userIdInput');
  const connectBtn = $('connectBtn');

  const amountInput = $('amountInput');
  const bidBtn = $('bidBtn');

  const statusSec = $('status-section');
  const bidSec = $('bid-section');
  const eventsSec = $('events-section');
  const manageSec = $('manage-section');

  const endsAtEl = $('endsAt');
  const highBidEl = $('highBid');
  const highBidderEl = $('highBidder');
  const stateEl = $('state');
  const timeLeftEl = $('timeLeft');
  const eventsEl = $('events');
  const errorEl = $('error');
  const connStatusEl = $('connStatus');

  const listTblBody = $('listTbl').querySelector('tbody');
  const refreshBtn = $('refreshBtn');
  const startBtn = $('startBtn');
  const stopBtn = $('stopBtn');

  const debounce = (fn, ms) => { let t; return (...a) => { clearTimeout(t); t = setTimeout(() => fn(...a), ms); }; };

  /* ------------------------------------------------------------ *
   *  SHARED STATE
   * ------------------------------------------------------------ */
  let ws = null;
  let auctionId = '';
  let userId = '';
  let endsAtUnix = 0;
  let countdownId = null;
  let retryDelay = 3_000;                // ms (exponential back‑off)

  const WS_STATE = Object.freeze({ INIT: 0, OPEN: 1, CLOSING: 2, CLOSED: 3 });
  let wsState = WS_STATE.INIT;

  /* ------------------------------------------------------------ *
   *  INIT
   * ------------------------------------------------------------ */
  connectBtn.addEventListener('click', connect);
  bidBtn.addEventListener('click', placeBid);
  refreshBtn.addEventListener('click', debounce(refreshList, 250));
  startBtn.addEventListener('click', startAuction);
  stopBtn.addEventListener('click', stopAuction);

  // Prefill from ?auction=...&user=... query parameters (handy for deep‑links)
  qs.get('auction') && (auctionIdInput.value = qs.get('auction'));
  qs.get('user') && (userIdInput.value = qs.get('user'));

  refreshList();                             // initial list fetch

  /* ------------------------------------------------------------ *
   *  CONNECTION HANDLING
   * ------------------------------------------------------------ */
  function connect() {
    if (wsState === WS_STATE.OPEN) return;   // already connected

    auctionId = auctionIdInput.value.trim();
    userId = userIdInput.value.trim();
    if (!auctionId || !userId) return alert('Please enter both auction ID and user ID.');

    disable(connectBtn);
    updateConnStatus('connecting…');

    const scheme = location.protocol.startsWith('https') ? 'wss' : 'ws';
    const url = `${scheme}://${location.host}/ws` +
      `?auction_id=${encodeURIComponent(auctionId)}` +
      `&user_id=${encodeURIComponent(userId)}`;

    ws = new WebSocket(url);
    wsState = WS_STATE.INIT;

    ws.onopen = handleOpen;
    ws.onmessage = evt => handleEvent(JSON.parse(evt.data));
    ws.onerror = err => console.error('WS error', err);
    ws.onclose = handleClose;
  }

  function handleOpen() {
    wsState = WS_STATE.OPEN;
    retryDelay = 3_000;                      // reset back‑off
    log('📡 connected');
    updateConnStatus('connected');

    statusSec.hidden = bidSec.hidden = eventsSec.hidden = manageSec.hidden = false;
  }

  function handleClose(ev) {
    wsState = WS_STATE.CLOSED;
    stopCountdown();
    updateConnStatus(`closed (code ${ev.code})`);
    enable(connectBtn);

    if (stateEl.textContent === 'FINISHED') {
      log('🏁 auction ended – no reconnection');
      return;
    }
    const delay = retryDelay;
    retryDelay = Math.min(retryDelay + 3_000, 30_000);  // exponential back‑off

    log(`🔌 disconnected – retrying in ${delay / 1_000}s`);
    setTimeout(connect, delay);
  }

  const updateConnStatus = txt => { connStatusEl.textContent = `WS: ${txt}`; };

  /* ------------------------------------------------------------ *
   *  WS EVENT HANDLING  (server → client)
   * ------------------------------------------------------------ */
  function handleEvent(msg) {
    switch (msg.event) {
      case 'auctions/snapshot': applySnapshot(msg.body); break;
      case 'auctions/start': onStart(msg.body); break;
      case 'auctions/bid': onBid(msg.body); break;
      case 'auctions/bid-ack': onBidAck(); break;
      case 'auctions/stop': onStop(); break;
      case 'error': onError(msg.body?.error); break;
      default: log(`ℹ️ ${JSON.stringify(msg)}`);
    }
  }

  function applySnapshot(snap) {
    // Redis hash uses "ea" (ends‑at); DB snapshots use camel‑case. Handle both.
    const ea = snap.ea ?? snap.endsAt ?? 0;
    if (ea) {
      endsAtUnix = +ea;
      endsAtEl.textContent = tsToLocale(endsAtUnix);
      startCountdown();
    }
    highBidEl.textContent = snap.hb ?? snap.highBid ?? '0';
    highBidderEl.textContent = snap.hbid ?? snap.highBidder ?? '—';
    stateEl.textContent = snap.st ?? snap.status ?? '—';
    log('📷 snapshot received');

    if (stateEl.textContent === 'FINISHED') onStop(); // straight to finished state
  }

  function onStart(body) {
    stateEl.textContent = 'RUNNING';
    endsAtUnix = +body.endsAt;
    endsAtEl.textContent = tsToLocale(endsAtUnix);
    startCountdown();
    log('🚀 auction started');
  }

  function onBid({ amount, bidder }) {
    if (amount) highBidEl.textContent = amount;
    if (bidder) highBidderEl.textContent = bidder;
    log(`💰 ${amount} bid by user ${bidder}`);
  }

  function onBidAck() {
    enable(bidBtn);
    errorEl.textContent = '';
    log('✅ bid acknowledged');
  }

  function onStop() {
    stateEl.textContent = 'FINISHED';
    stopCountdown();
    log('🏁 auction finished');
    ws?.close(1000, 'auction finished');
  }

  function onError(msg) {
    enable(bidBtn);
    errorEl.textContent = msg;
    log(`⚠️ ${msg}`);
  }

  /* ------------------------------------------------------------ *
   *  CLIENT → SERVER ACTIONS
   * ------------------------------------------------------------ */
  function sendWS(event, body) {
    ws?.send(JSON.stringify({ event, body }));
  }

  function placeBid() {
    if (wsState !== WS_STATE.OPEN) return alert('WebSocket not connected.');

    errorEl.textContent = '';
    const amount = +amountInput.value;
    if (!amount) return alert('Please enter a bid amount.');

    disable(bidBtn);
    sendWS('auctions/bid', { amount });
    amountInput.value = '';                 // clear field (ack/error will re‑enable)
  }

  /* ---------- Manage (start / stop) ------------------------------------ */
  async function startAuction() {
    // 5 minute default duration from now
    const endsAtISO = new Date(Date.now() + 5 * 60 * 1e3).toISOString();
    try {
      await api(`/auctions/${auctionId}/start`, 'POST', {
        seller_id: userId,
        ends_at: endsAtISO,
      });
    } catch (e) { alert(e.message); }
  }

  async function stopAuction() {
    try {
      await api(`/auctions/${auctionId}/stop`, 'POST');
    } catch (e) { alert(e.message); }
  }

  /* ------------------------------------------------------------ *
   *  LIST VIEW
   * ------------------------------------------------------------ */
  async function refreshList() {
    try {
      const auctions = await api('/auctions?status=RUNNING');
      const frag = document.createDocumentFragment();

      auctions.forEach(a => {
        const tr = document.createElement('tr');
        tr.innerHTML =
          `<td>${a.id}</td><td>${a.high_bid ?? '—'}</td><td>${a.status}</td>`;
        tr.addEventListener('click', () => { auctionIdInput.value = a.id; connect(); });
        frag.appendChild(tr);
      });
      listTblBody.replaceChildren(frag);
    } catch (err) { console.error(err); }
  }

  /* ------------------------------------------------------------ *
   *  COUNTDOWN (mm:ss)
   * ------------------------------------------------------------ */
  function startCountdown() {
    stopCountdown();                        // ensure only one timer running
    const tick = () => {
      const msLeft = endsAtUnix * 1_000 - Date.now();
      if (msLeft <= 0) { stopCountdown(); return; }

      const secs = Math.floor(msLeft / 1_000);
      const m = String(Math.floor(secs / 60)).padStart(2, '0');
      const s = String(secs % 60).padStart(2, '0');
      timeLeftEl.textContent = `${m}:${s}`;

      const delay = msLeft % 1_000 || 1_000;
      countdownId = setTimeout(tick, delay);
    };
    tick();
  }
  const stopCountdown = () => { clearTimeout(countdownId); countdownId = null; timeLeftEl.textContent = '—'; };

  /* ------------------------------------------------------------ *
   *  UTILITIES
   * ------------------------------------------------------------ */
  async function api(path, method = 'GET', body) {
    const res = await fetch(path, {
      method,
      headers: body && { 'Content-Type': 'application/json' },
      body: body && JSON.stringify(body),
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(data.error || res.statusText);
    return data;
  }

  const log = txt => {
    const div = document.createElement('div');
    div.textContent = `[${new Date().toLocaleTimeString()}] ${txt}`;
    eventsEl.appendChild(div);
    eventsEl.scrollTop = eventsEl.scrollHeight;
  };

  const tsToLocale = ts => new Date(ts * 1_000).toLocaleString();

  const disable = el => (el.disabled = true);
  const enable = el => (el.disabled = false);
})();
