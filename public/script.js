/* global location, WebSocket, fetch */

(() => {
  /* ------------------------------------------------------------ *
   *  DOM LOOK‑UPS
   * ------------------------------------------------------------ */
  const qs = new URLSearchParams(location.search);
  const $ = id => document.getElementById(id);

  const auctionIdInput = $('auctionIdInput');
  const connectBtn = $('connectBtn');
  const bidderIdInput = $('bidderIdInput');
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

  const debounce = (fn, ms) => {
    let handle; return (...a) => { clearTimeout(handle); handle = setTimeout(() => fn(...a), ms); };
  };


  /* ------------------------------------------------------------ *
   *  SHARED STATE
   * ------------------------------------------------------------ */
  let ws = null;
  let auctionId = '';
  let endsAtUnix = 0;
  let countdownId = null;
  let retryDelay = 3000;               // ms (exp‑backoff)

  const WS_STATE = Object.freeze({
    INIT: 0,
    OPEN: 1,
    CLOSING: 2,
    CLOSED: 3,
  });
  let wsState = WS_STATE.INIT;

  /* ------------------------------------------------------------ *
   *  INIT
   * ------------------------------------------------------------ */
  connectBtn.addEventListener('click', connect);
  bidBtn.addEventListener('click', placeBid);
  refreshBtn.addEventListener('click', debounce(refreshList, 250));

  startBtn.addEventListener('click', startAuction);
  stopBtn.addEventListener('click', stopAuction);

  qs.get('auction') && (auctionIdInput.value = qs.get('auction'));

  refreshList(); // preload table

  /* ------------------------------------------------------------ *
   *  CONNECTION HANDLING
   * ------------------------------------------------------------ */
  function connect() {
    if (wsState === WS_STATE.OPEN) return;           // already connected
    auctionId = auctionIdInput.value.trim();
    if (!auctionId) return alert('Please enter an auction ID.');

    disable(connectBtn);
    updateConnStatus('connecting…');

    const scheme = location.protocol.startsWith('https') ? 'wss' : 'ws';
    const url = `${scheme}://${location.host}/ws?auction_id=${encodeURIComponent(auctionId)}`;

    ws = new WebSocket(url);
    wsState = WS_STATE.INIT;

    ws.onopen = handleOpen;
    ws.onmessage = evt => handleEvent(JSON.parse(evt.data));
    ws.onerror = err => console.error('WS error', err);
    ws.onclose = handleClose;
  }

  function handleOpen() {
    wsState = WS_STATE.OPEN;
    retryDelay = 3000; // reset back‑off
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
    // exponential back‑off
    const delay = retryDelay;
    retryDelay = Math.min(retryDelay + 3000, 30_000);

    log(`🔌 disconnected – retrying in ${delay / 1000}s`);
    setTimeout(connect, delay);
  }

  function updateConnStatus(text) { connStatusEl.textContent = `WS: ${text}`; }

  /* ------------------------------------------------------------ *
   *  SERVER → CLIENT EVENTS
   * ------------------------------------------------------------ */
  function handleEvent(msg) {
    switch (msg.event) {
      case 'snapshot': applySnapshot(msg.data); break;
      case 'start': onStart(msg); break;
      case 'bid': onBid(msg); break;
      case 'stop': onStop(); break;
      default: log(`ℹ️ ${JSON.stringify(msg)}`);
    }
  }

  function applySnapshot(snap) {
    if (snap.ea) {
      endsAtUnix = +snap.ea;
      endsAtEl.textContent = tsToLocale(endsAtUnix);
      startCountdown();
    }
    highBidEl.textContent = snap.hb ?? '0';
    highBidderEl.textContent = snap.hbid ?? '—';
    stateEl.textContent = snap.st ?? '—';
    log('📷 snapshot received');

    // ★ If the snapshot tells us the auction is over, close the socket immediately.
    if (snap.st === 'FINISHED') {
      onStop();               // re‑use existing cleanup logic
    }
  }

  function onStart({ endsAt }) {
    stateEl.textContent = 'RUNNING';
    endsAtUnix = +endsAt;
    endsAtEl.textContent = tsToLocale(endsAtUnix);
    startCountdown();
    log('🚀 auction started');
  }
  function onBid({ amount, bidder }) {
    if (amount) highBidEl.textContent = amount;
    if (bidder) highBidderEl.textContent = bidder;
    log(`💰 ${amount} bid by user ${bidder}`);
  }
  function onStop() {
    stateEl.textContent = 'FINISHED';
    stopCountdown();
    log('🏁 auction finished');
    ws?.close(1000, 'auction finished');
  }

  /* ------------------------------------------------------------ *
   *  FORM ACTIONS
   * ------------------------------------------------------------ */
  async function placeBid() {
    errorEl.textContent = '';
    const bidderId = bidderIdInput.value.trim();
    const amount = +amountInput.value;

    if (!bidderId || !amount) {
      return alert('Provide both bidder ID and bid amount.');
    }
    disable(bidBtn);
    try {
      await api(`/auctions/${encodeURIComponent(auctionId)}/bid`, 'POST', {
        bidder_id: bidderId, amount
      });
      amountInput.value = '';
    } catch (err) {
      errorEl.textContent = err.message;
    } finally {
      enable(bidBtn);
    }
  }

  /* ---------- Manage (start/stop) ------------------------------ */
  async function startAuction() {
    try {
      await api(`/auctions/${auctionId}/start`, 'POST', {
        seller_id: 'seller123',
        ends_at: new Date(Date.now() + 5 * 60 * 1e3).toISOString()
      });
    } catch (e) { alert(e.message); }
  }
  async function stopAuction() {
    try {
      await api(`/auctions/${auctionId}/stop?user_id=seller123`, 'POST');
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
        tr.innerHTML = `<td>${a.id}</td><td>${a.high_bid ?? '—'}</td><td>${a.status}</td>`;
        tr.addEventListener('click', () => { auctionIdInput.value = a.id; connect(); });
        frag.appendChild(tr);
      });
      listTblBody.replaceChildren(frag);
    } catch (err) { console.error(err); }
  }

  /* ------------------------------------------------------------ *
   *  COUNTDOWN
   * ------------------------------------------------------------ */
  function startCountdown() {
    stopCountdown();           // ensure only one timer

    function tick() {
      const nowMs = Date.now();
      const msLeft = endsAtUnix * 1000 - nowMs;

      if (msLeft <= 0) {            // auction over
        stopCountdown();
        return;
      }

      const secs = Math.floor(msLeft / 1000);
      const m = String(Math.floor(secs / 60)).padStart(2, '0');
      const s = String(secs % 60).padStart(2, '0');
      timeLeftEl.textContent = `${m}:${s}`;

      /* schedule exactly at the next second boundary              *
       * (e.g. if 734 ms remain, wait 734 ms, else wait 1000 ms)   */
      const delay = msLeft % 1000 || 1000;
      countdownId = setTimeout(tick, delay);
    }

    tick();                     // kick‑off immediately
  }

  function stopCountdown() {
    clearTimeout(countdownId);
    countdownId = null;
    timeLeftEl.textContent = '—';
  }

  /* ------------------------------------------------------------ *
   *  UTIL
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

  function log(text) {
    const div = document.createElement('div');
    div.textContent = `[${new Date().toLocaleTimeString()}] ${text}`;
    eventsEl.appendChild(div);
    eventsEl.scrollTop = eventsEl.scrollHeight;
  }

  function tsToLocale(ts) { return new Date(ts * 1000).toLocaleString(); }



  const disable = el => (el.disabled = true);
  const enable = el => (el.disabled = false);

})();
