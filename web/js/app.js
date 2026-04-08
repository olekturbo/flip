'use strict';

// ── State ─────────────────────────────────────────────────────────────────────
const roomID    = window.location.pathname.split('/').pop();
let sessionID   = localStorage.getItem('flip7_session_' + roomID) || '';
let playerName  = localStorage.getItem('flip7_name') || '';
let playerID    = '';
let isHost      = false;
let gameState        = null;
let prevPlayers      = [];   // previous player states for animation diffs
let prevEventSeq     = -1;   // last handled lastEvent.seq
let ws               = null;
let reconnectDelay   = 1000;
let reconnectTimer   = null; // handle for the scheduled reconnect setTimeout
let overlayTimer     = null; // delayed round-end / game-over overlay
let shownOverlayPhase = null; // which phase the overlay was already shown for
let countdownInterval = null; // 100ms RAF-style ticker for next-round countdown
let roundEndedAtClient = null; // Date.now() calibrated to when round ended
const AUTO_NEXT_MS = 4000;    // must match Go AutoNextRound
let animBlockedUntil = 0;     // epoch ms — block turn controls until animations finish
let _lastPendingKey = null; // guard: play targeting-overlay sound only once per pending action

// Per-player card reveal state (for Flip 3 stagger)
const revealProgress = {}; // playerId → currently-visible card count
const revealTimers   = {}; // playerId → array of setTimeout handles

// ── Boot ──────────────────────────────────────────────────────────────────────
if (!roomID) {
  window.location.href = '/';
}

window.addEventListener('DOMContentLoaded', () => {
  el('hdr-room-id').textContent = roomID;
  el('share-url').value = window.location.href;
  initMuteButton();

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

// Keyboard shortcuts: (d) Draw, (s) Stop — active only when buttons are visible.
document.addEventListener('keydown', (e) => {
  if (e.repeat) return;
  const tag = document.activeElement?.tagName;
  if (tag === 'INPUT' || tag === 'TEXTAREA') return;
  const drawBtn = el('btn-draw');
  const stopBtn = el('btn-stop');
  if (e.key === 'd' && drawBtn && !drawBtn.classList.contains('hidden')) {
    sendAction('draw');
  } else if (e.key === 's' && stopBtn && !stopBtn.classList.contains('hidden')) {
    sendAction('stop');
  }
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

// Banner colour palette (one place to update colours).
const BANNER = {
  bust:    'rgba(185,28,28,0.95)',
  freeze:  'rgba(29,78,216,0.95)',
  flip3:   'rgba(194,65,12,0.95)',
  thief:   'rgba(109,40,217,0.95)',
  swap:    'rgba(3,105,161,0.95)',
  stop:    'rgba(22,101,52,0.95)',
  flip7:   'rgba(120,53,15,0.97)',
  sc:      'rgba(5,120,80,0.95)',
};

// Spawn a ghost card inside a player's hand panel, then auto-fade it out.
function spawnGhostCard(playerId, cssClass, label, title, delayMs = 0) {
  setTimeout(() => {
    const panel   = document.querySelector(`[data-player-id="${playerId}"]`);
    const cardsEl = panel && panel.querySelector('.player-cards');
    if (!cardsEl) return;
    const ghost = document.createElement('div');
    ghost.className   = `card ${cssClass}`;
    ghost.textContent = String(label);
    ghost.title       = title;
    cardsEl.appendChild(ghost);
    setTimeout(() => {
      ghost.classList.add('ghost-card-exit');
      setTimeout(() => ghost.remove(), 480);
    }, 2500);
  }, delayMs);
}

// Handle a structured game event emitted by the server.
// Single source of truth for all banners + sounds — no string parsing.
function handleGameEvent(evt) {
  const byID = id => gameState.players.find(p => p.id === id);

  switch (evt.type) {

    case 'bust': {
      const p = byID(evt.playerID);
      showActionBanner(
        p ? `💥 ${p.name} BUSTED — duplicate ${evt.cardValue}!` : '💥 BUSTED!',
        BANNER.bust
      );
      sndBust();
      break;
    }

    case 'second_chance': {
      const p = byID(evt.playerID);
      if (!p) break;
      showActionBanner(`🛡️ ${p.name} — 2nd Chance saved from ${evt.cardValue}!`, BANNER.sc);
      sndSecondChance();
      setTimeout(() => {
        const panel = document.querySelector(`[data-player-id="${p.id}"]`);
        if (panel) {
          panel.classList.add('just-second-chance');
          panel.addEventListener('animationend', () => panel.classList.remove('just-second-chance'), { once: true });
        }
      }, 80);
      spawnGhostCard(p.id, 'ghost-bust-card', evt.cardValue, `Would-be duplicate ${evt.cardValue}`, 80);
      spawnGhostCard(p.id, 'ghost-sc-card', '🛡️', '2nd Chance used', 80);
      break;
    }

    case 'freeze': {
      const p = byID(evt.playerID);
      if (p) { showActionBanner(`❄️ ${p.name} was frozen!`, BANNER.freeze); sndFreeze(); }
      break;
    }

    case 'flip3': {
      const p = byID(evt.playerID);
      if (p) { showActionBanner(`🎲 Flip 3 on ${p.name}!`, BANNER.flip3); sndFlip3(); }
      break;
    }

    case 'thief_steal': {
      const p = byID(evt.playerID);
      if (p) { showActionBanner(`🃏 ${p.name} stole ${evt.cardName}!`, BANNER.thief); sndThief(); }
      break;
    }

    case 'thief_discarded': {
      const p = byID(evt.playerID);
      if (p) {
        showActionBanner(`🃏 ${p.name} — Thief discarded (nothing to steal)`, BANNER.thief);
        spawnGhostCard(p.id, 'ghost-thief-card', '🃏', 'Thief — nothing to steal, discarded', 80);
      }
      break;
    }

    case 'swap_success': {
      const p1 = byID(evt.playerID);
      const p2 = byID(evt.playerID2);
      if (p1) {
        showActionBanner(
          `🔀 ${p1.name} swapped ${evt.cardName} ↔ ${p2 ? p2.name : '?'}'s ${evt.cardName2}!`,
          BANNER.swap
        );
        sndSwap();
        [p1, p2].forEach(p => {
          if (!p) return;
          const panel = document.querySelector(`[data-player-id="${p.id}"]`);
          if (panel) {
            panel.classList.add('just-swap');
            panel.addEventListener('animationend', () => panel.classList.remove('just-swap'), { once: true });
          }
        });
      }
      break;
    }

    case 'swap_discarded': {
      const p = byID(evt.playerID);
      if (p) {
        showActionBanner(`🔀 ${p.name} — Swap discarded (no valid target)`, BANNER.swap);
        spawnGhostCard(p.id, 'ghost-swap-card', '🔀', 'Swap — discarded', 80);
      }
      break;
    }

    case 'stop': {
      const p = byID(evt.playerID);
      if (p) { showActionBanner(`🏦 ${p.name} stopped — ${evt.score} pts`, BANNER.stop); sndStop(); }
      break;
    }

    case 'stop_forced': {
      const p = byID(evt.playerID);
      if (p) { showActionBanner(`🏦 ${p.name} stopped (no cards left)`, BANNER.stop); sndStop(); }
      break;
    }

    case 'flip7': {
      const p = byID(evt.playerID);
      if (p) { showActionBanner(`🎉 ${p.name} — FLIP 7! +15 bonus!`, BANNER.flip7); sndFlip7(); }
      break;
    }
  }
}

function render() {
  if (!gameState) return;

  const prevSnapshot = prevPlayers.slice();

  // Detect round transition — prevPlayers still holds the previous round's card
  // counts; on a new round treat every player's prevCount as 0.
  const isNewRound = prevSnapshot.length > 0 &&
    prevSnapshot[0].roundNumber !== undefined &&
    prevSnapshot[0].roundNumber !== gameState.roundNumber;

  // ── 1. Structured-event banners + sounds ──────────────────────────────────
  // Single code path for all game events — no string parsing.
  const evt = gameState.lastEvent;
  if (evt && evt.seq !== prevEventSeq) {
    prevEventSeq = evt.seq;
    handleGameEvent(evt);
  }

  // ── 2. Card-arrival animations ─────────────────────────────────────────────
  gameState.players.forEach(p => {
    const prev      = prevSnapshot.find(pp => pp.id === p.id);
    const prevCount = isNewRound ? 0 : (prev ? prev.cards.length : 0);
    const newCount  = p.cards.length - prevCount;
    if (prev && newCount > 0 && revealProgress[p.id] === undefined) {
      startCardReveals(p, prevCount);
      if (newCount === 1) sndCardDraw();
    }
  });

  // ── 3. State-diff events ───────────────────────────────────────────────────
  gameState.players.forEach(p => {
    const prev     = prevSnapshot.find(pp => pp.id === p.id);
    const prevStat = isNewRound ? null : (prev ? prev.status : null);

    // Status visual effects — CSS classes and ghost cards; banners + sounds come from handleGameEvent.
    if (prevStat && prevStat !== p.status) {
      const pid = p.id;
      if (p.status === 'frozen') {
        setTimeout(() => {
          const panel = document.querySelector(`[data-player-id="${pid}"]`);
          if (panel) {
            panel.classList.add('just-frozen');
            panel.addEventListener('animationend', () => panel.classList.remove('just-frozen'), { once: true });
          }
        }, 80);
      }
      if (p.status === 'busted') {
        blockTurnFor(1800);
        setTimeout(() => {
          const panel = document.querySelector(`[data-player-id="${pid}"]`);
          if (!panel) return;
          panel.classList.add('just-busted');
          panel.addEventListener('animationend', () => panel.classList.remove('just-busted'), { once: true });
          // Ghost: the duplicate card that caused the bust
          const bustCard = p.cards[p.cards.length - 1];
          if (bustCard && bustCard.type === 'number') {
            spawnGhostCard(pid, 'ghost-bust-card', bustCard.value, `Duplicate ${bustCard.value} — BUSTED`);
          }
        }, 80);
      }
    }
  });

  // ── 4. card-new highlight on single draws ──────────────────────────────────
  gameState.players.forEach(p => {
    const prev      = prevSnapshot.find(pp => pp.id === p.id);
    const prevCount = isNewRound ? 0 : (prev ? prev.cards.length : 0);
    if (p.cards.length - prevCount === 1 && revealProgress[p.id] === undefined) {
      setTimeout(() => {
        const panel = document.querySelector(`[data-player-id="${p.id}"]`);
        if (!panel) return;
        const cards = panel.querySelectorAll('.card:not(.card-bust-marker)');
        const last  = cards[cards.length - 1];
        if (last) {
          last.classList.add('card-new');
          last.addEventListener('animationend', () => last.classList.remove('card-new'), { once: true });
        }
      }, 65);
    }
  });

  // ── 5. Snapshot + static UI ────────────────────────────────────────────────
  prevPlayers = gameState.players.map(p => ({
    id: p.id, cards: [...p.cards], status: p.status,
    hasSecondChance: p.hasSecondChance, roundBonus: p.roundBonus || 0,
    roundNumber: gameState.roundNumber,
  }));

  el('hdr-round').textContent = gameState.roundNumber || '—';
  el('hdr-deck').textContent  = gameState.deckSize !== undefined ? gameState.deckSize : '—';
  const deckCountEl = el('deck-pile-count');
  if (deckCountEl) deckCountEl.textContent = (gameState.deckSize || 0) + ' cards';
  el('message-bar').textContent = gameState.message || '';
  updateEventLog(gameState.events);

  // ── 6. Render players ──────────────────────────────────────────────────────
  renderPlayersGrid();

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
      roundEndedAtClient = null;
      clearCountdown();
      renderPlaying();
      break;
    }
    case 'round_end':
    case 'game_over': {
      const phase = gameState.phase;
      const renderFn = phase === 'round_end' ? renderRoundEnd : renderGameOver;

      // Calibrate client-side round-end timestamp once per round transition.
      // Use server's nextRoundIn to back-calculate when the round actually ended.
      if (phase === 'round_end' && roundEndedAtClient === null) {
        const elapsed = (AUTO_NEXT_MS / 1000 - (gameState.nextRoundIn || 0)) * 1000;
        roundEndedAtClient = Date.now() - Math.max(0, elapsed);
        startCountdownTicker();
      }

      if (shownOverlayPhase === phase) {
        // Already shown — overlay is visible, countdown ticks independently
      } else {
        if (overlayTimer) clearTimeout(overlayTimer);
        // If a Flip 3 stagger is in progress, wait for it to finish so the
        // bust card is actually visible before the overlay covers everything.
        const revealMs = Object.keys(revealProgress).reduce((max, pid) => {
          const p = gameState.players.find(pl => pl.id === pid);
          if (!p) return max;
          const remaining = p.cards.length - (revealProgress[pid] || 0);
          return Math.max(max, remaining * 950);
        }, 0);
        const delay = Math.max(900, revealMs + 600);
        overlayTimer = setTimeout(() => {
          overlayTimer = null;
          shownOverlayPhase = phase;
          clearAllRevealTimers(); // clean up after overlay is about to show
          renderFn();
        }, delay);
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
    setTimeout(() => card.remove(), 900);
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
      // If a Flip 3 stagger is still animating for the drawer's cards, wait until
      // all cards are visually revealed before showing the targeting overlay.
      if (revealProgress[pa.drawerID] !== undefined) {
        hide('targeting-overlay');
        hide('targeting-waiting');
        return;
      }

      const labelMap = { freeze: 'Freeze', flip3: 'Flip 3', second_chance: '2nd Chance', thief: 'Thief', swap: 'Swap' };
      const cardLabel = labelMap[pa.card.type] || pa.card.name;
      const paKey = pa.drawerID + ':' + pa.card.type;
      if (_lastPendingKey !== paKey) {
        _lastPendingKey = paKey;
        sndActionCard();
      }
      el('targeting-card-name').textContent = `You drew: ${cardLabel}`;

      if (pa.card.type === 'thief' && pa.thiefVictimID) {
        // Stage 2: victim chosen — pick which card to steal
        const victim = gameState.players.find(p => p.id === pa.thiefVictimID);
        el('targeting-prompt').textContent =
          `Steal a card from ${victim ? esc(victim.name) : '?'}:`;
        el('targeting-buttons').innerHTML = (pa.stealableCards || []).map(c =>
          `<button class="btn-target card card-n-${c.value}" onclick="sendSteal(${c.value})">${esc(c.name)}</button>`
        ).join('');
      } else if (pa.card.type === 'swap' && pa.shufflePartnerID) {
        // Stage 2: partner chosen — pick the card pair to swap
        const partner = gameState.players.find(p => p.id === pa.shufflePartnerID);
        el('targeting-prompt').textContent =
          `Choose which cards to swap with ${partner ? esc(partner.name) : '?'}:`;
        const myCards     = pa.shuffleDrawerCards  || [];
        const theirCards  = pa.shufflePartnerCards || [];
        el('targeting-buttons').innerHTML = myCards.flatMap(mc =>
          theirCards.map(tc =>
            `<button class="btn-target btn-swap-pair" onclick="sendShuffleSwap(${mc.value},${tc.value})">` +
            `<span class="card card-n-${mc.value}">${esc(mc.name)}</span>` +
            `<span class="swap-arrow">↔</span>` +
            `<span class="card card-n-${tc.value}">${esc(tc.name)}</span>` +
            `</button>`
          )
        ).join('');
      } else {
        // Stage 1 (all action cards): pick a player
        let prompt = 'Choose a target:';
        if (pa.card.type === 'thief')   prompt = 'Choose a player to steal from:';
        if (pa.card.type === 'swap') prompt = 'Choose a player to swap with:';
        el('targeting-prompt').textContent = prompt;
        el('targeting-buttons').innerHTML = (pa.validTargetIDs || []).map(tid => {
          const tp = gameState.players.find(p => p.id === tid);
          const label = tp ? esc(tp.name) + (tid === playerID ? ' (you)' : '') : tid;
          return `<button class="btn-target" onclick="sendTarget('${tid}')">${label}</button>`;
        }).join('');
      }
      show('targeting-overlay');
    } else {
      // Also delay the "waiting" notice until the drawer's cards are fully revealed.
      if (revealProgress[pa.drawerID] !== undefined) {
        hide('targeting-overlay');
        hide('targeting-waiting');
        return;
      }
      const drawer = gameState.players.find(p => p.id === pa.drawerID);
      const waitLabelMap = { freeze: 'Freeze', flip3: 'Flip 3', second_chance: '2nd Chance', thief: 'Thief', swap: 'Swap' };
      const waitCardLabel = waitLabelMap[pa.card.type] || pa.card.type;
      el('targeting-waiting').textContent =
        `Waiting for ${drawer ? esc(drawer.name) : 'a player'} to play ${waitCardLabel}…`;
      show('targeting-waiting');
    }
    return;
  }

  _lastPendingKey = null; // no active pending action — reset for next draw

  if (myTurn) {
    const remaining = animBlockedUntil - Date.now();
    if (remaining > 0) {
      setTimeout(flushTurnControls, remaining + 50);
      return;
    }
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

function sendSteal(cardValue) {
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify({ action: 'steal', cardValue }));
  } else {
    showToast('Not connected — reconnecting…');
  }
}

function sendShuffleSwap(myCardValue, theirCardValue) {
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify({ action: 'swap', cardValue: myCardValue, cardValue2: theirCardValue }));
  } else {
    showToast('Not connected — reconnecting…');
  }
}

// ── Card reveal helpers (Flip 3 stagger) ─────────────────────────────────────

function clearRevealTimers(playerId) {
  (revealTimers[playerId] || []).forEach(clearTimeout);
  delete revealTimers[playerId];
  delete revealProgress[playerId];
}

function clearAllRevealTimers() {
  Object.keys(revealProgress).forEach(clearRevealTimers);
}

// Reveal `player.cards` incrementally, starting from `prevCount`.
// Single new card → instant fly + reveal.
// Multiple new cards (Flip 3) → fly + reveal every STAGGER ms.
function startCardReveals(player, prevCount) {
  const totalNew = player.cards.length - prevCount;
  if (totalNew <= 0) return;

  clearRevealTimers(player.id);

  if (totalNew === 1) {
    // Single draw: fly animation, card appears immediately in DOM.
    flyCardFromDeck(player.id);
    return;
  }

  // Flip 3 (or similar multi-card event): stagger reveals.
  const STAGGER    = 950;  // ms between cards
  const START      = 500;  // brief pause so the banner/message can land first

  // Block next-player controls until all staggered cards are revealed.
  blockTurnFor(START + (totalNew - 1) * STAGGER + 800);

  revealProgress[player.id] = prevCount;
  revealTimers[player.id]   = [];

  for (let i = 0; i < totalNew; i++) {
    const handle = setTimeout(() => {
      if (revealProgress[player.id] === undefined) return; // cancelled (e.g. round ended)
      revealProgress[player.id] = prevCount + i + 1;

      flyCardFromDeck(player.id);
      renderPlayersGrid();   // re-render with one more card visible

      // card-new CSS animation on the revealed card
      setTimeout(() => {
        const panel = document.querySelector(`[data-player-id="${player.id}"]`);
        if (!panel) return;
        const cards = panel.querySelectorAll('.card:not(.card-bust-marker)');
        const cardEl = cards[prevCount + i];
        if (cardEl) {
          cardEl.classList.add('card-new');
          cardEl.addEventListener('animationend', () => cardEl.classList.remove('card-new'), { once: true });
        }
      }, 70);

      if (i === totalNew - 1) {
        delete revealProgress[player.id];
        // If a pending action was waiting on this stagger, now show the overlay.
        if (gameState && gameState.pendingAction) {
          setTimeout(renderPlaying, 80);
        }
      }
    }, START + i * STAGGER);

    revealTimers[player.id].push(handle);
  }
}

// ── Animation turn-blocking ───────────────────────────────────────────────────

// Prevent the current player's action controls from appearing until ongoing
// animations (Flip 3 stagger, bust shake) have finished.
function blockTurnFor(ms) {
  animBlockedUntil = Math.max(animBlockedUntil, Date.now() + ms);
}

// Called by setTimeout when a block expires — shows controls if still myTurn.
function flushTurnControls() {
  if (!gameState || gameState.phase !== 'playing') return;
  if (gameState.pendingAction) return;
  const cpIdx = gameState.currentPlayerIndex;
  const cp = cpIdx >= 0 ? gameState.players[cpIdx] : null;
  if (!cp || cp.id !== playerID) return;
  const remaining = animBlockedUntil - Date.now();
  if (remaining > 0) {
    setTimeout(flushTurnControls, remaining + 50);
    return;
  }
  show('your-turn-label');
  show('btn-draw');
  show('btn-stop');
}

// ── Action banner ─────────────────────────────────────────────────────────────

const _activeBanners = []; // live banner elements — used to stack multiple banners vertically

function showActionBanner(text, bgColor) {
  // Purge banners that have already been removed from the DOM.
  _activeBanners.splice(0, _activeBanners.length,
    ..._activeBanners.filter(b => document.body.contains(b)));

  const slot = _activeBanners.length;
  const banner = document.createElement('div');
  banner.className = 'action-banner';
  banner.style.background = bgColor;
  // Stack additional banners below the first so they don't overlap.
  if (slot > 0) banner.style.top = `calc(5rem + ${slot} * 3.2rem)`;
  banner.textContent = text;
  document.body.appendChild(banner);
  _activeBanners.push(banner);

  // Double rAF ensures the initial opacity:0 state is painted before transition.
  requestAnimationFrame(() => requestAnimationFrame(() => banner.classList.add('action-banner-show')));
  setTimeout(() => {
    banner.classList.remove('action-banner-show');
    banner.classList.add('action-banner-hide');
    setTimeout(() => {
      banner.remove();
      const idx = _activeBanners.indexOf(banner);
      if (idx !== -1) _activeBanners.splice(idx, 1);
    }, 400);
  }, 2800);
}

// ── Round end ─────────────────────────────────────────────────────────────────
function renderRoundEnd() {
  show('round-end-overlay');
  el('round-end-title').textContent = `Round ${gameState.roundNumber} Over`;
  sndRoundEnd();

  const tbody = el('round-score-body');
  tbody.innerHTML = gameState.players.map(p => {
    let scoreCell;
    if (p.status === 'busted') {
      scoreCell = `<span style="color:var(--red)">BUSTED</span>`;
    } else {
      const base  = `+${p.roundScore}`;
      const bonus = p.roundBonus
        ? `<span class="flip7-bonus-tag">+${p.roundBonus} 🎉</span>`
        : '';
      scoreCell = `<span style="color:var(--gold-light)">${base}</span>${bonus}`;
    }
    return `
    <tr>
      <td>${esc(p.name)}${p.id === playerID ? ' (you)' : ''}</td>
      <td>${scoreCell}</td>
      <td>${p.totalScore}</td>
    </tr>`;
  }).join('');

  updateCountdownDisplay();
}

function startCountdownTicker() {
  if (countdownInterval) return; // already running
  countdownInterval = setInterval(updateCountdownDisplay, 100);
}

function updateCountdownDisplay() {
  const el2 = el('round-end-countdown');
  if (!el2) return;
  if (roundEndedAtClient === null) {
    el2.textContent = '';
    return;
  }
  const remaining = AUTO_NEXT_MS - (Date.now() - roundEndedAtClient);
  const secs = Math.ceil(remaining / 1000);
  if (secs > 0) {
    el2.textContent = `Next round in ${secs}…`;
  } else {
    el2.textContent = 'Starting next round…';
  }
}

function clearCountdown() {
  if (countdownInterval) { clearInterval(countdownInterval); countdownInterval = null; }
  roundEndedAtClient = null;
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

  // During Flip 3 stagger, show only the cards revealed so far.
  const visibleCount = revealProgress[p.id] !== undefined ? revealProgress[p.id] : (p.cards || []).length;
  const visibleCards = (p.cards || []).slice(0, visibleCount);
  const cards = visibleCards.map((c, ci) => {
    const isBustCard = p.status === 'busted' && ci === visibleCards.length - 1 && visibleCount >= (p.cards || []).length;
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
        Round: <strong>${p.roundScore}</strong>${p.roundBonus ? ` <span class="flip7-bonus-tag">+${p.roundBonus} 🎉</span>` : ''}
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
             e.includes('2nd Chance') || e.includes('froze') ||
             e.includes('Thief') || e.includes('stole'))        cls += ' ev-action';
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
  if (body && !eventLogCollapsed)  body.scrollTop = body.scrollHeight;
}

// ── Helpers ───────────────────────────────────────────────────────────────────
function el(id)       { return document.getElementById(id); }
function show(id)     { el(id)?.classList.remove('hidden'); }
function hide(id)     { el(id)?.classList.add('hidden'); }
function esc(str)     { return String(str).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])); }

function hideAllOverlays() {
  ['lobby-overlay','round-end-overlay','game-over-overlay'].forEach(hide);
  clearCountdown();
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

function copyLobbyLink(btn) {
  navigator.clipboard.writeText(window.location.href).then(() => {
    const prev = btn.textContent;
    btn.textContent = '✓ Copied!';
    btn.disabled = true;
    setTimeout(() => { btn.textContent = prev; btn.disabled = false; }, 1800);
  });
}
