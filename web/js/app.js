'use strict';

// ── State ─────────────────────────────────────────────────────────────────────
const roomID    = window.location.pathname.split('/').pop();
let sessionID   = localStorage.getItem('flip7_session_' + roomID) || '';
let playerName  = localStorage.getItem('flip7_name') || '';
let playerID    = '';
let isHost      = false;
let gameState        = null;
let prevPlayers      = [];   // previous player states for animation diffs
let ws               = null;
let reconnectDelay   = 1000;
let reconnectTimer   = null; // handle for the scheduled reconnect setTimeout
let overlayTimer     = null; // delayed round-end / game-over overlay
let shownOverlayPhase = null; // which phase the overlay was already shown for

// ── Boot ──────────────────────────────────────────────────────────────────────
if (!roomID) {
  window.location.href = '/';
}

window.addEventListener('DOMContentLoaded', () => {
  el('hdr-room-id').textContent = roomID;
  el('share-url').value = window.location.href;

  if (!playerName) {
    show('name-modal');
    el('name-input').focus();
  } else {
    connect();
  }
});

// ── Name entry ─────────────────────────────────────────────────────────────────
function submitName() {
  const name = el('name-input').value.trim();
  if (!name) { el('name-input').focus(); return; }
  playerName = name;
  localStorage.setItem('flip7_name', playerName);
  hide('name-modal');
  connect();
}

// ── WebSocket ─────────────────────────────────────────────────────────────────
function connect() {
  // Silently tear down any existing socket so its onclose won't schedule
  // another reconnect on top of this one.
  if (ws !== null) {
    ws.onopen = ws.onmessage = ws.onerror = ws.onclose = null;
    ws.close();
    ws = null;
  }
  if (reconnectTimer !== null) {
    clearTimeout(reconnectTimer);
    reconnectTimer = null;
  }

  const proto = location.protocol === 'https:' ? 'wss' : 'ws';
  ws = new WebSocket(`${proto}://${location.host}/ws/${roomID}`);

  ws.onopen = () => {
    reconnectDelay = 1000;
    setConnStatus('ok', 'Connected');
    ws.send(JSON.stringify({
      action:    'join',
      name:      playerName,
      sessionID: sessionID,
    }));
  };

  ws.onmessage = (e) => {
    try { handleMessage(JSON.parse(e.data)); }
    catch (_) {}
  };

  ws.onclose = () => {
    setConnStatus('err', 'Disconnected — reconnecting…');
    reconnectTimer = setTimeout(() => { reconnectTimer = null; connect(); }, reconnectDelay);
    reconnectDelay = Math.min(reconnectDelay * 2, 30000);
  };

  ws.onerror = () => setConnStatus('warn', 'Connection error');
}

// Reconnect immediately when the tab comes back to the foreground (mobile
// browsers pause JS when backgrounded, so the scheduled timer may never fire).
document.addEventListener('visibilitychange', () => {
  if (!document.hidden && (!ws || ws.readyState !== WebSocket.OPEN)) {
    reconnectDelay = 1000;
    connect();
  }
});

// Reconnect immediately when the device regains network connectivity.
window.addEventListener('online', () => {
  reconnectDelay = 1000;
  connect();
});

function sendAction(action) {
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify({ action }));
  } else {
    showToast('Not connected — reconnecting…');
  }
}

// ── Message dispatch ──────────────────────────────────────────────────────────
function handleMessage(msg) {
  switch (msg.type) {
    case 'joined':
      sessionID  = msg.sessionID;
      playerID   = msg.playerID;
      isHost     = msg.isHost;
      localStorage.setItem('flip7_session_' + roomID, sessionID);
      break;

    case 'state':
      gameState = msg.game;
      render();
      break;

    case 'error':
      showToast(msg.message);
      break;
  }
}

// ── Render ────────────────────────────────────────────────────────────────────
function render() {
  if (!gameState) return;

  // Detect new cards before rendering (for animation)
  const newCardOwners = [];
  gameState.players.forEach(p => {
    const prev = prevPlayers.find(pp => pp.id === p.id);
    const prevCount = prev ? prev.cards.length : 0;
    if (p.cards.length > prevCount) {
      newCardOwners.push({ id: p.id, count: p.cards.length - prevCount });
    }
  });

  // Header stats
  el('hdr-round').textContent = gameState.roundNumber || '—';
  el('hdr-deck').textContent  = gameState.deckSize !== undefined ? gameState.deckSize : '—';

  // Deck pile count
  const deckCountEl = el('deck-pile-count');
  if (deckCountEl) deckCountEl.textContent = (gameState.deckSize || 0) + ' cards';

  // Message bar
  el('message-bar').textContent = gameState.message || '';

  // Event log
  updateEventLog(gameState.events);

  // Players grid (always rendered)
  renderPlayersGrid();

  // Trigger animations for players who got new cards
  newCardOwners.forEach(({ id, count }) => {
    for (let i = 0; i < count; i++) {
      setTimeout(() => flyCardFromDeck(id), i * 120);
    }
    // Animate the last card(s) in the player's panel
    setTimeout(() => {
      const panel = document.querySelector(`[data-player-id="${id}"]`);
      if (!panel) return;
      const cards = panel.querySelectorAll('.card:not(.card-bust-marker)');
      // Animate newly added cards
      const total = gameState.players.find(p => p.id === id)?.cards.length || 0;
      const prev = prevPlayers.find(pp => pp.id === id);
      const prevCount = prev ? prev.cards.length : 0;
      const newCount = total - prevCount;
      for (let j = Math.max(0, cards.length - newCount); j < cards.length; j++) {
        const c = cards[j];
        c.classList.add('card-new');
        c.addEventListener('animationend', () => c.classList.remove('card-new'), { once: true });
      }
    }, count * 120 + 20);
  });

  // Save state for next diff
  prevPlayers = gameState.players.map(p => ({ id: p.id, cards: [...p.cards] }));

  // Phase-specific UI
  hideAllOverlays();
  hide('btn-draw'); hide('btn-stop');
  hide('your-turn-label'); hide('countdown');
  hide('targeting-overlay'); hide('targeting-waiting');

  switch (gameState.phase) {
    case 'lobby':   renderLobby();   break;
    case 'playing': {
      // If we were waiting to show an overlay, cancel it — round resumed
      if (overlayTimer) { clearTimeout(overlayTimer); overlayTimer = null; }
      shownOverlayPhase = null;
      renderPlaying();
      break;
    }
    case 'round_end':
    case 'game_over': {
      const phase = gameState.phase;
      const render = phase === 'round_end' ? renderRoundEnd : renderGameOver;
      if (shownOverlayPhase === phase) {
        // Already shown — just refresh content (e.g. countdown tick)
        render();
      } else {
        // First time entering this phase — delay 1 s so the final action is visible
        if (overlayTimer) clearTimeout(overlayTimer);
        overlayTimer = setTimeout(() => {
          overlayTimer = null;
          shownOverlayPhase = phase;
          render();
        }, 1000);
      }
      break;
    }
  }
}

// ── Deck animation ────────────────────────────────────────────────────────────
function flyCardFromDeck(targetPlayerID) {
  const deckEl = el('deck-pile');
  const panel  = document.querySelector(`[data-player-id="${targetPlayerID}"]`);
  if (!deckEl || !panel) return;

  const deckRect  = deckEl.getBoundingClientRect();
  const panelRect = panel.getBoundingClientRect();

  const card = document.createElement('div');
  card.className = 'card card-flying';
  card.style.cssText = `
    position: fixed;
    width: 38px;
    height: 52px;
    left: ${deckRect.left + deckRect.width / 2 - 19}px;
    top:  ${deckRect.top  + deckRect.height / 2 - 26}px;
    z-index: 1000;
    pointer-events: none;
    border-radius: 5px;
  `;
  document.body.appendChild(card);

  const endX = panelRect.left + panelRect.width  / 2 - 19;
  const endY = panelRect.top  + panelRect.height / 2 - 26;

  requestAnimationFrame(() => requestAnimationFrame(() => {
    card.style.left    = `${endX}px`;
    card.style.top     = `${endY}px`;
    card.style.opacity = '0';
    setTimeout(() => card.remove(), 420);
  }));
}

// ── Lobby ─────────────────────────────────────────────────────────────────────
function renderLobby() {
  show('lobby-overlay');
  el('share-url').value = window.location.href;

  const list = el('lobby-player-list');
  list.innerHTML = gameState.players.map(p => `
    <li>
      <span>${esc(p.name)}${p.id === playerID ? ' <em>(you)</em>' : ''}</span>
      ${p.isHost ? '<span class="host-tag">Host</span>' : ''}
    </li>
  `).join('');

  const hint = el('lobby-hint');
  if (isHost) {
    if (gameState.players.length < 2) {
      hide('btn-start');
      hint.textContent = 'Waiting for at least 1 more player…';
    } else {
      show('btn-start');
      hint.textContent = '';
    }
  } else {
    hide('btn-start');
    hint.textContent = 'Waiting for the host to start the game…';
  }
}

// ── Playing ───────────────────────────────────────────────────────────────────
function renderPlaying() {
  const cpIdx  = gameState.currentPlayerIndex;
  const cp     = cpIdx >= 0 ? gameState.players[cpIdx] : null;
  const myTurn = cp && cp.id === playerID;
  const pa     = gameState.pendingAction;

  if (pa) {
    if (pa.drawerID === playerID) {
      const labelMap = { freeze: 'Freeze', flip3: 'Flip 3', second_chance: '2nd Chance' };
      const cardLabel = labelMap[pa.card.type] || pa.card.name;
      el('targeting-card-name').textContent = `You drew: ${cardLabel}`;
      el('targeting-buttons').innerHTML = (pa.validTargetIDs || []).map(tid => {
        const tp = gameState.players.find(p => p.id === tid);
        const label = tp ? esc(tp.name) + (tid === playerID ? ' (you)' : '') : tid;
        return `<button class="btn-target" onclick="sendTarget('${tid}')">${label}</button>`;
      }).join('');
      show('targeting-overlay');
    } else {
      const drawer = gameState.players.find(p => p.id === pa.drawerID);
      el('targeting-waiting').textContent =
        `Waiting for ${drawer ? esc(drawer.name) : 'a player'} to choose a target…`;
      show('targeting-waiting');
    }
    return;
  }

  if (myTurn) {
    show('your-turn-label');
    show('btn-draw');
    show('btn-stop');
  }
}

function sendTarget(targetID) {
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify({ action: 'target', targetID }));
  } else {
    showToast('Not connected — reconnecting…');
  }
}

// ── Round end ─────────────────────────────────────────────────────────────────
function renderRoundEnd() {
  show('round-end-overlay');
  el('round-end-title').textContent = `Round ${gameState.roundNumber} Over`;

  const tbody = el('round-score-body');
  tbody.innerHTML = gameState.players.map(p => `
    <tr>
      <td>${esc(p.name)}${p.id === playerID ? ' (you)' : ''}</td>
      <td style="color:${p.status === 'busted' ? 'var(--red)' : 'var(--gold-light)'}">
        ${p.status === 'busted' ? 'BUSTED' : '+' + p.roundScore}
      </td>
      <td>${p.totalScore}</td>
    </tr>
  `).join('');

  if (gameState.nextRoundIn > 0) {
    el('round-end-countdown').textContent =
      `Next round in ${gameState.nextRoundIn}s…`;
  } else {
    el('round-end-countdown').textContent = 'Starting next round…';
  }
}

// ── Game over ─────────────────────────────────────────────────────────────────
function renderGameOver() {
  show('game-over-overlay');

  const winnerIDs = gameState.winnerIDs || [];
  const winners = gameState.players.filter(p => winnerIDs.includes(p.id));
  if (winners.length === 1) {
    el('winner-announcement').textContent =
      `${esc(winners[0].name)} wins with ${winners[0].totalScore} points!`;
  } else if (winners.length > 1) {
    el('winner-announcement').textContent =
      `Tie! ${winners.map(w => esc(w.name)).join(' & ')} with ${winners[0].totalScore} points!`;
  } else {
    el('winner-announcement').textContent = 'Game over!';
  }

  const tbody = el('final-score-body');
  const sorted = [...gameState.players].sort((a, b) => b.totalScore - a.totalScore);
  tbody.innerHTML = sorted.map(p => `
    <tr class="${winnerIDs.includes(p.id) ? 'won-row' : ''}">
      <td>${esc(p.name)}${p.id === playerID ? ' (you)' : ''}</td>
      <td>${p.totalScore}</td>
    </tr>
  `).join('');

  if (isHost) show('btn-restart');
  else hide('btn-restart');
}

// ── Players grid ──────────────────────────────────────────────────────────────
function renderPlayersGrid() {
  if (!gameState) return;
  const grid = el('players-grid');
  grid.innerHTML = gameState.players.map((p, i) => renderPlayerPanel(p, i)).join('');
}

function renderPlayerPanel(p, i) {
  const isCurrent = gameState.phase === 'playing' && i === gameState.currentPlayerIndex;
  const isYou     = p.id === playerID;

  const statusLabel = {
    active:   'Active',
    stopped:  'Stopped',
    busted:   'Busted',
    frozen:   'Frozen',
    inactive: 'Inactive',
  }[p.status] || p.status;

  const cards = (p.cards || []).map((c, ci) => {
    const isBustCard = p.status === 'busted' && ci === p.cards.length - 1;
    const numClass = c.type === 'number' ? ` card-n-${c.value}` : '';
    return `<div class="card card-${c.type}${numClass}${isBustCard ? ' card-bust-marker' : ''}" title="${esc(c.name)}">${esc(c.name)}</div>`;
  }).join('');

  const scIcon = p.hasSecondChance
    ? '<span class="second-chance-indicator">2nd Chance</span>'
    : '';

  const disconnected = !p.connected && gameState.phase !== 'lobby'
    ? '<div class="player-disconnected">Disconnected</div>'
    : '';

  return `
    <div class="player-panel ${p.status} ${isCurrent ? 'current-turn' : ''}" data-player-id="${p.id}">
      <div class="player-header">
        <span class="player-name">${esc(p.name)}</span>
        ${isYou  ? '<span class="player-you-badge">YOU</span>'  : ''}
        ${p.isHost ? '<span class="player-host-badge">Host</span>' : ''}
        <span class="status-badge status-${p.status}">${statusLabel}</span>
        ${scIcon}
      </div>
      <div class="player-scores">
        Total: <strong>${p.totalScore}</strong>
        &nbsp;|&nbsp;
        Round: <strong>${p.roundScore}</strong>
      </div>
      ${disconnected}
      <div class="player-cards">${cards || '<span style="color:var(--text-dim);font-size:0.8rem">No cards</span>'}</div>
    </div>
  `;
}

// ── Event log ─────────────────────────────────────────────────────────────────
let eventLogCollapsed = true;

function updateEventLog(events) {
  if (!events || events.length === 0) return;
  const logEl  = el('event-log');
  const body   = el('event-log-body');
  if (!logEl || !body) return;

  // Show the panel once there are events
  logEl.classList.remove('hidden');

  // Apply collapsed state on first show
  const body2 = el('event-log-body');
  const toggle2 = el('event-log-toggle');
  if (body2 && body2.dataset.len === undefined) {
    body2.style.display = eventLogCollapsed ? 'none' : '';
    if (toggle2) toggle2.textContent = eventLogCollapsed ? '+' : '−';
  }

  // Only re-render if content changed
  const newLen = events.length;
  if (body.dataset.len === String(newLen)) return;
  body.dataset.len = newLen;

  body.innerHTML = events.map(e => {
    let cls = 'ev';
    if (e.startsWith('──'))                                     cls += ' ev-round';
    else if (e.includes('BUSTED'))                              cls += ' ev-bust';
    else if (e.includes('FLIP 7'))                              cls += ' ev-flip7';
    else if (e.includes('stopped'))                             cls += ' ev-stop';
    else if (e.includes('Freeze') || e.includes('Flip 3') ||
             e.includes('2nd Chance') || e.includes('froze'))  cls += ' ev-action';
    else if (e.startsWith('  '))                                cls += ' ev-sub';
    return `<div class="${cls}">${esc(e.trim())}</div>`;
  }).join('');

  // Auto-scroll to bottom
  body.scrollTop = body.scrollHeight;
}

function toggleEventLog() {
  eventLogCollapsed = !eventLogCollapsed;
  const body   = el('event-log-body');
  const toggle = el('event-log-toggle');
  if (body)   body.style.display   = eventLogCollapsed ? 'none' : '';
  if (toggle) toggle.textContent   = eventLogCollapsed ? '+' : '−';
}

// ── Helpers ───────────────────────────────────────────────────────────────────
function el(id)       { return document.getElementById(id); }
function show(id)     { el(id)?.classList.remove('hidden'); }
function hide(id)     { el(id)?.classList.add('hidden'); }
function esc(str)     { return String(str).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])); }

function hideAllOverlays() {
  ['lobby-overlay','round-end-overlay','game-over-overlay'].forEach(hide);
}

function setConnStatus(level, text) {
  const s = el('conn-status');
  s.textContent = text;
  s.className = `conn-${level}`;
}

function showToast(message) {
  const toast = document.createElement('div');
  toast.textContent = message;
  toast.style.cssText = `
    position:fixed;bottom:4rem;left:50%;transform:translateX(-50%);
    background:var(--red);color:#fff;padding:0.5rem 1.2rem;border-radius:6px;
    font-size:0.9rem;z-index:300;box-shadow:0 4px 12px rgba(0,0,0,0.4);
  `;
  document.body.appendChild(toast);
  setTimeout(() => toast.remove(), 3500);
}

function copyLink() {
  navigator.clipboard.writeText(window.location.href).then(() => {
    const btn = el('btn-copy-link');
    const prev = btn.textContent;
    btn.textContent = '✓';
    setTimeout(() => { btn.textContent = prev; }, 1500);
  });
}
