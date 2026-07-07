// boot.js — entry point. Loaded once via the
// {{< partial "workstation-probe/scripts.html" . >}} partial (see
// layouts/partials/workstation-probe/scripts.html). Finds every
// .workstation-probe-panel element emitted by the panel shortcode and
// wires each one up: parse its embedded config, render the shell HTML,
// then subscribe to the shared poller for live updates.
import { subscribe } from './poller.js';
import { renderPanel } from './render.js';

const PANEL_CLASS = 'workstation-probe-panel';
const STATUS_CLASS = 'workstation-probe-status';
const SUB_CLASS = 'workstation-probe-sub';
const MODULE_ATTR = 'data-workstation-probe-module';
const SUB_STATE_ATTR = 'data-wsp-sub-state';
const PANEL_STALE_ATTR = 'data-wsp-stale';
const BOOTED_ATTR = 'data-workstation-probe-booted';
const CONFIG_ATTR = 'data-workstation-probe-config';
const REFRESH_SEL = '[data-workstation-probe-refresh]';
const URI_SEL = '[data-workstation-probe-uri]';
const HEADER_FACTS_SEL = '[data-workstation-probe-header-facts]';

const STALE_MULTIPLIER = 2.5; // panel dims when last fetch is older than refresh * N

function init() {
  const panels = document.querySelectorAll(`.${PANEL_CLASS}:not([${BOOTED_ATTR}])`);
  panels.forEach(bootOne);
}

function bootOne(panel) {
  panel.setAttribute(BOOTED_ATTR, '1');

  let cfg;
  try {
    cfg = JSON.parse(panel.getAttribute(CONFIG_ATTR));
  } catch (e) {
    panel.innerHTML = `<div class="workstation-probe-error">Invalid panel config: ${escapeHtml(e.message)}</div>`;
    return;
  }

  const refreshSeconds = parseRefreshSeconds(cfg.refresh || '5s');

  // Render the static shell. Inner DOM is rebuilt here so multiple panels
  // on the same page don't interfere with each other.
  panel.innerHTML = `
    <div class="${PANEL_CLASS}-header">
      <h3>${escapeHtml(cfg.name || cfg.api)}</h3>
      <span class="${PANEL_CLASS}-header-uri" ${URI_SEL.replace(/[\[\]]/g, '')}="${escapeHtml(cfg.api || '')}">${escapeHtml(cfg.api || '')}</span>
      <span class="${PANEL_CLASS}-header-facts" ${HEADER_FACTS_SEL.replace(/[\[\]]/g, '')}>loading</span>
      <div class="${PANEL_CLASS}-meta">
        <span class="${STATUS_CLASS}" data-state="connecting">
          <span class="${STATUS_CLASS}-dot" data-workstation-probe-status-dot></span>
          <span class="${PANEL_CLASS}-spinner" data-workstation-probe-loading-spinner></span>
          <span data-workstation-probe-status-text>loading</span>
        </span>
        <span class="workstation-probe-refresh" ${REFRESH_SEL.replace(/[\[\]]/g, '')}>↻ ${escapeHtml(cfg.refresh || '5s')}</span>
      </div>
    </div>
    <div class="${PANEL_CLASS}-body">
      ${(cfg.modules || []).map((m) => renderSubShell(m)).join('')}
    </div>
  `;

  // Subscribe once; the poller deduplicates by api+refresh+query.
  subscribe(cfg, (state) => {
    // Header status
    const statusEl = panel.querySelector(`.${STATUS_CLASS}`);
    const statusText = panel.querySelector('[data-workstation-probe-status-text]');
    const staleAgeMs = typeof state.lastSuccessAgeMs === 'number' ? state.lastSuccessAgeMs : null;
    const stale = staleAgeMs !== null && staleAgeMs > refreshSeconds * 1000 * STALE_MULTIPLIER;
    if (state.error && !state.data) {
      statusEl.setAttribute('data-state', 'error');
      statusEl.removeAttribute('data-wsp-state');
      statusText.textContent = `error`;
      statusEl.title = state.error.message || String(state.error);
    } else if (stale) {
      statusEl.setAttribute('data-state', 'stale');
      statusEl.removeAttribute('data-wsp-state');
      statusText.textContent = `stale`;
      statusEl.title = state.error ? state.error.message || String(state.error) : 'last successful sample is stale';
    } else if (state.error) {
      statusEl.setAttribute('data-state', 'warn');
      statusEl.removeAttribute('data-wsp-state');
      statusText.textContent = `retrying`;
      statusEl.title = state.error.message || String(state.error);
    } else {
      statusEl.setAttribute('data-state', 'ok');
      statusEl.removeAttribute('data-wsp-state');
      statusText.textContent = `${state.elapsedMs}ms`;
      statusEl.removeAttribute('title');
    }

    // Stale detection: mark the panel if last successful fetch is
    // older than refresh * STALE_MULTIPLIER. The CSS dims the panel.
    if (state.data) {
      if (stale) panel.setAttribute(PANEL_STALE_ATTR, 'true');
      else panel.removeAttribute(PANEL_STALE_ATTR);
    }

    if (state.data) {
      renderPanel(panel, cfg, state.data, state.profile);
    }
  });
}

// renderSubShell produces the per-module shell. Each module picks a
// body layout that matches its data shape:
//   cpu     : per-core tile grid
//   memory  : htop-style stacked bar
//   storage : per-mount row list
//   gpu     : per-device card grid
//
// All modules share the same meta structure: a wrapper div containing
// a data-wsp-sub-meta-text span that render.js writes to. CPU
// additionally has a static legend on the right of the meta line.
function renderSubShell(mod) {
  const title = escapeHtml(mod);
  const isCpu = mod === 'cpu';
  const isMemory = mod === 'memory';
  const isStorage = mod === 'storage';
  const isGpu = mod === 'gpu';
  const body = isCpu
    ? `<div class="${SUB_CLASS}-cores" data-wsp-cores></div>${renderLoadingBody('cpu')}`
    : isMemory
      ? `<div class="${SUB_CLASS}-membar" data-wsp-membar></div>${renderLoadingBody('memory')}`
      : isStorage
        ? `<div class="${SUB_CLASS}-storage" data-wsp-storage></div>${renderLoadingBody('storage')}`
        : isGpu
          ? `<div class="${SUB_CLASS}-gpu" data-wsp-gpu></div>${renderLoadingBody('gpu')}`
          : `<div class="${SUB_CLASS}-value" data-wsp-value data-loading="true">—</div>
             <div class="${SUB_CLASS}-chart"><canvas></canvas></div>${renderLoadingBody('generic')}`;
  const meta = isCpu
    ? `<div class="${SUB_CLASS}-meta">
         <span data-wsp-sub-meta-text>loading</span>
         <span class="${SUB_CLASS}-legend" title="core utilization scale: 0 (cool) → 100 (hot)">
           <span class="${SUB_CLASS}-legend-label">0</span>
           <span class="${SUB_CLASS}-legend-bar"></span>
           <span class="${SUB_CLASS}-legend-label">100</span>
         </span>
       </div>`
    : `<div class="${SUB_CLASS}-meta">
         <span data-wsp-sub-meta-text>loading</span>
       </div>`;
  return `
    <div class="${SUB_CLASS}" ${MODULE_ATTR}="${title}" ${SUB_STATE_ATTR}="connecting">
      <div class="${SUB_CLASS}-header">
        <span class="${SUB_CLASS}-title">${title}</span>
        <span class="${SUB_CLASS}-dot" data-wsp-sub-dot></span>
      </div>
      ${body}
      ${meta}
    </div>
  `;
}

function renderLoadingBody(mod) {
  if (mod === 'cpu') {
    return `
      <div class="${PANEL_CLASS}-loading-body ${PANEL_CLASS}-loading-cpu" data-wsp-loading>
        ${Array.from({ length: 12 }, () => `<span class="${PANEL_CLASS}-skeleton-block"></span>`).join('')}
      </div>
    `;
  }
  if (mod === 'memory') {
    return `
      <div class="${PANEL_CLASS}-loading-body ${PANEL_CLASS}-loading-memory" data-wsp-loading>
        <span class="${PANEL_CLASS}-skeleton-block ${PANEL_CLASS}-skeleton-bar"></span>
        <span class="${PANEL_CLASS}-skeleton-line"></span>
      </div>
    `;
  }
  if (mod === 'gpu') {
    return `
      <div class="${PANEL_CLASS}-loading-body ${PANEL_CLASS}-loading-gpu" data-wsp-loading>
        ${Array.from({ length: 3 }, () => `
          <div class="${PANEL_CLASS}-loading-card">
            <span class="${PANEL_CLASS}-skeleton-line ${PANEL_CLASS}-skeleton-line-short"></span>
            <span class="${PANEL_CLASS}-skeleton-line"></span>
            <span class="${PANEL_CLASS}-skeleton-line"></span>
            <span class="${PANEL_CLASS}-skeleton-line"></span>
          </div>
        `).join('')}
      </div>
    `;
  }
  if (mod === 'storage') {
    return `
      <div class="${PANEL_CLASS}-loading-body ${PANEL_CLASS}-loading-storage" data-wsp-loading>
        <span class="${PANEL_CLASS}-skeleton-line"></span>
        <span class="${PANEL_CLASS}-skeleton-line ${PANEL_CLASS}-skeleton-line-short"></span>
      </div>
    `;
  }
  return `
    <div class="${PANEL_CLASS}-loading-body" data-wsp-loading>
      <span class="${PANEL_CLASS}-skeleton-line"></span>
      <span class="${PANEL_CLASS}-skeleton-line ${PANEL_CLASS}-skeleton-line-short"></span>
    </div>
  `;
}

function parseRefreshSeconds(s) {
  const m = String(s).match(/^(\d+(?:\.\d+)?)(ms|s|m)?$/);
  if (!m) return 5;
  const n = parseFloat(m[1]);
  const unit = m[2] || 's';
  if (unit === 'ms') return n / 1000;
  if (unit === 's') return n;
  if (unit === 'm') return n * 60;
  return 5;
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]));
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', init);
} else {
  init();
}
