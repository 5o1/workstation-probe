// poller.js — one polling cycle per unique (api, refresh) pair. Multiple
// panels pointing at the same monitor share a single fetch and receive
// the data through a Set of subscriber callbacks. This keeps N panels on
// a page from generating N independent requests per refresh.
//
// The poller is created lazily on the first subscribe() and lives until
// the page navigates away. There is no explicit teardown because the
// subscriber is owned by a panel that lives for the page's lifetime; if
// that ever changes, subscribe() returns an unsubscribe function.

// Public state shape passed to callbacks:
//   {
//     data: <latest /metrics response or null>,
//     profile: </profile response or null>,
//     error: <metrics Error or null>,
//     profileError: <profile Error or null>,
//     elapsedMs: <int>,
//     lastSuccessAgeMs: <int|null>
//   }

// Optional panel-config knobs forwarded as query params on /metrics:
//   cfg.mode   — e.g. "peak" to ask the server for peak-over-window
//                samples (CPU/memory/GPU only). Omitted when not set.
//   cfg.window — trailing window for peak mode, e.g. "5s". Required
//                when mode=peak; the server rejects requests that
//                have mode=peak without a parseable window.

const pollers = new Map();
const STARTUP_RETRY_MS = 500;
const STARTUP_TIMEOUT_MS = 1500;

export function subscribe(cfg, callback) {
  const key = `${cfg.api}|${cfg.refresh || '5s'}|${metricsUrlQuery(cfg)}`;
  let poller = pollers.get(key);
  if (!poller) {
    poller = createPoller(cfg);
    pollers.set(key, poller);
  }
  poller.callbacks.add(callback);
  // Replay the most recent result so the subscriber doesn't have to wait
  // one full refresh cycle before painting.
  if (poller.lastData !== null || poller.lastError !== null) {
    safeCall(callback, currentState(poller));
  }
  return () => poller.callbacks.delete(callback);
}

// metricsUrlQuery builds "?mode=...&window=..." when both are present
// in the config. Returns "" otherwise so the default /metrics URL is
// untouched (the most common case).
function metricsUrlQuery(cfg) {
  if (!cfg || !cfg.mode || !cfg.window) return '';
  return `?mode=${encodeURIComponent(cfg.mode)}&window=${encodeURIComponent(cfg.window)}`;
}

function createPoller(cfg) {
  const poller = {
    cfg,
    callbacks: new Set(),
    timerId: null,
    profileTimerId: null,
    abortTimer: null,
    running: false,
    profileRunning: false,
    hasSuccess: false,
    lastSuccessAt: 0,
    lastData: null,
    lastError: null,
    lastElapsedMs: 0,
    profile: null,
    profileError: null,
  };

  const intervalMs = parseDurationMs(cfg.refresh || '5s');
  const url = `${cfg.api}/metrics${metricsUrlQuery(cfg)}`;
  const profileUrl = `${cfg.api}/profile`;

  async function loadProfile() {
    if (poller.profile || poller.profileRunning) return;
    poller.profileRunning = true;
    const ctrl = new AbortController();
    const abortTimer = setTimeout(() => ctrl.abort(), STARTUP_TIMEOUT_MS);
    try {
      const res = await fetch(profileUrl, { signal: ctrl.signal });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      poller.profile = await res.json();
      poller.profileError = null;
    } catch (e) {
      poller.profileError = e;
    } finally {
      poller.profileRunning = false;
      clearTimeout(abortTimer);
      if (poller.lastData !== null || poller.lastError !== null) {
        broadcast(poller);
      }
      if (!poller.profile) {
        poller.profileTimerId = setTimeout(loadProfile, STARTUP_RETRY_MS);
      }
    }
  }

  async function tick() {
    if (poller.running) return;
    poller.running = true;
    const start = performance.now();
    // Hard timeout on the request itself (separate from refresh interval).
    // The server can stall silently and we don't want a stuck promise to
    // block the next tick — the running flag handles that, but a hard
    // abort frees sockets faster on the browser side.
    const ctrl = new AbortController();
    const timeoutMs = poller.hasSuccess ? Math.max(intervalMs, 5000) : STARTUP_TIMEOUT_MS;
    poller.abortTimer = setTimeout(() => ctrl.abort(), timeoutMs);
    try {
      const res = await fetch(url, { signal: ctrl.signal });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = await res.json();
      poller.lastData = data;
      poller.lastError = null;
      poller.hasSuccess = true;
      poller.lastSuccessAt = Date.now();
      poller.lastElapsedMs = Math.round(performance.now() - start);
      broadcast(poller);
    } catch (e) {
      poller.lastError = e;
      poller.lastElapsedMs = Math.round(performance.now() - start);
      broadcast(poller);
    } finally {
      poller.running = false;
      if (poller.abortTimer) {
        clearTimeout(poller.abortTimer);
        poller.abortTimer = null;
      }
      scheduleNext();
    }
  }

  function scheduleNext() {
    const delayMs = poller.hasSuccess ? intervalMs : STARTUP_RETRY_MS;
    poller.timerId = setTimeout(tick, delayMs);
  }

  function broadcast(p) {
    for (const cb of p.callbacks) {
      safeCall(cb, currentState(p));
    }
  }

  loadProfile();
  // Fire-and-forget the first tick. Until the first successful response,
  // retry quickly so a page opened while the monitor is still starting does
  // not wait a full refresh interval after the first connection failure.
  tick();
  return poller;
}

function currentState(p) {
  return {
    data: p.lastData,
    profile: p.profile,
    error: p.lastError,
    profileError: p.profileError,
    elapsedMs: p.lastElapsedMs,
    lastSuccessAgeMs: p.lastSuccessAt > 0 ? Date.now() - p.lastSuccessAt : null,
  };
}

function safeCall(cb, state) {
  try {
    cb(state);
  } catch (e) {
    console.error('[workstation-probe] subscriber callback threw:', e);
  }
}

// Parse a Go-style duration suffix (ms / s / m) into milliseconds. The
// monitor's config layer uses Go durations; mirroring that grammar keeps
// the YAML easy to read alongside the server's config.yaml.
function parseDurationMs(s) {
  const m = String(s).match(/^(\d+(?:\.\d+)?)(ms|s|m)?$/);
  if (!m) return 5000;
  const n = parseFloat(m[1]);
  const unit = m[2] || 's';
  if (unit === 'ms') return n;
  if (unit === 's') return n * 1000;
  if (unit === 'm') return n * 60 * 1000;
  return 5000;
}
