'use strict';

// ── Sound engine (Web Audio API synthesis) ────────────────────────────────────
// All sounds are generated procedurally — no audio files required.

let _ctx = null;
let muted = (localStorage.getItem('flip7_muted') === '1');

function audioCtx() {
  if (!_ctx) _ctx = new (window.AudioContext || window.webkitAudioContext)();
  if (_ctx.state === 'suspended') _ctx.resume();
  return _ctx;
}

function play(fn) {
  if (muted) return;
  try { fn(audioCtx()); } catch (_) {}
}

// Helpers
function osc(ctx, type, freq, start, dur, gainPeak, gainEnd = 0) {
  const o = ctx.createOscillator();
  const g = ctx.createGain();
  o.connect(g); g.connect(ctx.destination);
  o.type = type;
  o.frequency.setValueAtTime(freq, start);
  g.gain.setValueAtTime(0, start);
  g.gain.linearRampToValueAtTime(gainPeak, start + 0.01);
  g.gain.exponentialRampToValueAtTime(Math.max(gainEnd, 0.0001), start + dur);
  o.start(start);
  o.stop(start + dur + 0.01);
}

function freqRamp(ctx, type, f0, f1, start, dur, gainPeak) {
  const o = ctx.createOscillator();
  const g = ctx.createGain();
  o.connect(g); g.connect(ctx.destination);
  o.type = type;
  o.frequency.setValueAtTime(f0, start);
  o.frequency.exponentialRampToValueAtTime(f1, start + dur);
  g.gain.setValueAtTime(0, start);
  g.gain.linearRampToValueAtTime(gainPeak, start + 0.01);
  g.gain.exponentialRampToValueAtTime(0.0001, start + dur);
  o.start(start);
  o.stop(start + dur + 0.02);
}

// ── Individual sounds ─────────────────────────────────────────────────────────

// Soft card flip click
function sndCardDraw() {
  play(ctx => {
    const t = ctx.currentTime;
    freqRamp(ctx, 'sine', 1200, 600, t, 0.07, 0.18);
    // subtle noise tick via high-freq triangle
    freqRamp(ctx, 'triangle', 2400, 800, t, 0.05, 0.07);
  });
}

// Low thud + descending buzz → bust
function sndBust() {
  play(ctx => {
    const t = ctx.currentTime;
    freqRamp(ctx, 'sawtooth', 260, 60, t, 0.35, 0.28);
    freqRamp(ctx, 'square',   180, 50, t + 0.04, 0.32, 0.12);
  });
}

// Cold descending shimmer → freeze
function sndFreeze() {
  play(ctx => {
    const t = ctx.currentTime;
    freqRamp(ctx, 'triangle', 1400, 400, t,        0.45, 0.14);
    freqRamp(ctx, 'sine',     1800, 600, t + 0.05, 0.40, 0.10);
    freqRamp(ctx, 'triangle',  900, 300, t + 0.12, 0.35, 0.08);
  });
}

// Three quick ascending beeps → Flip 3
function sndFlip3() {
  play(ctx => {
    const t = ctx.currentTime;
    [440, 550, 660].forEach((freq, i) => {
      osc(ctx, 'sine', freq, t + i * 0.13, 0.10, 0.22);
    });
  });
}

// Rising "saved!" chime → Second Chance
function sndSecondChance() {
  play(ctx => {
    const t = ctx.currentTime;
    osc(ctx, 'sine', 520, t,        0.18, 0.20);
    osc(ctx, 'sine', 780, t + 0.14, 0.22, 0.24);
    osc(ctx, 'triangle', 1040, t + 0.28, 0.28, 0.14);
  });
}

// Victory arpeggio → Flip 7 bonus
function sndFlip7() {
  play(ctx => {
    const t = ctx.currentTime;
    const notes = [523, 659, 784, 1047, 1319]; // C5 E5 G5 C6 E6
    notes.forEach((freq, i) => {
      osc(ctx, 'sine', freq, t + i * 0.11, 0.20, 0.26);
      // shimmer overtone
      osc(ctx, 'triangle', freq * 2, t + i * 0.11, 0.14, 0.08);
    });
  });
}

// Soft bank click → player stops
function sndStop() {
  play(ctx => {
    const t = ctx.currentTime;
    osc(ctx, 'sine', 660, t, 0.12, 0.14);
    freqRamp(ctx, 'triangle', 880, 440, t, 0.10, 0.07);
  });
}

// Gentle bell chime → round ends
function sndRoundEnd() {
  play(ctx => {
    const t = ctx.currentTime;
    osc(ctx, 'sine',     880, t,        0.6, 0.18);
    osc(ctx, 'triangle', 660, t + 0.06, 0.5, 0.12);
    osc(ctx, 'sine',    1100, t + 0.12, 0.4, 0.07);
  });
}

// Action card drawn (generic pick-up sound)
function sndActionCard() {
  play(ctx => {
    const t = ctx.currentTime;
    freqRamp(ctx, 'sine', 500, 900, t, 0.12, 0.18);
    osc(ctx, 'triangle', 700, t + 0.08, 0.14, 0.10);
  });
}

// Thief card — sneaky descending swipe
function sndThief() {
  play(ctx => {
    const t = ctx.currentTime;
    freqRamp(ctx, 'sawtooth', 800, 300, t,        0.14, 0.20);
    freqRamp(ctx, 'triangle', 600, 200, t + 0.07, 0.10, 0.15);
  });
}

// Swap card — two-note crossing swish (cards trading places)
function sndSwap() {
  play(ctx => {
    const t = ctx.currentTime;
    freqRamp(ctx, 'sine',     400, 900, t,        0.16, 0.18); // up
    freqRamp(ctx, 'triangle', 900, 400, t + 0.06, 0.16, 0.18); // down (crossing)
    osc(ctx, 'sine', 660, t + 0.18, 0.10, 0.12); // resolution click
  });
}

// ── Mute toggle ───────────────────────────────────────────────────────────────
function toggleMute() {
  muted = !muted;
  localStorage.setItem('flip7_muted', muted ? '1' : '0');
  const btn = document.getElementById('btn-mute');
  if (btn) btn.textContent = muted ? '🔇' : '🔊';
}

function initMuteButton() {
  const btn = document.getElementById('btn-mute');
  if (btn) btn.textContent = muted ? '🔇' : '🔊';
}
