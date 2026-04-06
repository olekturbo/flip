'use strict';

// ── State ─────────────────────────────────────────────────────────────────────
const roomID    = window.location.pathname.split('/').pop();
let sessionID   = localStorage.getItem('flip7_session_' + roomID) || '';
let playerName  = localStorage.getItem('flip7_name') || '';
let playerID    = '';
let isHost      = false;
let gameState        = null;
let prevPlayers      = [];   // previous player states for animation diffs
let prevMessage      = '';   // previous gameState.message for change detection
let ws               = null;
let reconnectDelay   = 1000;
let reconnectTimer   = null; // handle for the scheduled reconnect setTimeout
let overlayTimer     = null; // delayed round-end / game-over overlay
let shownOverlayPhase = null; // which phase the overlay was already shown for
let countdownInterval = null; // 100ms RAF-style ticker for next-round countdown
let roundEndedAtClient = null; // Date.now() calibrated to when round ended
const AUTO_NEXT_MS = 4000;    // must match Go AutoNextRound
let animBlockedUntil = 0;     // epoch ms — block turn controls until animations finish
let _pendingActionSoundPlayed = false; // guard: play targeting sound only once per pending action

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

  // ── 1. Diff against previous state ────────────────────────────────────────
  const prevSnapshot = prevPlayers.slice();

  // Detect a round transition: prevPlayers still holds the previous round's
  // card counts. On a new round all cards are fresh, so prevCount must be 0
  // for every player — otherwise Flip 3 stagger / bust timing breaks because
  // newCount is computed against stale old-round card counts.
  const isNewRound = prevSnapshot.length > 0 &&
    prevSnapshot[0].roundNumber !== undefined &&
    prevSnapshot[0].roundNumber !== gameState.roundNumber;

  gameState.players.forEach(p => {
    const prev      = prevSnapshot.find(pp => pp.id === p.id);
    const prevCount = isNewRound ? 0 : (prev ? prev.cards.length : 0);
    const prevStat  = isNewRound ? null : (prev ? prev.status : null);
    const newCount  = p.cards.length - prevCount;

    // Card arrival: start staggered reveal (or instant for single draws).
    // Guard with prev so reconnecting to an existing game doesn't replay animations.
    if (prev && newCount > 0 && revealProgress[p.id] === undefined) {
      startCardReveals(p, prevCount);
      if (newCount === 1) sndCardDraw();
    }

    // Multi-card arrival banner: 2+ new cards is either Flip 3 or a Thief steal
    // (steal adds stolen card + Thief card itself). Distinguish by card type.
    // Guard with prev to avoid false positives on reconnect.
    if (prev && newCount >= 2) {
      const newCards = p.cards.slice(prevCount);
      if (newCards.some(c => c.type === 'thief')) {
        const stolen = newCards.find(c => c.type === 'number');
        const stolenLabel = stolen != null ? stolen.value : '?';
        showActionBanner(`🃏 ${p.name} — stole ${stolenLabel}!`, 'rgba(109,40,217,0.95)');
        sndThief();
      } else {
        showActionBanner(`🎲 Flip 3 on ${p.name}!`, 'rgba(194,65,12,0.95)');
        sndFlip3();
      }
    }

    // Status transition animations (run after grid is in DOM, hence setTimeout)
    if (prevStat && prevStat !== p.status) {
      const pid = p.id, pname = p.name;
      if (p.status === 'frozen') {
        showActionBanner(`❄️ ${pname} was frozen!`, 'rgba(29,78,216,0.95)');
        sndFreeze();
        setTimeout(() => {
          const panel = document.querySelector(`[data-player-id="${pid}"]`);
          if (panel) {
            panel.classList.add('just-frozen');
            panel.addEventListener('animationend', () => panel.classList.remove('just-frozen'), { once: true });
          }
        }, 80);
      }
      if (p.status === 'stopped') {
        const pts = p.roundScore != null ? p.roundScore : '?';
        showActionBanner(`🏦 ${pname} stopped — ${pts} pts`, 'rgba(22,101,52,0.95)');
        sndStop();
      }
      if (p.status === 'busted') {
        // If Flip 3 stagger is in progress, delay banner/shake to coincide with
        // the bust card being revealed (last card of the stagger).
        const bustCard = p.cards[p.cards.length - 1];
        const bustDelay = revealProgress[pid] !== undefined
          ? 500 + (newCount - 1) * 950 + 80   // wait for last stagger card
          : 80;
        blockTurnFor(bustDelay + 1800); // hold next player's controls until bust is visible
        const bustVal = bustCard && bustCard.type === 'number' ? bustCard.value : null;
        const bustLabel = bustVal !== null
          ? `💥 ${pname} BUSTED — duplicate ${bustVal}!`
          : `💥 ${pname} BUSTED!`;
        setTimeout(() => {
          showActionBanner(bustLabel, 'rgba(185,28,28,0.95)');
          sndBust();
          const panel = document.querySelector(`[data-player-id="${pid}"]`);
          if (panel) {
            panel.classList.add('just-busted');
            panel.addEventListener('animationend', () => panel.classList.remove('just-busted'), { once: true });
            // Ghost card: show the duplicate that caused the bust, then fade out
            if (bustVal !== null) {
              const cardsEl = panel.querySelector('.player-cards');
              if (cardsEl) {
                const ghostBust = document.createElement('div');
                ghostBust.className = 'card ghost-bust-card';
                ghostBust.textContent = bustVal;
                ghostBust.title = `Duplicate ${bustVal} — BUSTED`;
                cardsEl.appendChild(ghostBust);
                setTimeout(() => {
                  ghostBust.classList.add('ghost-card-exit');
                  setTimeout(() => ghostBust.remove(), 480);
                }, 2500);
              }
            }
          }
        }, bustDelay);
      }
    }

    // Flip 7 bonus earned
    if (prev && p.roundBonus > 0 && !prev.roundBonus) {
      showActionBanner(`🎉 ${p.name} — FLIP 7! +${p.roundBonus} bonus!`, 'rgba(120,53,15,0.97)');
      sndFlip7();
    }

    // Second Chance consumed: hasSecondChance flipped true→false while player
    // was already active and within the same round (guards against false positive
    // on round transition where ResetForRound zeroes SC while prevPlayers still
    // holds the old value from the previous round).
    if (prev && prev.hasSecondChance && !p.hasSecondChance &&
        prevStat === 'active' && p.status === 'active' &&
        prev.roundNumber === gameState.roundNumber) {
      const pid = p.id, pname = p.name;
      // Parse the duplicate value from the message (e.g. "drew 9 (duplicate!)")
      const scMatch = gameState.message.match(/(?:drew|dealt) (\d+)/);
      const savedVal = scMatch ? scMatch[1] : '?';

      showActionBanner(`🛡️ ${pname} — 2nd Chance saved from ${savedVal}!`, 'rgba(5,120,80,0.95)');
      sndSecondChance();

      // Inject ghost cards into the player's hand so the event is clear visually.
      setTimeout(() => {
        const panel = document.querySelector(`[data-player-id="${pid}"]`);
        if (!panel) return;
        const cardsEl = panel.querySelector('.player-cards');
        if (!cardsEl) return;

        // Ghost: the would-be bust card (red, crossed out)
        const ghostBust = document.createElement('div');
        ghostBust.className = 'card ghost-bust-card';
        ghostBust.textContent = savedVal;
        ghostBust.title = `Would-be duplicate ${savedVal}`;
        cardsEl.appendChild(ghostBust);

        // Ghost: the consumed SC card (green shield)
        const ghostSC = document.createElement('div');
        ghostSC.className = 'card ghost-sc-card';
        ghostSC.textContent = '🛡️';
        ghostSC.title = '2nd Chance used';
        cardsEl.appendChild(ghostSC);

        // Panel glow
        panel.classList.add('just-second-chance');
        panel.addEventListener('animationend', () => panel.classList.remove('just-second-chance'), { once: true });

        // Fade both ghost cards out after 2.5s
        setTimeout(() => {
          ghostBust.classList.add('ghost-card-exit');
          ghostSC.classList.add('ghost-card-exit');
          setTimeout(() => { ghostBust.remove(); ghostSC.remove(); }, 480);
        }, 2500);
      }, 80);
    }
  });

  // ── 2. Static UI updates ───────────────────────────────────────────────────
  el('hdr-round').textContent = gameState.roundNumber || '—';
  el('hdr-deck').textContent  = gameState.deckSize !== undefined ? gameState.deckSize : '—';
  const deckCountEl = el('deck-pile-count');
  if (deckCountEl) deckCountEl.textContent = (gameState.deckSize || 0) + ' cards';

  el('message-bar').textContent = gameState.message || '';
  updateEventLog(gameState.events);

  // ── 3. Render players (respects revealProgress for card visibility) ────────
  renderPlayersGrid();

  // ── 4. card-new animation for single-draw cards ────────────────────────────
  // (Multi-card stagger is handled inside startCardReveals)
  gameState.players.forEach(p => {
    const prev      = prevSnapshot.find(pp => pp.id === p.id);
    const prevCount = isNewRound ? 0 : (prev ? prev.cards.length : 0);
    if (p.cards.length - prevCount === 1 && revealProgress[p.id] === undefined) {
      setTimeout(() => {
        const panel = document.querySelector(`[data-player-id="${p.id}"]`);
        if (!panel) return;
        const cards = panel.querySelectorAll('.card:not(.card-bust-marker)');
        if (cards.length > 0) {
          const last = cards[cards.length - 1];
          last.classList.add('card-new');
          last.addEventListener('animationend', () => last.classList.remove('card-new'), { once: true });
        }
      }, 65);
    }
  });

  // ── 5. Thief discarded with no effect — ghost card animation ─────────────
  const curMsg = gameState.message || '';
  if (curMsg !== prevMessage) {
    // Normal discard: "X drew/dealt/used Thief … discarded."
    // Matches: "X drew Thief — no card to steal, discarded."
    //          "X dealt Thief — no valid target, discarded."
    //          "X used Thief on Y — nothing to steal, discarded."
    const thiefDiscardMatch = curMsg.match(/^(.+?) (?:drew|dealt|used) Thief(?: on .+?)? — (?:no card to steal|no valid target|nothing to steal), discarded/);

    // Deferred discard (Thief drawn during Flip 3, auto-resolved with no target):
    // "Thief (deferred from Flip 3) — no valid target to steal from, discarded."
    // "Thief (deferred) — no valid target to steal from, discarded."
    const deferredThiefDiscard = !thiefDiscardMatch &&
      /^Thief \(deferred/.test(curMsg) && curMsg.includes('discarded');

    if (thiefDiscardMatch || deferredThiefDiscard) {
      // For normal discards the player is named; for deferred ones identify
      // the Flip 3 target by finding whoever has a flip3 card in their hand.
      let targetPlayer;
      if (thiefDiscardMatch) {
        const drawerName = thiefDiscardMatch[1];
        targetPlayer = gameState.players.find(p => p.name === drawerName);
      } else {
        targetPlayer = gameState.players.find(p =>
          p.cards && p.cards.some(c => c.type === 'flip3'));
      }

      if (targetPlayer) {
        showActionBanner(`🃏 ${targetPlayer.name} — Thief discarded (nothing to steal)`, 'rgba(109,40,217,0.95)');
        setTimeout(() => {
          const panel = document.querySelector(`[data-player-id="${targetPlayer.id}"]`);
          if (!panel) return;
          const cardsEl = panel.querySelector('.player-cards');
          if (!cardsEl) return;
          const ghostThief = document.createElement('div');
          ghostThief.className = 'card ghost-thief-card';
          ghostThief.textContent = '🃏';
          ghostThief.title = 'Thief — nothing to steal, discarded';
          cardsEl.appendChild(ghostThief);
          setTimeout(() => {
            ghostThief.classList.add('ghost-card-exit');
            setTimeout(() => ghostThief.remove(), 480);
          }, 2500);
        }, 80);
      }
    }
  }

  // ── 6. Persist snapshot (includes status for next diff) ───────────────────
  prevMessage = curMsg;
  prevPlayers = gameState.players.map(p => ({ id: p.id, cards: [...p.cards], status: p.status, hasSecondChance: p.hasSecondChance, roundBonus: p.roundBonus || 0, roundNumber: gameState.roundNumber }));

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

      const labelMap = { freeze: 'Freeze', flip3: 'Flip 3', second_chance: '2nd Chance', thief: 'Thief' };
      const cardLabel = labelMap[pa.card.type] || pa.card.name;
      if (!_pendingActionSoundPlayed) {
        _pendingActionSoundPlayed = true;
        const soundMap = { freeze: sndFreeze, flip3: sndFlip3, second_chance: sndSecondChance, thief: sndThief };
        (soundMap[pa.card.type] || sndActionCard)();
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
      } else {
        // Stage 1 (all action cards): pick a player
        const prompt = pa.card.type === 'thief' ? 'Choose a player to steal from:' : 'Choose a target:';
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
      const waitLabelMap = { freeze: 'Freeze', flip3: 'Flip 3', second_chance: '2nd Chance', thief: 'Thief' };
      const waitCardLabel = waitLabelMap[pa.card.type] || pa.card.type;
      el('targeting-waiting').textContent =
        `Waiting for ${drawer ? esc(drawer.name) : 'a player'} to play ${waitCardLabel}…`;
      show('targeting-waiting');
    }
    return;
  }

  _pendingActionSoundPlayed = false; // no active pending action — reset for next draw

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
