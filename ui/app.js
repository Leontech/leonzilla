"use strict";
/* Leonzilla – UI */

const TOKEN = (location.hash.match(/t=(\w+)/) || [])[1] || sessionStorage.getItem("lz-token") || "";
if (TOKEN) { sessionStorage.setItem("lz-token", TOKEN); history.replaceState(null, "", location.pathname); }

const $ = (s, el = document) => el.querySelector(s);
const esc = (s) => String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
// Blur handlers that change the layout (closing the "vzor" field, re-rendering chips) must wait
// until the mouse button is released – otherwise the button under the cursor moves away and the click is lost.
let pointerIsDown = false;
document.addEventListener("pointerdown", () => (pointerIsDown = true), true);
document.addEventListener("pointerup", () => setTimeout(() => (pointerIsDown = false), 0), true);
function afterPointer(fn) {
  if (!pointerIsDown) return fn();
  document.addEventListener("pointerup", () => setTimeout(fn, 0), { once: true, capture: true });
}
const icon = (n) => `<svg><use href="#i-${n}"/></svg>`;

const S = {
  tabs: [], settings: {}, jobs: {}, current: null, locked: false, colors: [], defaultExcludes: [],
  canPickDir: false, dataDir: "", version: "", localOK: {},
  job: null, jobTab: null, logSeen: 0, pollTimer: null, saveTimers: {}, pendingSave: 0,
  editing: new Set(), // cards unlocked for editing (everything else is read-only)
  // card editing is a draft since 1.11.15: changes stay in the card object until "Uložit"; "Zavřít bez uložení" restores this copy
  snap: {},           // card id → copy of the card when editing started
  dirty: new Set(),   // cards with unsaved changes
  open: [],            // cards shown as tabs in the top bar (opened from the overview, closable; per PC)
  view: "overview",    // overview (tiles of all cards) | card (one card in detail)
  jobData: {},         // latest job snapshot per card (overview)
  checked: {},         // last "Zkontrolovat" (preview) result per card
};
const editing = (id) => S.editing.has(id);
const groups = () => S.settings.groups || [];
const builds = () => S.settings.builds || [];
const cardBuilds = (t) => (t.builds || []).map((id) => builds().find((b) => b.id === id)).filter(Boolean);
// "Build light" (in the UI "Vlastní skript" / "Skript"): the card's own script (Python, BAT, PowerShell) run like a build (then upload)
const hasLight = (t) => !!(t.buildLight && (t.buildLight.script || "").trim());
// own label of the main button (build light), e.g. "Build a nahrát na web"; "" = default label
const lightLabel = (t) => (hasLight(t) ? (t.buildLight.label || "").trim() : "");
const lightName = (t) => (t.buildLight?.script || "").trim().replace(/^"|"$/g, "").split(/[\\/]/).pop();
// kind of the Build light script by its extension (shown in the build list / confirmation)
const lightKind = (t) => {
  const ext = (lightName(t).match(/\.([^.]+)$/) || [])[1]?.toLowerCase() || "";
  return { py: "Python", pyw: "Python", bat: "BAT", cmd: "CMD", ps1: "PowerShell", exe: "EXE" }[ext] || (ext ? ext.toUpperCase() : tr("skript"));
};
const hasBuilds = (t) => cardBuilds(t).length > 0 || hasLight(t);
// the card runs only its own script (no RAD Studio build) – the UI then says "Skript" instead of "Build"
// (stored in config.json as buildLight like before, so existing cards keep working)
const scriptOnly = (t) => hasLight(t) && cardBuilds(t).length === 0;
const buildNames = (t) => [...cardBuilds(t).map((b) => b.name), ...(hasLight(t) ? [lightName(t)] : [])];
// "Info k verzi" (version info) of a build, passed to the post-build in LEONZILLA_VERSION_INFO; not set yet = build name
const buildInfo = (b) => (b.versionInfo ?? b.name ?? "").replace(/\s+/g, " ").trim().slice(0, 80);
// what the card will send: last text used on this card, otherwise the build setting
const cardBuildInfo = (t, b) => (t.buildInfo && Object.prototype.hasOwnProperty.call(t.buildInfo, b.id) ? t.buildInfo[b.id] : buildInfo(b));
const activeBackups = (t) => (t.backups || []).filter((b) => b.enabled && (b.source || "").trim() && (b.target || "").trim());
const hasBackups = (t) => activeBackups(t).length > 0;
const zipPrefix = (b) => ((b.prefix || "").trim() || (b.source || "").replace(/[\\/]+$/, "").split(/[\\/]/).pop() || "zaloha").replace(/[<>:"/\\|?*]+/g, "_");
const zipStampNow = () => { const d = new Date(), z = (n) => String(n).padStart(2, "0"); return `${d.getFullYear()}-${z(d.getMonth() + 1)}-${z(d.getDate())}_${z(d.getHours())}-${z(d.getMinutes())}-${z(d.getSeconds())}`; };
const activeGit = (t) => (t.git || []).filter((g) => g.enabled && (g.path || "").trim());
const hasGit = (t) => activeGit(t).length > 0;
// FTP part of the card is used (server or folder pairs filled in)
const syncSet = (t) => !!(t.conn.host || t.pairs.some((p) => (p.local || "").trim()));
const hasFw = (t) => !!(t.fw && (t.fw.project || "").trim() && (t.fw.variants || []).length);
const hasMon = (t) => !!t.fw?.monitor;
const cardUnset = (t) => !syncSet(t) && !hasBuilds(t) && !hasBackups(t) && !hasGit(t) && !hasFw(t) && !hasMon(t);
// synchronization can run only with a server AND an enabled folder pair
const syncReady = (t) => !!(t.conn.host && t.pairs.some((p) => p.enabled && (p.local || "").trim()));
const syncHalfText = (t) => (t.conn.host ? "chybí zapnutá složka v PC" : "chybí server") + " – doplň ho, nebo dvojici složek odeber, pokud karta synchronizovat nemá";
const mainMode = (t) => (hasBuilds(t) ? (syncReady(t) ? "buildsync" : "build") : syncReady(t) ? "sync" : "");
// default text of the main button (without the card's own label)
const mainLabelDefault = (t) => (scriptOnly(t) ? { buildsync: "Skript + Synchronizovat", build: "Spustit skript", sync: "Synchronizovat" }
  : { buildsync: "Build + Synchronizovat", build: "Build", sync: "Synchronizovat" })[mainMode(t)] || "";
const mainLabel = (t) => { const m = mainMode(t); if ((m === "buildsync" || m === "build") && lightLabel(t)) return lightLabel(t); return mainLabelDefault(t); };
const groupOf = (t) => (groups().some((g) => g.id === t.group) ? t.group : "");
const groupName = (id) => groups().find((g) => g.id === id)?.name || "";
// cards in display order: by group (settings order, "no group" last), then by card order
function orderedTabs() {
  const idx = (t) => { const g = groupOf(t); const i = groups().findIndex((x) => x.id === g); return i < 0 ? 1e6 : i; };
  return S.tabs.map((t, i) => [t, i]).sort((a, b) => idx(a[0]) - idx(b[0]) || a[1] - b[1]).map((x) => x[0]);
}
// moves card `id` into group `group`, before card `beforeId` (or to the end of the group), and saves
function moveCard(id, group, beforeId) {
  const t = S.tabs.find((x) => x.id === id);
  if (!t || id === beforeId) return;
  t.group = group;
  let list = orderedTabs().filter((x) => x.id !== id);
  let at = beforeId ? list.findIndex((x) => x.id === beforeId) : -1;
  if (at < 0) {
    // after the last card of the target group
    at = list.length;
    for (let i = list.length - 1; i >= 0; i--) if (groupOf(list[i]) === group) { at = i + 1; break; }
    if (at === list.length) {
      const gi = groups().findIndex((g) => g.id === group);
      const firstAfter = list.findIndex((x) => { const xi = groups().findIndex((g) => g.id === groupOf(x)); return (xi < 0 ? 1e6 : xi) > (gi < 0 ? 1e6 : gi); });
      if (firstAfter >= 0) at = firstAfter;
    }
  }
  list.splice(at, 0, t);
  S.tabs = list;
  renderTabs();
  if (S.view === "overview") renderOverview();
  api("POST", "/api/layout", { items: S.tabs.map((x) => ({ id: x.id, group: groupOf(x) })) }).catch((e) => toast(e.message, true));
}

async function api(method, path, body) {
  // folder / file dialogs: the window gets focus back before the answer arrives (see the focus handler)
  const dlg = path.startsWith("/api/pick");
  if (dlg) S.inflight = (S.inflight || 0) + 1;
  let r;
  try {
    r = await fetch(path, {
      method, headers: { "X-Token": TOKEN, "Content-Type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  } finally { if (dlg) setTimeout(() => S.inflight--, 400); }
  let data = {};
  try { data = await r.json(); } catch {}
  if (!r.ok) {
    if (r.status === 423) { S.locked = true; render(); }
    throw new Error(data.error || `Chyba ${r.status}`);
  }
  return data;
}

function toast(msg, err = false) {
  const t = document.createElement("div");
  t.className = "toast" + (err ? " err" : "");
  t.textContent = msg;
  $("#toasts").append(t);
  setTimeout(() => t.remove(), err ? 7000 : 3500);
}

const fmtBytes = (b) => b >= 1 << 30 ? (b / (1 << 30)).toFixed(2) + " GB" : b >= 1 << 20 ? (b / (1 << 20)).toFixed(1) + " MB" : b >= 1024 ? Math.round(b / 1024) + " kB" : b + " B";
const fmtNum = (n) => Number(n || 0).toLocaleString(I18N.locale());
function fmtWhen(iso) {
  const d = new Date(iso), now = new Date();
  const t = d.toLocaleTimeString(I18N.locale(), { hour: "2-digit", minute: "2-digit" });
  if (d.toDateString() === now.toDateString()) return tr("dnes") + " " + t;
  const y = new Date(now); y.setDate(now.getDate() - 1);
  if (d.toDateString() === y.toDateString()) return tr("včera") + " " + t;
  return d.toLocaleDateString(I18N.locale(), { day: "numeric", month: "numeric", year: d.getFullYear() === now.getFullYear() ? undefined : "numeric" }) + " " + t;
}
const fmtDur = (s) => s < 60 ? (I18N.lang === "en" ? s.toFixed(1) : s.toFixed(1).replace(".", ",")) + " s" : `${Math.floor(s / 60)} min ${Math.round(s % 60)} s`;
const PROTO = { ftp: "FTP", ftps: "FTPS", "ftps-implicit": "FTPS implicitní", sftp: "SFTP" };
const DEFPORT = { ftp: 21, ftps: 21, "ftps-implicit": 990, sftp: 22 };
const cur = () => S.tabs.find((t) => t.id === S.current);
const running = (id) => !!S.jobs[id] && S.jobs[id] !== "done";

/* ------------------------------------------------------------------ state */

async function load() {
  try {
    const st = await api("GET", "/api/state");
    I18N.setLang(st.lang);
    S.locked = st.locked; S.settings = st.settings; S.colors = st.colors; S.defaultExcludes = st.defaultExcludes;
    S.canPickDir = st.canPickDir; S.dataDir = st.dataDir; S.version = st.version; S.defaultBackupExcludes = st.defaultBackupExcludes || []; S.gitFound = !!st.gitFound;
    S.autostart = !!st.autostart; S.autostartSupported = !!st.autostartSupported;
    S.idfProfile = st.idfProfile || ""; S.fwRunning = st.fwRunning || {};
    Object.keys(S.fwRunning).forEach((id) => fwBgKick(id));
    S.tabs = st.tabs || []; S.jobs = st.jobs || {}; S.pc = st.pc || "";
    applyTheme();
    // window state of this PC (collapsed groups, open tabs, last card) – kept by the app per computer, see uiGet
    S.ui = st.ui && typeof st.ui === "object" ? st.ui : {};
    const remembered = uiGet("current", null);
    if (!S.tabs.some((t) => t.id === S.current)) S.current = S.tabs.some((t) => t.id === remembered) ? remembered : S.tabs[0]?.id ?? null;
    const view = uiGet("view", "overview");
    const open = uiGet("open", null);
    S.open = Array.isArray(open) ? open.filter((id) => S.tabs.some((t) => t.id === id)) : [];
    S.view = view === "card" && S.current ? "card" : "overview";
    if (S.view === "card") openTab(S.current);
    render();
    if (S.view === "card") { checkLocal(); if (running(S.current)) watchJob(S.current); }
    else checkAllLocal();
    bgKick();
  } catch (e) {
    document.body.innerHTML = `<div class="empty"><h1>Leonzilla neodpovídá</h1><p>${esc(e.message)}</p></div>`;
  }
}

function applyTheme() {
  const t = S.settings.theme || "auto";
  if (t === "auto") document.documentElement.removeAttribute("data-theme");
  else document.documentElement.setAttribute("data-theme", t);
}

/* Per-PC window state: saved by the app into Leonzilla_data/ui/<computer>.json (the page runs on a random local port,
   so the browser storage would forget it on every start). The old browser storage is only read as a fallback. */
const UI_OLD = { current: "lz-current", view: "lz-view", open: "lz-open", collapsed: "lz-collapsed" };
function uiGet(key, def) {
  if (S.ui && key in S.ui) return S.ui[key];
  try {
    const v = localStorage.getItem(UI_OLD[key]);
    if (v != null) return key === "open" || key === "collapsed" ? JSON.parse(v) : v;
  } catch {}
  return def;
}
let uiTimer = null, uiDirty = {};
function uiSet(key, value) {
  (S.ui ||= {})[key] = value;
  uiDirty[key] = value;
  clearTimeout(uiTimer);
  uiTimer = setTimeout(uiFlush, 400);
}
function uiFlush() {
  const body = uiDirty; uiDirty = {};
  if (Object.keys(body).length) api("POST", "/api/ui", body).catch(() => {});
}
addEventListener("pagehide", () => { clearTimeout(uiTimer); uiFlush(); });

function saveOpen() { uiSet("open", S.open); }
function openTab(id) { if (id && !S.open.includes(id)) { S.open.push(id); saveOpen(); } }

// closes the tab of a card (the card stays – it is opened again from the overview)
function closeTab(id) {
  const t = S.tabs.find((x) => x.id === id);
  if (t && S.dirty.has(id)) return askUnsaved(t, () => closeTab(id));
  if (t && editing(id)) leaveEdit(t);
  const vis = orderedTabs().filter((x) => S.open.includes(x.id)).map((x) => x.id);
  const at = vis.indexOf(id);
  S.open = S.open.filter((x) => x !== id);
  saveOpen();
  if (S.view === "card" && S.current === id) {
    const next = vis[at + 1] || vis[at - 1];
    if (next && next !== id) return select(next);
    return showOverview();
  }
  renderTabs();
}

function select(id) {
  openTab(id);
  const prev = cur();
  if (prev && prev.id !== id && editing(prev.id) && !S.dirty.has(prev.id)) leaveEdit(prev); // unsaved draft stays (● on the tab)
  S.current = id;
  S.view = "card";
  uiSet("current", id); uiSet("view", "card");
  renderTabs(); renderMain(); checkLocal();
  const j = S.jobs[id];
  if (j) watchJob(id); else hideActivity();
  bgKick();
}

function showOverview() {
  const prev = cur();
  if (prev && editing(prev.id) && !S.dirty.has(prev.id)) leaveEdit(prev);
  S.view = "overview";
  uiSet("view", "overview");
  clearTimeout(S.pollTimer);
  S.jobTab = null;
  hideActivity();
  renderTabs(); renderMain(); checkAllLocal();
  bgKick();
}

async function checkAllLocal() {
  const paths = [...new Set(S.tabs.flatMap((t) => [...t.pairs.map((p) => p.local), ...(t.backups || []).map((b) => b.source)].filter(Boolean)))];
  if (!paths.length) return;
  try { Object.assign(S.localOK, await api("POST", "/api/localcheck", { paths })); } catch {}
}

/* ------------------------------------------------------------------ saving */

// autosave: typing waits 800 ms of quiet (fewer writes into Dropbox); a failed save is retried once quietly
// before an error is shown – Dropbox briefly locks the freshly changed config.json while it uploads it
function scheduleSave(tab, delay = 800) {
  if (editing(tab.id)) return markDirty(tab); // edit mode: saved only by "Uložit"
  clearTimeout(S.saveTimers[tab.id]);
  setSaveState("Ukládám…");
  S.saveTimers[tab.id] = setTimeout(() => saveTab(tab), delay);
}

async function saveTab(tab) {
  clearTimeout(S.saveTimers[tab.id]);
  delete S.saveTimers[tab.id];
  S.pendingSave++;
  try {
    await putTab(tab);
    setSaveState("Uloženo");
    delete (S.saveRetry ||= {})[tab.id];
  } catch (e) {
    S.saveRetry ||= {};
    if (!S.saveRetry[tab.id] && !S.saveTimers[tab.id]) {
      S.saveRetry[tab.id] = true;
      setSaveState("Ukládám…");
      S.saveTimers[tab.id] = setTimeout(() => saveTab(tab), 2000);
    } else if (!S.saveTimers[tab.id]) {
      delete S.saveRetry[tab.id];
      setSaveState("");
      toast("Uložení se nepovedlo: " + e.message, true);
    }
  } finally { S.pendingSave--; }
}

// PUT the card; ids assigned by the server are copied back into the open card (throws on error)
async function putTab(tab) {
  const body = JSON.parse(JSON.stringify(tab));
  const fresh = await api("PUT", `/api/tabs/${tab.id}`, body);
  delete tab.conn.passNew;
  tab.conn.hasPass = fresh.conn.hasPass;
  tab.conn.hostKey = fresh.conn.hostKey;
  fresh.pairs.forEach((p, i) => { if (tab.pairs[i] && !tab.pairs[i].id) tab.pairs[i].id = p.id; });
  (fresh.backups || []).forEach((b, i) => { if (tab.backups?.[i] && !tab.backups[i].id) tab.backups[i].id = b.id; });
  (fresh.git || []).forEach((g, i) => { if (tab.git?.[i] && !tab.git[i].id) tab.git[i].id = g.id; });
  (fresh.fw?.variants || []).forEach((v, i) => { const mine = tab.fw?.variants?.[i]; if (mine && !mine.id) mine.id = v.id; });
  return fresh;
}

   when closing the tab) puts the card back as it was. Switching to another card keeps the draft (● on the tab). */
function beginEdit(t) {
  S.editing.add(t.id);
  S.snap[t.id] = JSON.parse(JSON.stringify(t));
  S.dirty.delete(t.id);
}
function leaveEdit(t) {
  S.editing.delete(t.id);
  delete S.snap[t.id];
  S.dirty.delete(t.id);
}
function markDirty(t) {
  const first = !S.dirty.has(t.id);
  S.dirty.add(t.id);
  if (first) { renderTabs(); drawEditNote(t); }
}
function drawEditNote(t) {
  const el = $("#edit-note");
  if (el && S.current === t.id) {
    el.textContent = S.dirty.has(t.id) ? "Neuložené změny – uložíš tlačítkem Uložit" : "Úpravy karty · změny se uloží tlačítkem Uložit";
    el.classList.toggle("dirty", S.dirty.has(t.id));
  }
}
function revertEdit(t) {
  const snap = S.snap[t.id];
  if (snap) { for (const k of Object.keys(t)) delete t[k]; Object.assign(t, JSON.parse(JSON.stringify(snap))); }
  leaveEdit(t);
}
// card with unsaved changes is being closed: save / discard / stay
function askUnsaved(t, then) {
  modal(`
    <div class="modal-h"><h3>Neuložené změny karty „${esc(t.name || "bez názvu")}“</h3><p>Úpravy této karty ještě nejsou uložené.</p></div>
    <div class="modal-f"><button class="ghost" data-close>Zpět k úpravám</button><button class="danger" id="m-drop">Zahodit změny</button><button class="primary" id="m-save">Uložit</button></div>`,
    (box, close) => {
      $("#m-drop", box).onclick = () => { close(); revertEdit(t); render(); then?.(); };
      $("#m-save", box).onclick = async () => { close(); if (await commitEdit(t)) then?.(); };
    });
}
// "Uložit": writes the draft; on success the card is locked again
async function commitEdit(t) {
  clearTimeout(S.saveTimers[t.id]); delete S.saveTimers[t.id];
  if (S.dirty.has(t.id)) {
    S.pendingSave++;
    try { await putTab(t); }
    catch (e) { toast("Uložení se nepovedlo: " + e.message, true); return false; }
    finally { S.pendingSave--; }
    setSaveState("Uloženo");
  }
  leaveEdit(t);
  renderTabs(); renderMain(); checkLocal();
  return true;
}

async function flushSave(tab) {
  if (S.saveTimers[tab.id]) await saveTab(tab);
  while (S.pendingSave > 0) await new Promise((r) => setTimeout(r, 50));
}

let saveStateTimer;
function setSaveState(t) {
  const el = $("#save-state");
  el.textContent = t;
  clearTimeout(saveStateTimer);
  if (t === "Uloženo") saveStateTimer = setTimeout(() => (el.textContent = ""), 1800);
}

async function checkLocal() {
  const t = cur();
  if (!t) return;
  const paths = [...t.pairs.map((p) => p.local), ...(t.backups || []).map((b) => b.source)].filter(Boolean);
  if (!paths.length) return;
  try {
    const res = await api("POST", "/api/localcheck", { paths });
    Object.assign(S.localOK, res);
    document.querySelectorAll(".vpath[data-local]").forEach((el) => {
      el.classList.toggle("missing", el.dataset.local !== "" && S.localOK[el.dataset.local] === false);
    });
    document.querySelectorAll("[data-local-field]").forEach((el) => {
      const v = el.querySelector("input").value.trim();
      el.classList.toggle("missing", v !== "" && S.localOK[v] === false);
    });
  } catch {}
}

/* ------------------------------------------------------------------ render */

function render() {
  if (S.locked) return renderLock();
  $(".bar").hidden = false;
  renderTabs();
  renderMain();
}

function renderLock() {
  $("#tabs").innerHTML = "";
  $("#main").innerHTML = `
    <div class="empty">
      <svg class="mark"><use href="#i-lock"/></svg>
      <h1>Leonzilla je zamčená</h1>
      <p>Hesla k serverům jsou zašifrovaná hlavním heslem.</p>
      <form class="lock-form" id="lock-form">
        <input type="password" id="lock-pass" placeholder="Hlavní heslo" autofocus>
        <button class="primary">Odemknout</button>
      </form>
    </div>`;
  $("#lock-pass").focus();
  $("#lock-form").onsubmit = async (e) => {
    e.preventDefault();
    try { await api("POST", "/api/unlock", { password: $("#lock-pass").value }); S.locked = false; load(); }
    catch (err) { toast(err.message, true); $("#lock-pass").select(); }
  };
}

function renderTabs() {
  const nav = $("#tabs");
  const anyRunning = S.tabs.some((t) => running(t.id));
  nav.innerHTML = `
    <div class="tab ov${anyRunning ? " running" : ""}" role="tab" tabindex="0" id="tab-ov" aria-selected="${S.view === "overview"}" title="Přehled všech projektů" style="--c:var(--spruce)">
      <svg class="ovi"><use href="#i-grid"/></svg><span class="name">Přehled</span>
    </div>` + orderedTabs().filter((t) => S.open.includes(t.id)).map((t, i, arr) => `${i > 0 && groupOf(arr[i - 1]) !== groupOf(t) ? `<span class="tab-sep" aria-hidden="true"></span>` : ""}
    <div class="tab${running(t.id) ? " running" : ""}${editing(t.id) ? " editing" : ""}" role="tab" tabindex="0" draggable="true"
         aria-selected="${S.view === "card" && t.id === S.current}" data-id="${t.id}" style="--c:${esc(t.color)}" title="${esc(t.name)}">
      <span class="dot"></span><span class="name" translate="no">${esc(t.name)}</span>${editing(t.id) ? `<svg class="ed" aria-label="upravuje se"><use href="#i-gear"/></svg>` : ""}${S.dirty.has(t.id) ? `<span class="unsaved" title="Neuložené změny">●</span>` : ""}
      <button class="tab-x" data-close-tab title="Zavřít záložku (Ctrl+W) – karta zůstane v Přehledu" aria-label="Zavřít">${icon("x")}</button>
    </div>`).join("");
  const ov = $("#tab-ov");
  ov.onclick = () => S.view !== "overview" && showOverview();
  ov.onkeydown = (e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); showOverview(); } };
  nav.querySelectorAll(".tab[data-id]").forEach((el) => {
    const id = el.dataset.id;
    el.onclick = (e) => {
      if (e.target.closest("[data-close-tab]")) { e.stopPropagation(); return closeTab(id); }
      if (id !== S.current || S.view !== "card") select(id);
    };
    el.onauxclick = (e) => { if (e.button === 1) { e.preventDefault(); closeTab(id); } }; // middle click
    el.onkeydown = (e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); select(id); } };
    el.ondblclick = () => { if (editing(id)) startRename(el, id); };
    el.oncontextmenu = (e) => { e.preventDefault(); tabMenu(id, e.clientX, e.clientY); };
    el.ondragstart = (e) => { e.dataTransfer.setData("text/plain", id); e.dataTransfer.effectAllowed = "move"; };
    el.ondragover = (e) => { e.preventDefault(); el.classList.add("drag-over"); };
    el.ondragleave = () => el.classList.remove("drag-over");
    el.ondrop = (e) => {
      e.preventDefault(); el.classList.remove("drag-over");
      const from = e.dataTransfer.getData("text/plain");
      if (!from || from === id) return;
      const target = S.tabs.find((t) => t.id === id);
      moveCard(from, groupOf(target), id);
    };
  });
  requestAnimationFrame(() => {
    nav.querySelector('.tab[aria-selected="true"]')?.scrollIntoView({ block: "nearest", inline: "nearest" });
    updateTabArrows();
  });
}

// arrows when the open tabs don't fit; mouse wheel scrolls the tab strip too
function updateTabArrows() {
  const nav = $("#tabs");
  const l = $("#tabs-left"), r = $("#tabs-right");
  if (!nav || !l) return;
  const over = nav.scrollWidth > nav.clientWidth + 1;
  l.hidden = r.hidden = !over;
  l.disabled = nav.scrollLeft <= 0;
  r.disabled = nav.scrollLeft + nav.clientWidth >= nav.scrollWidth - 1;
}
(() => {
  const nav = $("#tabs");
  const by = (d) => nav.scrollBy({ left: d * Math.max(160, nav.clientWidth * 0.6), behavior: "smooth" });
  $("#tabs-left").onclick = () => by(-1);
  $("#tabs-right").onclick = () => by(1);
  nav.addEventListener("scroll", updateTabArrows, { passive: true });
  nav.addEventListener("wheel", (e) => { if (Math.abs(e.deltaY) > Math.abs(e.deltaX)) { nav.scrollLeft += e.deltaY; e.preventDefault(); } }, { passive: false });
  window.addEventListener("resize", updateTabArrows);
})();

function startRename(el, id) {
  const t = S.tabs.find((x) => x.id === id);
  const nameEl = el.querySelector(".name");
  const inp = document.createElement("input");
  inp.className = "rename"; inp.value = t.name;
  nameEl.replaceWith(inp);
  inp.focus(); inp.select();
  const done = (ok) => {
    if (ok && inp.value.trim()) { t.name = inp.value.trim(); scheduleSave(t, 0); }
    renderTabs(); if (id === S.current) $("#title").value = t.name;
  };
  inp.onkeydown = (e) => { if (e.key === "Enter") done(true); if (e.key === "Escape") done(false); e.stopPropagation(); };
  inp.onblur = () => done(true);
  inp.onclick = (e) => e.stopPropagation();
}

function tabMenu(id, x, y) {
  const t = S.tabs.find((v) => v.id === id);
  const m = $("#ctx");
  const ed = editing(id);
  m.innerHTML = ed ? `
    <button data-a="rename">Přejmenovat</button>
    <button data-a="dup">Duplikovat kartu</button>
    <div class="colors">${S.colors.map((c) => `<button data-c="${c}" style="--c:${c}" aria-pressed="${c === t.color}" title="Barva karty"></button>`).join("")}</div>
    <hr><button data-a="del" class="del">Smazat kartu…</button>` : `
    <button data-a="edit">${icon("gear")}Upravit kartu</button>
    <button data-a="dup">Duplikovat kartu</button>
    <hr><button data-a="del" class="del">Smazat kartu…</button>`;
  m.hidden = false;
  m.style.left = Math.min(x, innerWidth - 210) + "px";
  m.style.top = Math.min(y, innerHeight - 200) + "px";
  m.onclick = async (e) => {
    const b = e.target.closest("button");
    if (!b) return;
    m.hidden = true;
    if (b.dataset.c) { t.color = b.dataset.c; renderTabs(); scheduleSave(t, 0); if (editing(t.id) && S.current === t.id) renderMain(); return; }
    if (b.dataset.a === "edit") { if (id !== S.current) select(id); startEdit(t); return; }
    if (b.dataset.a === "rename") { if (id !== S.current) select(id); startRename($(`.tab[data-id="${id}"]`), id); }
    if (b.dataset.a === "dup") duplicateTab(t);
    if (b.dataset.a === "del") deleteTab(t);
  };
}
document.addEventListener("click", (e) => { if (!e.target.closest("#ctx")) $("#ctx").hidden = true; });

// copy of the card with all settings (server, password, folders, builds, backups, GitHub) right after it, opened for editing
async function duplicateTab(t) {
  try {
    if (S.dirty.has(t.id) && !(await commitEdit(t))) return; // the copy is made from the saved card
    await flushSave(t);
    const n = await api("POST", `/api/tabs/${t.id}/duplicate`);
    const at = S.tabs.findIndex((x) => x.id === t.id);
    S.tabs.splice(at < 0 ? S.tabs.length : at + 1, 0, n);
    beginEdit(n);
    select(n.id);
    setTimeout(() => { $("#title")?.focus(); $("#title")?.select(); }, 30);
    toast("Karta zkopírovaná – uprav, co se liší");
  } catch (err) { toast(err.message, true); }
}

function deleteTab(t) {
  modal(`
    <div class="modal-h"><h3>Smazat kartu „${esc(t.name)}“?</h3>
    <p>Smaže se jen nastavení v Leonzille. Soubory v PC ani na serveru se nemění.</p></div>
    <div class="modal-f"><button class="ghost" data-close>Zrušit</button><button class="danger" id="m-ok">Smazat kartu</button></div>`,
    (box, close) => {
      $("#m-ok", box).onclick = async () => {
        try {
          await api("DELETE", `/api/tabs/${t.id}`);
          S.tabs = S.tabs.filter((x) => x.id !== t.id);
          leaveEdit(t);
          S.open = S.open.filter((x) => x !== t.id); saveOpen();
          close();
          S.current = S.tabs[0]?.id ?? null;
          S.view = "overview";
          render(); hideActivity();
        } catch (e) { toast(e.message, true); }
      };
    });
}

function renderMain() {
  const main = $("#main");
  const t = cur();
  if (S.view === "overview" && S.tabs.length) return renderOverview();
  if (!t) {
    main.innerHTML = `
      <div class="empty">
        <svg class="mark"><use href="#i-mark"/></svg>
        <h1>Leonzilla</h1>
        <p>Každý projekt má vlastní kartu: přístup na server a dvojice složek v PC a na serveru. Synchronizace jen nahrává nové a změněné soubory – na serveru nic nemaže.</p>
        <button class="primary" id="btn-first">${icon("plus")}Vytvořit první kartu</button>
      </div>`;
    $("#btn-first").onclick = addTab;
    return;
  }
  if (!editing(t.id)) return renderView(t);
  const c = t.conn;
  main.innerHTML = `
  <div class="page">
    <section class="hero is-editing">
      <input class="title-input" id="title" value="${esc(t.name)}" aria-label="Název karty" spellcheck="false">
      <div class="hero-actions">
        <div class="row"><button class="plain small del-card" id="btn-del" title="Smazat kartu (jen nastavení v Leonzille)">${icon("x")}Smazat</button>
        <button class="plain small" id="btn-dup" title="Nová karta se stejným nastavením">${icon("copy")}Duplikovat</button>
        <button class="plain small" id="btn-cancel" title="Zahodí změny od otevření úprav">${icon("x")}Zavřít bez uložení</button>
        <button class="primary done" id="btn-done">${icon("lock")}Uložit</button></div>
      </div>
      <div class="hero-sub edit-note${S.dirty.has(t.id) ? " dirty" : ""}" id="edit-note">${S.dirty.has(t.id) ? "Neuložené změny – uložíš tlačítkem Uložit" : "Úpravy karty · změny se uloží tlačítkem Uložit"}</div>
      <div class="hero-group">
        <label for="f-group">Skupina</label>
        <select id="f-group"><option value="">Bez skupiny</option>${groups().map((g) => `<option value="${g.id}" ${g.id === groupOf(t) ? "selected" : ""}>${esc(g.name)}</option>`).join("")}</select>
        <button class="plain small" id="btn-groups">Spravovat skupiny</button>
      </div>
      <div class="hero-group hero-color">
        <span class="lbl">Barva karty</span>
        <div class="colors" id="f-colors" role="radiogroup" aria-label="Barva karty">${S.colors.map((col) => `<button type="button" data-c="${col}" style="--c:${col}" role="radio" aria-pressed="${col === t.color}" aria-checked="${col === t.color}" title="Barva karty"></button>`).join("")}</div>
      </div>
      <div class="hero-group hero-dropbox">
        <label for="f-dropbox">Dropbox</label>
        <select id="f-dropbox">
          <option value="" ${!t.dropbox ? "selected" : ""}>nehlídat</option>
          <option value="ask" ${t.dropbox === "ask" ? "selected" : ""}>zkontrolovat pauzu, jinak se zeptat</option>
          <option value="stop" ${t.dropbox === "stop" ? "selected" : ""}>vždy vypnout na dobu buildu</option>
        </select>
        <span class="note">při buildu: vlastní skript, FW ESP32, RAD Studio</span>
      </div>
    </section>

    <section class="sec">
      <div class="sec-h"><h2>Synchronizace na server – složky</h2><span class="hint">vlevo složka v PC, vpravo cíl na serveru · nepovinné</span></div>
      <div class="pairs" id="pairs"></div>
      <button class="ghost small add-pair" id="btn-add-pair" style="margin-top:10px">${icon("plus")}Přidat dvojici složek</button>
    </section>

    <details class="sec" id="sec-server" open>
      <summary><div class="sec-h">${icon("chev")}<h2>Server</h2><span class="hint">${c.host ? esc(`${PROTO[c.protocol]} · ${c.host}${c.port ? ":" + c.port : ""}`) : "přístupové údaje od providera · nepovinné"}</span></div></summary>
      <div class="form">
        <div class="f c3"><label for="f-proto">Protokol</label>
          <select id="f-proto">${Object.entries(PROTO).map(([k, v]) => `<option value="${k}" ${k === c.protocol ? "selected" : ""}>${v}</option>`).join("")}</select></div>
        <div class="f c7"><label for="f-host">Server</label><input id="f-host" value="${esc(c.host)}" placeholder="ftp.mujweb.cz" spellcheck="false"></div>
        <div class="f c2"><label for="f-port">Port</label><input id="f-port" type="number" min="0" max="65535" value="${c.port || ""}" placeholder="${DEFPORT[c.protocol]}"></div>
        <div class="f c5"><label for="f-user">Uživatel</label><input id="f-user" value="${esc(c.user)}" autocomplete="off" spellcheck="false"></div>
        <div class="f c7"><label for="f-pass">Heslo</label>
          <div class="pass-wrap"><input id="f-pass" type="password" autocomplete="new-password" placeholder="${c.hasPass ? "•••••••• (uloženo – napiš nové pro změnu)" : ""}"></div></div>
        <div class="f c12" id="row-key" ${c.protocol === "sftp" ? "" : "hidden"}><label for="f-key">Soubor s privátním klíčem (nepovinné – heslo pak slouží jako heslo ke klíči)</label>
          <div class="path-field"><input id="f-key" value="${esc(c.keyFile || "")}" placeholder="C:\\Users\\…\\.ssh\\id_ed25519" spellcheck="false">
          <button id="pick-key" title="Vybrat soubor">${icon("file")}</button></div></div>
        <div class="f c12 checks">
          <label class="check"><input type="checkbox" id="f-safe" ${t.safeUpload ? "checked" : ""}>Nahrávat přes dočasný soubor (web nikdy neuvidí napůl nahraný soubor)</label>
          <label class="check" data-ftp ${c.protocol === "sftp" ? "hidden" : ""}><input type="checkbox" id="f-epsv" ${c.disableEPSV ? "checked" : ""}>Vypnout EPSV (když se zasekává výpis složek)</label>
          <label class="check" data-tls ${c.protocol.startsWith("ftps") ? "" : "hidden"}><input type="checkbox" id="f-insecure" ${c.insecureTLS ? "checked" : ""}>Ignorovat chybu certifikátu</label>
        </div>
        <div class="f c12 test-row">
          <button class="ghost small" id="btn-test">${icon("server")}Otestovat připojení</button>
          <span class="test-msg" id="test-msg">${c.protocol === "sftp" && c.hostKey ? "Důvěryhodný klíč serveru: " + esc(c.hostKey) : ""}</span>
        </div>
      </div>
    </details>

    <details class="sec" id="sec-build" open>
      <summary><div class="sec-h">${icon("chev")}<h2>Build</h2><span class="hint">${cardBuilds(t).length ? `${cardBuilds(t).length} v pořadí` : "nepovinné – bez serveru bude karta jen buildovat"}</span></div></summary>
      <div class="form"><div class="f c12" id="f-builds"></div></div>
    </details>

    <details class="sec" id="sec-light" open>
      <summary><div class="sec-h">${icon("chev")}<h2>Vlastní skript</h2><span class="hint" id="light-hint">${hasLight(t) ? esc(lightName(t)) : "nepovinné – vlastní skript (Python, BAT, PowerShell) místo buildu RAD Studia"}</span></div></summary>
      <div class="form">
        <div class="f c12"><label for="f-lscript">Skript <span class="note">– Python (.py), dávka (.bat, .cmd) nebo PowerShell (.ps1); spustí se ve své složce; když doběhne bez chyby (kód 0), karta nahraje složky na server. Na kartě vypadá jako build: <i>Build + Synchronizovat</i>, <i>Jen build</i>, <i>Jen nahrát</i>.</span></label>
          <div class="path-field"><input id="f-lscript" value="${esc(t.buildLight?.script || "")}" placeholder="C:\\Projekty\\web\\build.py · build.bat" spellcheck="false">
          ${S.canPickDir ? `<button id="f-lscript-pick" title="Vybrat skript">${icon("file")}</button>` : ""}</div></div>
        <div class="f c12"><label for="f-largs">Parametry skriptu <span class="note">– nepovinné, např. <code>--release</code></span></label>
          <input id="f-largs" value="${esc(t.buildLight?.args || "")}" spellcheck="false" placeholder="bez parametrů"></div>
        <div class="f c12"><label for="f-llabel">Popisek tlačítka <span class="note">– nepovinné, např. <i>Build a nahrát na leotech.cz</i> · prázdné = výchozí popisek</span></label>
          <input id="f-llabel" value="${esc(t.buildLight?.label || "")}" maxlength="60" spellcheck="false" placeholder="${esc(mainLabelDefault(t) || "Spustit skript")}"></div>
      </div>
    </details>

    <details class="sec" id="sec-check" open>
      <summary><div class="sec-h">${icon("chev")}<h2>Kontrola před synchronizací</h2><span class="hint">${(t.checklist || []).length ? `${t.checklist.length} ${t.checklist.length === 1 ? "bod" : t.checklist.length < 5 ? "body" : "bodů"}` : "nepovinné"}</span></div></summary>
      <div class="form">
        <div class="f c12"><label for="f-check">Co musíš ověřit, než se nahraje na server – jeden bod na řádek. Před každou synchronizací je budeš muset všechny odškrtnout. Prázdné = synchronizace bez kontroly.</label>
          <textarea id="f-check" spellcheck="false" style="min-height:84px" placeholder="Je v config.h nastaveno #define PRODUCTION?&#10;Je zkompilovaná release verze?">${esc((t.checklist || []).join("\n"))}</textarea></div>
      </div>
    </details>

    <details class="sec" id="sec-ex" open>
      <summary><div class="sec-h">${icon("chev")}<h2>Vyloučit ve všech složkách</h2><span class="hint">${t.excludes.length} pravidel – platí pro všechny dvojice této karty</span></div></summary>
      <div class="form">
        <div class="f c12"><label for="f-ex">Jedno pravidlo na řádek. <code>složka/</code> = složka s tímto názvem kdekoli, <code>*.psd</code> = soubory podle masky, <code>img/raw/</code> = konkrétní cesta. Řádek s # je poznámka.</label>
          <textarea id="f-ex" spellcheck="false">${esc(t.excludes.join("\n"))}</textarea></div>
        <div class="f c12"><div><button class="plain small" id="btn-ex-default">Doplnit výchozí pravidla</button></div></div>
      </div>
    </details>

    <details class="sec" id="sec-backup" open>
      <summary><div class="sec-h">${icon("chev")}<h2>Zálohování</h2><span class="hint">${(t.backups || []).length ? `${(t.backups || []).length} ${(t.backups || []).length === 1 ? "záloha" : (t.backups || []).length < 5 ? "zálohy" : "záloh"}` : "nepovinné – po nastavení přibude tlačítko Zálohovat"}</span></div></summary>
      <p class="note" style="margin:0 0 10px">Složka se zabalí do ZIP v cílové složce jako <code>prefix_RRRR-MM-DD_hh-mm-ss.zip</code>.</p>
      <div class="pairs" id="bpairs"></div>
      <button class="ghost small add-pair" id="btn-add-backup" style="margin-top:10px">${icon("plus")}Přidat zálohu</button>
      <div class="form" style="margin-top:14px">
        <div class="f c12 checks"><label class="check"><input type="checkbox" id="f-bnote" ${t.backupNoNote ? "" : "checked"}>Při zálohování nabídnout poznámku nebo vlastní název ZIP (<code>prefix_poznámka_datum.zip</code> / <code>fw-160.zip</code>) – u každé zálohy zvlášť</label></div>
        <div class="f c12"><label for="f-bex">Vyloučit ve všech zálohách této karty – jedno pravidlo na řádek (stejně jako u synchronizace).</label>
          <textarea id="f-bex" spellcheck="false" style="min-height:90px">${esc((t.backupExcludes || []).join("\n"))}</textarea></div>
      </div>
    </details>

    <details class="sec" id="sec-git" open>
      <summary><div class="sec-h">${icon("chev")}<h2>GitHub</h2><span class="hint">${(t.git || []).length ? `${(t.git || []).length} ${(t.git || []).length === 1 ? "repozitář" : (t.git || []).length < 5 ? "repozitáře" : "repozitářů"}` : "nepovinné – po nastavení přibude tlačítko GitHub"}</span></div></summary>
      ${S.gitFound ? "" : `<p class="bwarn">Na tomto PC není nainstalovaný Git (Git for Windows) – bez něj tlačítko GitHub nefunguje.</p>`}
      <p class="note" style="margin:0 0 10px">Vyber složky s git repozitáři. Kam se posílá, si každý repozitář pamatuje sám (origin). Tlačítko GitHub je projde v tomto pořadí.</p>
      <div class="pairs" id="gitrepos"></div>
      <button class="ghost small add-pair" id="btn-add-git" style="margin-top:10px">${icon("plus")}Přidat repozitář</button>
      <div class="form" style="margin-top:12px"><div class="f c12 checks">
        <label class="check"><input type="checkbox" id="f-gitpull" ${t.gitPull ? "checked" : ""}>Před pushem stáhnout změny ze serveru (pull --rebase) – když commituješ i z jiného PC</label>
      </div></div>
    </details>

    <details class="sec" id="sec-fw" open>
      <summary><div class="sec-h">${icon("chev")}<h2>FW build a monitor (ESP32)</h2><span class="hint">${hasFw(t) ? `${t.fw.variants.length} ${t.fw.variants.length === 1 ? "varianta" : t.fw.variants.length < 5 ? "varianty" : "variant"}${hasMon(t) ? " · monitor" : ""}` : hasMon(t) ? "monitor" : "nepovinné – po nastavení přibude tlačítko FW build / Monitor"}</span></div></summary>
      <div class="form" id="fwbox"></div>
    </details>
  </div>`;

  renderPairs();
  bindMain(t);
  updateSyncButton();
  showPathEnds();
}

function lastBuildText(t) {
  const l = t.lastBuild;
  if (!l) return "";
  const res = { ok: "OK", failed: "selhal", cancelled: "přerušen" }[l.result] || l.result;
  return `${scriptOnly(t) ? "poslední skript" : "poslední build"} ${fmtWhen(l.at)} · ${res}${l.result !== "ok" && l.message ? ": " + esc(l.message) : ""}`;
}

function lastText(t) {
  const l = t.lastSync;
  if (!l) return "Zatím nesynchronizováno";
  const res = { ok: "", errors: " · s chybami", cancelled: " · přerušeno", failed: " · nepovedlo se" }[l.result] ?? "";
  if (l.result === "failed") return `Naposledy ${fmtWhen(l.at)}${res}: ${esc(l.message || "")}`;
  return `Naposledy ${fmtWhen(l.at)} · ${l.uploaded ? fmtNum(l.uploaded) + " souborů, " + fmtBytes(l.bytes) : "vše aktuální"}${res}`;
}

function renderPairs() {
  const t = cur();
  const wrap = $("#pairs");
  if (!t.pairs.length) {
    wrap.innerHTML = `<p class="note">Žádná dvojice – karta nenahrává na server. Přidej dvojici, jen pokud má synchronizovat.</p>`;
    return;
  }
  wrap.innerHTML = t.pairs.map((p, i) => `
    <div class="pair${p.enabled ? "" : " off"}" data-i="${i}">
      <div class="pair-main">
        <input type="checkbox" class="toggle" data-k="enabled" ${p.enabled ? "checked" : ""} title="Synchronizovat tuto dvojici">
        <div class="path-field${p.local && S.localOK[p.local] === false ? " missing" : ""}" data-local-field>
          <input data-k="local" value="${esc(p.local)}" placeholder="C:\\Projekty\\web\\www" spellcheck="false" aria-label="Složka v PC">
          ${S.canPickDir ? `<button data-pick="local" title="Vybrat složku v PC">${icon("folder")}</button>` : ""}
        </div>
        <span class="flow">${icon("arrow")}</span>
        <div class="path-field">
          <input data-k="remote" value="${esc(p.remote)}" placeholder="/www" spellcheck="false" aria-label="Složka na serveru">
          <button data-pick="remote" title="Procházet server">${icon("server")}</button>
        </div>
        <span class="pair-acts"><button class="icon-btn small" data-pair-menu title="Další volby">${icon("dots")}</button><button class="icon-btn small" data-pair-del title="Odebrat dvojici">${icon("x")}</button></span>
      </div>
      <div class="pair-ex">
        <span class="lbl">Vyloučit:</span>
        ${(p.excludes || []).map((x, k) => `<span class="chip">${esc(x)}<button data-ex-del="${k}" title="Zrušit vyloučení">${icon("x")}</button></span>`).join("")}
        <span class="chip-add">
          ${S.canPickDir ? `<button data-ex-pick="dir" title="Vybrat složku uvnitř zdrojové složky">${icon("folder")}složku</button>
          <button data-ex-pick="file" title="Vybrat soubor uvnitř zdrojové složky">${icon("file")}soubor</button>` : ""}
          <button data-ex-type title="Napsat cestu nebo masku (např. *.psd)">${icon("plus")}vzor</button>
        </span>
      </div>
    </div>`).join("");
  requestAnimationFrame(showPathEnds);

  wrap.querySelectorAll(".pair").forEach((el) => {
    const p = t.pairs[+el.dataset.i];
    el.querySelectorAll("[data-k]").forEach((inp) => {
      const k = inp.dataset.k;
      if (k === "enabled") inp.onchange = () => { p.enabled = inp.checked; el.classList.toggle("off", !p.enabled); scheduleSave(t, 0); };
      else {
        inp.oninput = () => { p[k] = inp.value; scheduleSave(t); };
        if (k === "local") inp.onblur = () => checkLocal();
      }
    });
    el.querySelector('[data-pick="local"]')?.addEventListener("click", async () => {
      try {
        const r = await api("POST", "/api/pickdir", { title: tr("Vyber složku v PC"), start: p.local });
        if (r.cancelled) return;
        p.local = r.path; el.querySelector('[data-k="local"]').value = r.path;
        scheduleSave(t, 0); checkLocal();
      } catch (e) { toast(e.message, true); }
    });
    el.querySelector('[data-pick="remote"]').onclick = () => browseRemote(t, p, (path) => {
      p.remote = path; el.querySelector('[data-k="remote"]').value = path; scheduleSave(t, 0);
    });
    // stopPropagation: the global "click outside closes the menu" handler would close it right away
    el.querySelector("[data-pair-menu]").onclick = (e) => { e.stopPropagation(); pairMenu(t, +el.dataset.i, e.clientX, e.clientY); };
    el.querySelector("[data-pair-del]").onclick = () => { t.pairs.splice(+el.dataset.i, 1); renderPairs(); scheduleSave(t, 0); checkLocal(); };
    el.querySelectorAll("[data-ex-del]").forEach((b) => b.onclick = () => {
      p.excludes.splice(+b.dataset.exDel, 1); renderPairs(); scheduleSave(t, 0);
    });
    el.querySelectorAll("[data-ex-pick]").forEach((b) => b.onclick = async () => {
      if (!p.local) return toast("Nejdřív vyber složku v PC", true);
      try {
        const r = await api("POST", "/api/pickexclude", { local: p.local, kind: b.dataset.exPick });
        if (r.cancelled) return;
        addPairExclude(t, p, r.rel);
      } catch (e) { toast(e.message, true); }
    });
    el.querySelector("[data-ex-type]").onclick = (e) => {
      const holder = e.currentTarget.parentElement;
      const inp = document.createElement("input");
      inp.className = "chip-input"; inp.placeholder = "img/raw/ nebo *.psd";
      holder.before(inp); inp.focus();
      let fin = false;
      const done = (ok) => { if (fin) return; fin = true; const v = inp.value.trim(); inp.remove(); if (ok && v) addPairExclude(t, p, v); };
      inp.onkeydown = (ev) => { if (ev.key === "Enter") done(true); if (ev.key === "Escape") done(false); };
      inp.onblur = () => afterPointer(() => done(true));
    };
  });
}

// long paths: show the end (the interesting part) when the field is not being edited
function showPathEnds() {
  document.querySelectorAll(".path-field input").forEach((inp) => {
    inp.scrollLeft = inp.scrollWidth;
    inp.addEventListener("blur", () => (inp.scrollLeft = inp.scrollWidth));
  });
}

function addPairExclude(t, p, v) {
  v = v.replace(/\\/g, "/").replace(/^\.\//, "");
  p.excludes = p.excludes || [];
  if (!p.excludes.includes(v)) p.excludes.push(v);
  renderPairs(); scheduleSave(t, 0);
}

function pairMenu(t, i, x, y) {
  const p = t.pairs[i];
  const m = $("#ctx");
  m.innerHTML = `
    ${p.local ? `<button data-a="open">${icon("folder")}Otevřít složku v PC</button>` : ""}
    ${i > 0 ? `<button data-a="up">Posunout nahoru</button>` : ""}
    <button data-a="dup">Duplikovat dvojici</button>
    <hr><button data-a="del" class="del">Odebrat dvojici</button>`;
  m.hidden = false;
  m.style.left = Math.min(x, innerWidth - 210) + "px";
  m.style.top = Math.min(y, innerHeight - 180) + "px";
  m.onclick = async (e) => {
    const b = e.target.closest("button");
    if (!b) return;
    m.hidden = true;
    const a = b.dataset.a;
    if (a === "open") api("POST", "/api/openfolder", { path: p.local }).catch((err) => toast(err.message, true));
    if (a === "up") { t.pairs.splice(i - 1, 0, t.pairs.splice(i, 1)[0]); }
    if (a === "dup") t.pairs.splice(i + 1, 0, { ...JSON.parse(JSON.stringify(p)), id: "" });
    if (a === "del") t.pairs.splice(i, 1);
    if (a !== "open") { renderPairs(); scheduleSave(t, 0); }
  };
}

function bindMain(t) {
  const c = t.conn;
  $("#title").oninput = (e) => { t.name = e.target.value; renderTabsName(t); scheduleSave(t); };
  $("#f-dropbox").onchange = (e) => { t.dropbox = e.target.value; scheduleSave(t, 0); };
  // card colour right at the top of the edit form (also in the tab's right-click menu)
  $("#f-colors").onclick = (e) => {
    const b = e.target.closest("button[data-c]");
    if (!b || b.dataset.c === t.color) return;
    t.color = b.dataset.c;
    $("#f-colors").querySelectorAll("button").forEach((x) => { const on = x === b; x.setAttribute("aria-pressed", on); x.setAttribute("aria-checked", on); });
    renderTabs(); scheduleSave(t, 0);
  };
  $("#title").onkeydown = (e) => { if (e.key === "Enter") e.target.blur(); };
  $("#btn-add-pair").onclick = () => { t.pairs.push({ id: "", local: "", remote: "", enabled: true, excludes: [] }); renderPairs(); scheduleSave(t, 0); };

  const bindVal = (sel, fn) => { const el = $(sel); el.oninput = el.onchange = () => { fn(el); scheduleSave(t); }; };
  bindVal("#f-proto", (el) => {
    c.protocol = el.value;
    $("#f-port").placeholder = DEFPORT[c.protocol];
    $("#row-key").hidden = c.protocol !== "sftp";
    document.querySelector("[data-ftp]").hidden = c.protocol === "sftp";
    document.querySelector("[data-tls]").hidden = !c.protocol.startsWith("ftps");
  });
  bindVal("#f-host", (el) => (c.host = el.value.trim()));
  bindVal("#f-port", (el) => (c.port = parseInt(el.value, 10) || 0));
  bindVal("#f-user", (el) => (c.user = el.value));
  bindVal("#f-pass", (el) => (c.passNew = el.value));
  bindVal("#f-key", (el) => (c.keyFile = el.value));
  bindVal("#f-safe", (el) => (t.safeUpload = el.checked));
  bindVal("#f-epsv", (el) => (c.disableEPSV = el.checked));
  bindVal("#f-insecure", (el) => (c.insecureTLS = el.checked));
  drawCardBuilds(t);
  renderBackupPairs(t);
  $("#btn-add-backup").onclick = () => {
    t.backups = [...(t.backups || []), { id: "", source: "", target: "", prefix: "", enabled: true, excludes: [] }];
    renderBackupPairs(t); scheduleSave(t, 0);
  };
  renderGitRepos(t);
  renderFwEdit(t);
  bindLight(t);
  $("#btn-add-git").onclick = () => { t.git = [...(t.git || []), { id: "", path: "", enabled: true }]; renderGitRepos(t); scheduleSave(t, 0); };
  $("#f-gitpull").onchange = (e) => { t.gitPull = e.target.checked; scheduleSave(t, 0); };
  $("#f-bnote").onchange = (e) => { t.backupNoNote = !e.target.checked; scheduleSave(t, 0); };
  $("#f-bex").oninput = (e) => { t.backupExcludes = e.target.value.split("\n").map((x) => x.trim()).filter(Boolean); scheduleSave(t, 800); };
  $("#f-check").oninput = (e) => { t.checklist = e.target.value.split("\n").map((s) => s.trim()).filter(Boolean); scheduleSave(t, 800); };
  $("#f-ex").oninput = (e) => { t.excludes = e.target.value.split("\n").map((s) => s.trim()).filter(Boolean); scheduleSave(t, 800); };
  $("#btn-ex-default").onclick = () => {
    const set = new Set(t.excludes);
    S.defaultExcludes.forEach((x) => set.add(x));
    t.excludes = [...set];
    $("#f-ex").value = t.excludes.join("\n");
    scheduleSave(t, 0);
  };
  $("#pick-key").onclick = async () => {
    try {
      const r = await api("POST", "/api/pickfile", { title: tr("Vyber privátní klíč"), start: "" });
      if (!r.cancelled) { c.keyFile = r.path; $("#f-key").value = r.path; scheduleSave(t, 0); }
    } catch (e) { toast(e.message, true); }
  };
  $("#btn-test").onclick = () => testConn(t);
  $("#btn-done").onclick = () => commitEdit(t);
  $("#btn-cancel").onclick = () => {
    if (!S.dirty.has(t.id)) { leaveEdit(t); return render(); }
    modal(`
      <div class="modal-h"><h3>Zavřít bez uložení?</h3><p>Změny karty „${esc(S.snap[t.id]?.name || t.name)}“ od otevření úprav se zahodí.</p></div>
      <div class="modal-f"><button class="ghost" data-close>Zpět k úpravám</button><button class="danger" id="m-drop">Zahodit změny</button></div>`,
      (box, close) => { $("#m-drop", box).onclick = () => { close(); revertEdit(t); render(); checkLocal(); }; });
  };
  $("#btn-dup").onclick = () => duplicateTab(t);
  $("#btn-del").onclick = () => deleteTab(t);
  $("#f-group").onchange = (e) => { t.group = e.target.value; renderTabs(); scheduleSave(t, 0); };
  $("#btn-groups").onclick = () => openSettings(true);
}

/* ------------------------------------------------------------------ overview (tiles) */

let bgTimer = null;
function bgKick() { if (!bgTimer) bgTimer = setTimeout(bgPoll, 250); }

// polls every running job that is not shown in the detail panel
async function bgPoll() {
  bgTimer = null;
  const ids = Object.keys(S.jobs).filter((id) => running(id) && !(S.view === "card" && id === S.jobTab));
  if (!ids.length) return;
  await Promise.all(ids.map(async (id) => {
    let j;
    try { j = await api("GET", `/api/tabs/${id}/job?log=999999999`); } catch { return; }
    if (S.view === "card" && id === S.jobTab) return; // the detail panel took over
    if (j.none) { delete S.jobs[id]; return; }
    const was = running(id);
    S.jobs[id] = j.phase;
    S.jobData[id] = j;
    if (was && j.phase === "done") { renderTabs(); jobFinished(id, j, true); }
    else updateTile(id);
  }));
  bgTimer = setTimeout(bgPoll, 500);
}

// collapsed groups in the overview – remembered per PC (uiSet)
function collapsedGroups() {
  const v = uiGet("collapsed", []);
  return new Set(Array.isArray(v) ? v : []);
}
function setCollapsed(set) { uiSet("collapsed", [...set]); }

function renderOverview() {
  const n = S.tabs.length;
  const gs = groups();
  const col = collapsedGroups();
  const tilesOf = (gid) => orderedTabs().filter((t) => groupOf(t) === gid);
  const tile = (t) => `<article class="tile" draggable="true" data-id="${t.id}" style="--c:${esc(t.color)}">${tileInner(t)}</article>`;
  const sections = [...gs.map((g) => ({ id: g.id, name: g.name })), { id: "", name: gs.length ? "Bez skupiny" : "" }]
    .filter((g) => g.id !== "" || tilesOf("").length || !gs.length);
  const count = (k) => `${k} ${k === 1 ? "projekt" : k > 1 && k < 5 ? "projekty" : "projektů"}`;
  $("#main").innerHTML = `
  <div class="page ovp">
    <div class="ov-head">
      <h1 class="title">Přehled</h1>
      <span class="hint">${count(n)}${gs.length ? ` · ${gs.length} ${gs.length === 1 ? "skupina" : gs.length < 5 ? "skupiny" : "skupin"}` : ""}</span>
      <span class="spacer"></span>
      <button class="plain small" id="ov-groups">${icon("sliders")}Skupiny</button>
      <button class="ghost small" id="ov-add">${icon("plus")}Nová karta</button>
    </div>
    ${sections.map((g) => {
      const list = tilesOf(g.id);
      const closed = col.has(g.id || "_none");
      return `
      <section class="grp${closed ? " closed" : ""}" data-group="${g.id}">
        ${g.name ? `<div class="grp-h">
          <button class="grp-toggle" data-toggle aria-expanded="${!closed}">${icon("chev")}<span translate="no">${esc(g.name)}</span></button>
          <span class="hint">${count(list.length)}</span>
          ${g.id ? `<button class="plain small grp-add" data-add title="Nová karta v této skupině">${icon("plus")}</button>` : ""}
        </div>` : ""}
        <div class="tiles" data-group="${g.id}">${list.map(tile).join("") || `<div class="drop-hint">Přetáhni sem dlaždici</div>`}</div>
      </section>`;
    }).join("")}
  </div>`;
  $("#ov-add").onclick = () => addTab("");
  $("#ov-groups").onclick = () => openSettings(true);
  const page = $("#main .ovp");
  page.onclick = (e) => {
    const sec = e.target.closest(".grp");
    if (e.target.closest("[data-toggle]")) {
      const key = sec.dataset.group || "_none";
      const set = collapsedGroups();
      set.has(key) ? set.delete(key) : set.add(key);
      setCollapsed(set);
      sec.classList.toggle("closed");
      e.target.closest("[data-toggle]").setAttribute("aria-expanded", !sec.classList.contains("closed"));
      return;
    }
    if (e.target.closest("[data-add]")) return addTab(sec.dataset.group);
    const b = e.target.closest("[data-act]");
    if (!b) return;
    const t = S.tabs.find((x) => x.id === b.closest(".tile").dataset.id);
    if (!t) return;
    const a = b.dataset.act;
    if (a === "open") select(t.id);
    if (a === "check" && !running(t.id)) runPreview(t);
    if (a === "sync" && !running(t.id)) confirmSync(t);
    if (a === "stop") { api("POST", `/api/tabs/${t.id}/cancel`).catch(() => {}); toast("Zastavuji…"); }
    if (a === "list" && S.checked[t.id]) showPreview(t, S.checked[t.id]);
    if (a === "onlybuild" && !running(t.id)) confirmSync(t, "build");
    if (a === "onlysync" && !running(t.id)) confirmSync(t, "sync");
    if (a === "buildwin") openBuildWindow(t.id);
    if (a === "backup" && !running(t.id)) confirmBackup(t);
    if (a === "git") openGitWindow(t);
    if (a === "fw") openFwWindow(t);
    if (a === "mon") openFwWindow(t, true);
    if (a === "fwwin") openFwWindow(t, !hasFw(t));
  };

  // drag & drop of tiles between / within groups
  let dragId = null;
  const clearMarks = () => page.querySelectorAll(".drop-before, .drop-after, .drop-into").forEach((x) => x.classList.remove("drop-before", "drop-after", "drop-into"));
  page.querySelectorAll(".tile").forEach((el) => {
    el.oncontextmenu = (e) => { e.preventDefault(); tabMenu(el.dataset.id, e.clientX, e.clientY); };
    el.ondragstart = (e) => { dragId = el.dataset.id; e.dataTransfer.setData("text/plain", dragId); e.dataTransfer.effectAllowed = "move"; requestAnimationFrame(() => el.classList.add("dragging")); };
    el.ondragend = () => { el.classList.remove("dragging"); clearMarks(); dragId = null; };
  });
  page.querySelectorAll(".grp").forEach((sec) => {
    sec.ondragover = (e) => {
      if (!dragId) return;
      e.preventDefault();
      clearMarks();
      const over = e.target.closest(".tile");
      if (over && over.dataset.id !== dragId) {
        const r = over.getBoundingClientRect();
        over.classList.add(e.clientX < r.left + r.width / 2 ? "drop-before" : "drop-after");
      } else if (!over) sec.classList.add("drop-into");
    };
    sec.ondragleave = (e) => { if (!sec.contains(e.relatedTarget)) clearMarks(); };
    sec.ondrop = (e) => {
      e.preventDefault();
      const id = dragId || e.dataTransfer.getData("text/plain");
      const over = e.target.closest(".tile");
      const group = sec.dataset.group;
      clearMarks();
      if (!id) return;
      if (over && over.dataset.id !== id) {
        const r = over.getBoundingClientRect();
        let before = over.dataset.id;
        if (e.clientX >= r.left + r.width / 2) {
          let sib = over.nextElementSibling;
          while (sib && (!sib.classList.contains("tile") || sib.dataset.id === id)) sib = sib.nextElementSibling;
          before = sib ? sib.dataset.id : null;
        }
        moveCard(id, group, before);
      } else if (!over) moveCard(id, group, null);
    };
  });
}

function cardKinds(t) {
  const k = [];
  if (hasBuilds(t)) k.push(scriptOnly(t) ? "Skript" : "Build");
  if (hasBackups(t)) k.push(activeBackups(t).length === 1 ? "Záloha" : `${activeBackups(t).length} zálohy`);
  if (hasGit(t)) k.push(activeGit(t).length === 1 ? "GitHub (1 repozitář)" : `GitHub (${activeGit(t).length} repozitáře)`);
  if (hasFw(t)) k.push(`FW build (${t.fw.variants.map((v) => v.name).join(", ")})`);
  if (hasMon(t)) k.push("Monitor");
  return k.join(" · ") || "Karta není nastavená";
}

function tileInner(t) {
  const c = t.conn;
  const on = t.pairs.filter((p) => p.enabled && p.local);
  const targets = on.map((p) => esc(p.remote || "/")).join(", ");
  const ftp = syncReady(t), mm = mainMode(t);
  const xb = (tiny) => `${hasBackups(t) ? `<button class="backup-btn${tiny ? " tiny" : ""}" data-act="backup" title="${esc(lastBackupText(t))}"${running(t.id) ? " disabled" : ""}>${icon("archive")}Zálohovat</button>` : ""}${hasGit(t) ? `<button class="git-btn${tiny ? " tiny" : ""}" data-act="git" title="${esc(lastGitText(t))}">${icon("branch")}GitHub</button>` : ""}${hasFw(t) ? `<button class="fw-btn${tiny ? " tiny" : ""}${S.fwRunning?.[t.id] && S.fwRunning[t.id] !== "monitor" ? " live" : ""}" data-act="fw" title="FW build / nahrání (ESP-IDF)">${icon("chip")}FW</button>` : ""}${hasMon(t) ? `<button class="mon-btn${tiny ? " tiny" : ""}${S.fwRunning?.[t.id] === "monitor" ? " live" : ""}" data-act="mon" title="Monitor – sériová konzole zařízení">${icon("log")}Monitor</button>` : ""}`;
  if (!ftp) return `
    <header>
      <span class="dot"></span>
      <h2><button data-act="open" title="Otevřít kartu"><span translate="no">${esc(t.name)}</span></button></h2>
      <button class="icon-btn small" data-act="open" title="Otevřít kartu">${icon("chev")}</button>
    </header>
    <p class="srv">${esc(cardKinds(t))}</p>
    ${syncSet(t) ? `<p class="last err">Synchronizace nedokončená – ${t.conn.host ? "chybí složka" : "chybí server"}</p>` : ""}
    ${hasBuilds(t) ? `<p class="tbuild">${icon("gear")}${scriptOnly(t) ? "Skript" : "Build"}: ${buildNames(t).map(esc).join(" → ")}</p>` : ""}
    <div class="st">${tileStatus(t)}</div>
    <div class="tile-actions">
      ${mm ? tileSyncButton(t) : ""}
      ${cardUnset(t) ? `<button class="ghost" data-act="open">${icon("gear")}Nastavit kartu</button>` : !mm ? xb(false) : ""}
    </div>
    ${mm && (hasBackups(t) || hasGit(t) || hasFw(t) || hasMon(t)) ? `<div class="tile-more">${xb(true)}</div>` : ""}`;
  return `
    <header>
      <span class="dot"></span>
      <h2><button data-act="open" title="Otevřít kartu"><span translate="no">${esc(t.name)}</span></button></h2>
      <button class="icon-btn small" data-act="open" title="Otevřít kartu">${icon("chev")}</button>
    </header>
    <p class="srv">${c.host ? `${esc(c.user ? c.user + "@" : "")}${esc(c.host)} · ${PROTO[c.protocol]}` : "server není nastavený"}</p>
    <p class="tg">${on.length ? `${on.length} ${on.length === 1 ? "složka" : on.length < 5 ? "složky" : "složek"} → <span class="mono">${targets}</span>` : "žádná zapnutá složka"}</p>
    ${(t.checklist || []).length ? `<p class="tcheck">${icon("lock")}${t.checklist.length} ${t.checklist.length === 1 ? "kontrola" : t.checklist.length < 5 ? "kontroly" : "kontrol"} před synchronizací</p>` : ""}
    ${hasBuilds(t) ? `<p class="tbuild">${icon("gear")}${scriptOnly(t) ? "Skript" : "Build"}: ${buildNames(t).map(esc).join(" → ")}</p>` : ""}
    <div class="st">${tileStatus(t)}</div>
    <div class="tile-actions">
      <button class="ghost" data-act="check"${running(t.id) ? " disabled" : ""}>Zkontrolovat</button>
      ${tileSyncButton(t)}
    </div>
    ${hasBuilds(t) || hasBackups(t) || hasGit(t) || hasFw(t) || hasMon(t) ? `<div class="tile-more">
      ${xb(true)}${hasBackups(t) || hasGit(t) || hasFw(t) || hasMon(t) ? `<span class="spacer"></span>` : ""}
      ${hasBuilds(t) ? `<button class="link" data-act="onlybuild">${scriptOnly(t) ? "Jen skript" : "Jen build"}</button><button class="link" data-act="onlysync">Jen nahrát</button>` : ""}</div>` : ""}`;
}

function tileSyncButton(t) {
  const j = S.jobData[t.id];
  const busy = running(t.id);
  let pct = 0, label = mainLabel(t), indet = false;
  if (busy) {
    if (j?.backup) { indet = true; label = "Záloha…"; }
    else if (j?.phase === "building") { indet = true; const k = (j.steps || []).findIndex((x) => x.state === "running"); label = scriptOnly(t) ? "Skript běží…" : `Build ${k + 1} / ${(j.steps || []).length}`; }
    else if (!j || j.phase !== "uploading" || j.preview) { indet = true; label = j?.preview ? "Kontroluji…" : j?.phase === "scanning" ? "Porovnávám…" : "Připojuji…"; }
    else {
      pct = j.bytesTotal > 0 ? Math.min(100, (j.bytesDone / j.bytesTotal) * 100) : (j.filesTotal ? (j.filesDone / j.filesTotal) * 100 : 0);
      label = `${Math.floor(pct)} %`;
    }
  }
  return `<button class="sync sm${busy ? " busy" : ""}${indet ? " indet" : ""}" data-act="sync"><span class="fill" style="width:${pct.toFixed(1)}%"></span><span class="lbl">${busy ? "" : icon(mainMode(t) === "build" ? "gear" : "up")}${label}</span></button>`;
}

function tileStatus(t) {
  const fwPh = S.fwRunning?.[t.id];
  if (fwPh) return `<p class="live">${fwPh === "monitor" ? "Monitor běží" : "FW build běží…"} <button class="link" data-act="fwwin">Otevřít</button></p>`;
  const j = S.jobData[t.id];
  let last = "";
  const lb = t.lastBuild;
  if (syncReady(t)) {
    last = `<p class="last${t.lastSync && t.lastSync.result !== "ok" ? " err" : ""}">${lastText(t)}</p>`;
    if (lb && lb.result !== "ok" && (!t.lastSync || new Date(lb.at) >= new Date(t.lastSync.at))) last += `<p class="last err">${lastBuildText(t)}</p>`;
  } else {
    if (hasBuilds(t)) last += `<p class="last${lb && lb.result !== "ok" ? " err" : ""}">${lb ? lastBuildText(t).replace(/^p/, "P") : scriptOnly(t) ? "Skript zatím neběžel" : "Zatím nebuildováno"}</p>`;
    if (hasBackups(t)) last += `<p class="last${t.lastBackup && t.lastBackup.result !== "ok" ? " err" : ""}">${lastBackupText(t)}</p>`;
    if (hasGit(t)) last += `<p class="last${t.lastGit && t.lastGit.result !== "ok" ? " err" : ""}">${lastGitText(t)}</p>`;
  }
  if (running(t.id)) {
    let txt = "Připojuji…";
    if (j?.backup) {
      const pct = j.bytesTotal > 0 ? Math.floor((j.bytesDone / j.bytesTotal) * 100) : 0;
      const txt = j.phase === "zipping" ? `Zálohuji… ${pct} % · ${fmtNum(j.filesDone)} / ${fmtNum(j.filesTotal)} souborů` : `Zálohuji – prohledávám… ${fmtNum(j.scanFiles)}`;
      return `${last}<p class="live">${txt} <button class="link" data-act="buildwin">Průběh</button> <button class="link" data-act="stop">Zastavit</button></p>`;
    }
    if (j?.phase === "building" || S.jobs[t.id] === "building") {
      const st = (j?.steps || []).find((x) => x.state === "running");
      return `${last}<p class="live">${st ? esc(st.name) + "…" : "Build…"} <button class="link" data-act="buildwin">Průběh</button> <button class="link" data-act="stop">Zastavit</button></p>`;
    }
    if (j?.phase === "scanning") txt = `Porovnávám… ${fmtNum(j.scanFiles)} souborů`;
    if (j?.phase === "uploading" && !j.preview) {
      txt = `${fmtNum(j.filesDone)} / ${fmtNum(j.filesTotal)} souborů · ${fmtBytes(j.bytesDone)} / ${fmtBytes(j.bytesTotal)}`;
      if (j.speed > 0) txt += ` · ${fmtBytes(Math.round(j.speed))}/s`;
    }
    return `${last}<p class="live">${txt} <button class="link" data-act="stop">Zastavit</button></p>`;
  }
  const ck = S.checked[t.id];
  if (ck) {
    const files = (ck.plans || []).reduce((a, p) => a + (p.files?.length || 0), 0);
    const bytes = (ck.plans || []).reduce((a, p) => a + (p.files || []).reduce((b, f) => b + f.size, 0), 0);
    const dirs = (ck.plans || []).reduce((a, p) => a + (p.dirs?.length || 0), 0);
    const when = new Date(ck.ended).toLocaleTimeString(I18N.locale(), { hour: "2-digit", minute: "2-digit" });
    if (ck.result === "failed") return `${last}<p class="ck err">Kontrola ${when}: ${esc(ck.log?.at(-1)?.text || "nepovedla se")}</p>`;
    if (!files && !dirs) return `${last}<p class="ck ok">Kontrola ${when}: vše aktuální</p>`;
    return `${last}<p class="ck todo">Kontrola ${when}: k nahrání ${fmtNum(files)} ${files === 1 ? "soubor" : files < 5 && files ? "soubory" : "souborů"} · ${fmtBytes(bytes)} <button class="link" data-act="list">Seznam</button></p>`;
  }
  return last;
}

function updateTile(id) {
  if (S.view !== "overview") return;
  const el = document.querySelector(`.tile[data-id="${id}"]`);
  const t = S.tabs.find((x) => x.id === id);
  if (!el || !t) return;
  el.querySelector(".st").innerHTML = tileStatus(t);
  const ck = el.querySelector('[data-act="check"]'); if (ck) ck.disabled = running(id);
  el.querySelectorAll(".tile-more button, [data-act=backup]").forEach((b) => { if (b.dataset.act !== "git") b.disabled = running(id); });
  const tb = el.querySelector('[data-act="backup"]'); if (tb) tb.title = lastBackupText(t);
  const tg = el.querySelector('[data-act="git"]'); if (tg) tg.title = lastGitText(t);
  const old = el.querySelector('[data-act="sync"]');
  if (!old) return;
  const tmp = document.createElement("div");
  tmp.innerHTML = tileSyncButton(t);
  const nb = tmp.firstElementChild;
  old.className = nb.className;
  old.querySelector(".fill").style.width = nb.querySelector(".fill").style.width;
  old.querySelector(".lbl").innerHTML = nb.querySelector(".lbl").innerHTML;
}

/* ------------------------------------------------------------------ builds: card selection */

function drawCardBuilds(t) {
  const box = $("#f-builds");
  if (!box) return;
  const all = builds();
  const sel = t.builds || [];
  if (!all.length) {
    box.innerHTML = `<p class="note">Zatím nemáš žádný build. Založíš ho v Nastavení aplikace → Buildy (jednou, pak ho vybíráš v kartách).</p>
      <div><button class="ghost small" id="cb-manage">${icon("sliders")}Založit build</button></div>`;
  } else {
    box.innerHTML = `<label>Před synchronizací se pustí tyto buildy v tomto pořadí. Když některý selže, nic se nenahraje.</label>
      <ol class="cblist">${sel.map((id, i) => {
        const b = all.find((x) => x.id === id);
        return b ? `<li data-i="${i}"><span class="bn" translate="no">${esc(b.name)}</span><span class="note">${esc(b.config)} · ${b.defines.map(esc).join(", ") || "bez přepínačů"}</span>
          <button class="icon-btn small" data-up title="Dřív" ${i === 0 ? "disabled" : ""}>${icon("up")}</button>
          <button class="icon-btn small" data-del title="Odebrat z karty">${icon("x")}</button></li>` : "";
      }).join("") || `<li class="note">Žádný build – synchronizace nahraje složky tak, jak jsou.</li>`}</ol>
      <div class="cbadd">
        <select id="cb-add"><option value="">+ Přidat build…</option>${all.filter((b) => !sel.includes(b.id)).map((b) => `<option value="${b.id}">${esc(b.name)} (${esc(b.config)})</option>`).join("")}</select>
        <button class="plain small" id="cb-manage">Spravovat buildy</button>
      </div>`;
    box.querySelectorAll("li[data-i]").forEach((li) => {
      const i = +li.dataset.i;
      li.querySelector("[data-up]").onclick = () => { const a = [...t.builds]; a.splice(i - 1, 0, a.splice(i, 1)[0]); t.builds = a; drawCardBuilds(t); scheduleSave(t, 0); };
      li.querySelector("[data-del]").onclick = () => { t.builds = t.builds.filter((_, k) => k !== i); drawCardBuilds(t); scheduleSave(t, 0); };
    });
    $("#cb-add").onchange = (e) => { if (!e.target.value) return; t.builds = [...(t.builds || []), e.target.value]; drawCardBuilds(t); scheduleSave(t, 0); };
  }
  $("#cb-manage").onclick = () => openSettings("builds");
}

/* ------------------------------------------------------------------ builds: editor (app settings) */

function openBuildEditor(src) {
  const isNew = !src;
  const b = src ? JSON.parse(JSON.stringify(src)) : {
    id: "", name: "", project: builds()[0]?.project || "", config: "", platform: "Win32", incFile: "",
    switches: [], defines: [], crlf: true, crlfMask: "*.pas;*.dpr;*.inc;*.dfm", postBuild: true, extraCmd: "", extraArgs: "",
  };
  let insp = null;
  modal(`
    <div class="modal-h"><h3>${isNew ? "Nový build" : "Build „" + esc(b.name) + "“"}</h3>
      <p>Zkompiluje projekt TMS WEB Core příkazovým kompilátorem s přepínači, které tu zaškrtneš.</p></div>
    <div class="modal-b set-grid">
      <div class="f"><label for="be-name">Název (zobrazí se v kartě)</label><input id="be-name" value="${esc(b.name)}" placeholder="např. Web – produkce" maxlength="60"></div>
      <div class="f"><label for="be-info">Info k verzi <span class="note">– nepovinné, 1 řádek, max 80 znaků. Post-build a příkaz po buildu ho dostanou v proměnné <code>LEONZILLA_VERSION_INFO</code> (např. pro zápis do version.json), případně i pod názvem z Nastavení aplikace → Buildy; prázdné = bez info.</span></label>
        <input id="be-info" value="${esc(buildInfo(b))}" maxlength="80" spellcheck="false" placeholder="prázdné = bez info"></div>
      <div class="f"><label for="be-proj">Projekt (.dproj)</label>
        <div class="path-field"><input id="be-proj" value="${esc(b.project)}" placeholder="C:\\Projekty\\MojeApp\\MojeApp.dproj" spellcheck="false">
        <button id="be-proj-pick" title="Vybrat projekt">${icon("file")}</button></div></div>
      <div class="f"><label for="be-cfg">Konfigurace</label><select id="be-cfg"><option value="">– načítám –</option></select>
        <span class="note" id="be-cfgnote"></span></div>
      <div class="f"><span class="lab">Přepínače z Predefines.inc (horní část v bloku {$IFNDEF LEONZILLA})</span>
        <div id="be-incnote"></div>
        <ul class="swlist" id="be-sw"></ul>
        <div class="row-btns"><button class="ghost small" id="be-reload">Načíst přepínače z Predefines.inc</button><span class="note" id="be-reloadnote"></span></div>
        <span class="note" id="be-defs"></span></div>
      <div class="f"><label class="check"><input type="checkbox" id="be-crlf" ${b.crlf ? "checked" : ""}>Před buildem převést konce řádků na CRLF (soubory ve složce projektu)</label>
        <input id="be-mask" value="${esc(b.crlfMask)}" spellcheck="false" style="max-width:320px" aria-label="Které soubory"></div>
      <div class="f"><label class="check"><input type="checkbox" id="be-post" ${b.postBuild ? "checked" : ""}>Po kompilaci spustit Post-build z projektu</label>
        <code class="cmdline" id="be-postcmd">–</code></div>
      <div class="f"><label for="be-extra">Příkaz po buildu (nepovinné, spustí se v cmd ve složce projektu)</label><input id="be-extra" value="${esc(b.extraCmd)}" spellcheck="false"></div>
      <div class="f"><label for="be-args">Další parametry kompilátoru (nepovinné, např. /UnitSearchPaths:…)</label><input id="be-args" value="${esc(b.extraArgs)}" spellcheck="false"></div>
    </div>
    <div class="modal-f">
      ${isNew ? "" : `<button class="danger left" id="be-del">Smazat build</button>`}
      <button class="ghost" id="be-cancel">Zpět</button>
      <button class="primary" id="be-save">Uložit build</button>
    </div>`, (box, close) => {
    const cfgDefs = () => new Set((insp?.configDefines || []).map((x) => x.toUpperCase()));
    const drawDefs = () => {
      const all = [...(insp?.configDefines || []), "LEONZILLA", ...b.defines];
      const seen = new Set();
      $("#be-defs", box).textContent = "Kompilátor dostane /Defines: " + all.filter((d) => { const k = d.toUpperCase(); if (seen.has(k)) return false; seen.add(k); return true; }).join("; ");
    };
    const drawSwitches = () => {
      const fixed = cfgDefs();
      $("#be-sw", box).innerHTML = b.switches.length ? b.switches.map((x) => {
        const fx = fixed.has(x.name.toUpperCase());
        return `<li><label class="check"><input type="checkbox" data-sw="${esc(x.name)}" ${fx || b.defines.includes(x.name) ? "checked" : ""} ${fx ? "disabled" : ""}>
          <span><b>${esc(x.name)}</b>${fx ? ` <span class="note">– nastaví konfigurace ${esc(b.config)}</span>` : ""}${x.comment ? `<span class="note swc">${esc(x.comment)}</span>` : ""}</span></label></li>`;
      }).join("") : `<li class="note">Žádné přepínače – vyber projekt a dej Načíst přepínače.</li>`;
      $("#be-sw", box).querySelectorAll("[data-sw]").forEach((cb) => (cb.onchange = () => {
        const n = cb.dataset.sw;
        b.defines = cb.checked ? [...new Set([...b.defines, n])] : b.defines.filter((d) => d !== n);
        drawDefs();
      }));
      drawDefs();
    };
    const mergeSwitches = (apply) => {
      const inc = insp?.inc;
      if (!inc?.exists) return "";
      const known = new Set(b.switches.map((x) => x.name.toUpperCase()));
      const inFile = new Set(inc.switches.map((x) => x.name.toUpperCase()));
      const added = inc.switches.filter((x) => !known.has(x.name.toUpperCase()));
      const removed = b.switches.filter((x) => !inFile.has(x.name.toUpperCase()));
      if (apply) {
        const fixed = cfgDefs();
        added.forEach((x) => { if (inc.activeNow.includes(x.name) && !fixed.has(x.name.toUpperCase())) b.defines.push(x.name); });
        b.defines = b.defines.filter((d) => inFile.has(d.toUpperCase()) && !fixed.has(d.toUpperCase()));
        b.switches = inc.switches.map((x) => ({ name: x.name, comment: x.comment || "" }));
        drawSwitches();
      }
      const parts = [];
      if (added.length) parts.push("přibylo: " + added.map((x) => x.name).join(", "));
      if (removed.length) parts.push("ubylo: " + removed.map((x) => x.name).join(", "));
      return parts.join(" · ");
    };
    let pending = Promise.resolve();
    const inspect = (firstLoad) => (pending = doInspect(firstLoad));
    const doInspect = async (firstLoad) => {
      if (!b.project) { $("#be-cfg", box).innerHTML = `<option value="">– nejdřív vyber projekt –</option>`; drawSwitches(); return; }
      try {
        insp = await api("POST", "/api/builds/inspect", { project: b.project, incFile: b.incFile, config: b.config, platform: b.platform });
        const cfgs = insp.configs || [];
        if (!cfgs.includes(b.config)) {
          b.config = cfgs.find((c) => !/^debug$/i.test(c)) || cfgs[0] || "";
          if (b.config) insp = await api("POST", "/api/builds/inspect", { project: b.project, incFile: b.incFile, config: b.config, platform: b.platform });
        }
        $("#be-cfg", box).innerHTML = cfgs.map((c) => `<option ${c === b.config ? "selected" : ""}>${esc(c)}</option>`).join("");
        $("#be-cfgnote", box).textContent = insp.configDefines?.length ? `Konfigurace sama nastaví: ${insp.configDefines.join(", ")}` : "";
        $("#be-postcmd", box).textContent = insp.postBuild || "projekt nemá Post-build pro tuto konfiguraci";
        const inc = insp.inc;
        const note = $("#be-incnote", box);
        if (!inc?.exists) note.innerHTML = `<p class="bwarn">${esc(inc?.path || "Predefines.inc")} nenalezen – přepínače nejde nastavit.</p>`;
        else if (!inc.hasBlock) note.innerHTML = `<p class="bwarn">V ${esc(inc.path)} ještě není horní část v bloku <code>{$IFNDEF LEONZILLA} … {$ENDIF}</code>. Bez něj by se přepínače z buildu neuplatnily a build skončí chybou.</p>`;
        else note.innerHTML = "";
        if (!b.switches.length) mergeSwitches(true);
        else {
          drawSwitches();
          const diff = mergeSwitches(false);
          $("#be-reloadnote", box).textContent = diff ? `V souboru se změnilo – ${diff}` : "";
        }
      } catch (e) {
        $("#be-cfg", box).innerHTML = `<option value="">–</option>`;
        $("#be-incnote", box).innerHTML = `<p class="bwarn">${esc(e.message)}</p>`;
        drawSwitches();
      }
    };
    // until the info is edited by hand it follows the build name
    let infoTouched = b.versionInfo != null;
    $("#be-name", box).oninput = (e) => { b.name = e.target.value; if (!infoTouched) $("#be-info", box).value = b.name.trim().slice(0, 80); };
    $("#be-info", box).oninput = () => { infoTouched = true; };
    $("#be-proj", box).onchange = (e) => {
      const v = e.target.value.trim();
      if (v === b.project) return;
      b.project = v; b.switches = []; b.defines = []; inspect();
    };
    $("#be-proj-pick", box).onclick = async () => {
      try {
        const r = await api("POST", "/api/pickfile", { title: tr("Vyber projekt RAD Studia"), start: b.project, filterName: tr("Projekt Delphi (*.dproj)"), filterSpec: "*.dproj" });
        if (!r.cancelled) { b.project = r.path; $("#be-proj", box).value = r.path; b.switches = []; b.defines = []; inspect(); }
      } catch (e) { toast(e.message, true); }
    };
    $("#be-cfg", box).onchange = (e) => { b.config = e.target.value; inspect(); };
    $("#be-reload", box).onclick = async () => {
      await inspect();
      const diff = mergeSwitches(true);
      $("#be-reloadnote", box).textContent = diff ? `Načteno – ${diff}` : "Načteno – beze změny";
    };
    $("#be-crlf", box).onchange = (e) => (b.crlf = e.target.checked);
    $("#be-mask", box).oninput = (e) => (b.crlfMask = e.target.value);
    $("#be-post", box).onchange = (e) => (b.postBuild = e.target.checked);
    $("#be-extra", box).oninput = (e) => (b.extraCmd = e.target.value);
    $("#be-args", box).oninput = (e) => (b.extraArgs = e.target.value);
    $("#be-cancel", box).onclick = () => openSettings("builds");
    const saveAll = async (list) => {
      const r = await api("POST", "/api/builds", { builds: list });
      S.settings.builds = r.builds;
      const ids = new Set(r.builds.map((x) => x.id));
      S.tabs.forEach((t) => { t.builds = (t.builds || []).filter((id) => ids.has(id)); });
      renderTabs(); renderMain();
    };
    $("#be-save", box).onclick = async () => {
      $("#be-proj", box).dispatchEvent(new Event("change"));
      await pending;
      if (!b.name.trim()) return toast("Zadej název buildu", true);
      if (!b.project) return toast("Vyber projekt (.dproj)", true);
      if (!b.config) return toast("Vyber konfiguraci", true);
      b.versionInfo = $("#be-info", box).value.replace(/\s+/g, " ").trim().slice(0, 80);
      const list = builds().map((x) => (x.id === b.id ? b : x));
      if (isNew) list.push(b);
      try { await saveAll(list); toast(`Build „${b.name}“ uložen`); openSettings("builds"); } catch (e) { toast(e.message, true); }
    };
    $("#be-del", box)?.addEventListener("click", async (e) => {
      const btn = e.currentTarget;
      if (!btn.dataset.sure) { btn.dataset.sure = "1"; btn.textContent = "Opravdu smazat? (karty ho přestanou používat)"; return; }
      try { await saveAll(builds().filter((x) => x.id !== b.id)); toast("Build smazán"); openSettings("builds"); } catch (er) { toast(er.message, true); }
    });
    inspect(true);
    setTimeout(() => (isNew ? $("#be-name", box) : $("#be-sw", box))?.focus?.(), 30);
  }, true);
}

/* ------------------------------------------------------------------ backups */

function renderBackupPairs(t) {
  const wrap = $("#bpairs");
  if (!wrap) return;
  const list = t.backups || [];
  if (!list.length) { wrap.innerHTML = `<p class="note">Žádná záloha – přidej první.</p>`; return; }
  wrap.innerHTML = list.map((b, i) => `
    <div class="pair bpair${b.enabled ? "" : " off"}" data-i="${i}">
      <div class="pair-main">
        <input type="checkbox" class="toggle" data-k="enabled" ${b.enabled ? "checked" : ""} title="Zálohovat tuto složku">
        <div class="path-field${b.source && S.localOK[b.source] === false ? " missing" : ""}" data-local-field>
          <input data-k="source" value="${esc(b.source)}" placeholder="Co zálohovat – složka v PC" spellcheck="false" aria-label="Zálohovaná složka">
          ${S.canPickDir ? `<button data-pick="source" title="Vybrat zálohovanou složku">${icon("folder")}</button>` : ""}
        </div>
        <span class="flow bflow">${icon("arrow")}</span>
        <div class="path-field">
          <input data-k="target" value="${esc(b.target)}" placeholder="Kam uložit ZIP" spellcheck="false" aria-label="Cílová složka">
          ${S.canPickDir ? `<button data-pick="target" title="Vybrat cílovou složku">${icon("folder")}</button>` : ""}
        </div>
        <button class="icon-btn small" data-del title="Odebrat zálohu">${icon("x")}</button>
      </div>
      <div class="bprefix">
        <label>Název ZIP</label>
        <input data-k="prefix" value="${esc(b.prefix)}" placeholder="${esc(zipPrefix({ ...b, prefix: "" }))}" spellcheck="false" maxlength="80">
        <span class="note zipname">${esc(zipPrefix(b))}_${zipStampNow()}.zip</span>
      </div>
      <div class="pair-ex">
        <span class="lbl">Vyloučit:</span>
        ${(b.excludes || []).map((x, k) => `<span class="chip">${esc(x)}<button data-ex-del="${k}" title="Zrušit vyloučení">${icon("x")}</button></span>`).join("")}
        <span class="chip-add">
          ${S.canPickDir ? `<button data-ex-pick="dir" title="Vybrat složku uvnitř zálohované složky">${icon("folder")}složku</button>
          <button data-ex-pick="file" title="Vybrat soubor uvnitř zálohované složky">${icon("file")}soubor</button>` : ""}
          <button data-ex-type title="Napsat cestu nebo masku (např. *.psd)">${icon("plus")}vzor</button>
        </span>
      </div>
    </div>`).join("");
  requestAnimationFrame(showPathEnds);
  wrap.querySelectorAll(".bpair").forEach((el) => {
    const b = t.backups[+el.dataset.i];
    const refreshName = () => { const z = el.querySelector(".zipname"); if (z) z.textContent = `${zipPrefix(b)}_${zipStampNow()}.zip`; el.querySelector('[data-k="prefix"]').placeholder = zipPrefix({ ...b, prefix: "" }); };
    el.querySelectorAll("[data-k]").forEach((inp) => {
      const k = inp.dataset.k;
      if (k === "enabled") inp.onchange = () => { b.enabled = inp.checked; el.classList.toggle("off", !b.enabled); scheduleSave(t, 0); };
      else inp.oninput = () => { b[k] = inp.value; refreshName(); scheduleSave(t); };
    });
    el.querySelectorAll("[data-pick]").forEach((btn) => (btn.onclick = async () => {
      const k = btn.dataset.pick;
      try {
        const r = await api("POST", "/api/pickdir", { title: tr(k === "source" ? "Vyber složku k zálohování" : "Vyber, kam ukládat ZIP zálohy"), start: b[k] || b.source });
        if (r.cancelled) return;
        b[k] = r.path; el.querySelector(`[data-k="${k}"]`).value = r.path; refreshName(); scheduleSave(t, 0);
      } catch (e) { toast(e.message, true); }
    }));
    el.querySelector("[data-del]").onclick = () => { t.backups.splice(+el.dataset.i, 1); renderBackupPairs(t); scheduleSave(t, 0); };
    el.querySelectorAll("[data-ex-del]").forEach((x) => (x.onclick = () => { b.excludes.splice(+x.dataset.exDel, 1); renderBackupPairs(t); scheduleSave(t, 0); }));
    el.querySelectorAll("[data-ex-pick]").forEach((x) => (x.onclick = async () => {
      if (!b.source) return toast("Nejdřív vyber zálohovanou složku", true);
      try {
        const r = await api("POST", "/api/pickexclude", { local: b.source, kind: x.dataset.exPick });
        if (r.cancelled) return;
        addBackupExclude(t, b, r.rel);
      } catch (e) { toast(e.message, true); }
    }));
    el.querySelector("[data-ex-type]").onclick = (e) => {
      const holder = e.currentTarget.parentElement;
      const inp = document.createElement("input");
      inp.className = "chip-input"; inp.placeholder = "img/raw/ nebo *.psd";
      holder.before(inp); inp.focus();
      let fin = false;
      const done = (ok) => { if (fin) return; fin = true; const v = inp.value.trim(); inp.remove(); if (ok && v) addBackupExclude(t, b, v); };
      inp.onkeydown = (ev) => { if (ev.key === "Enter") done(true); if (ev.key === "Escape") done(false); };
      inp.onblur = () => afterPointer(() => done(true));
    };
  });
}

function addBackupExclude(t, b, v) {
  v = v.replace(/\\/g, "/").replace(/^\.\//, "");
  b.excludes = b.excludes || [];
  if (!b.excludes.includes(v)) b.excludes.push(v);
  renderBackupPairs(t); scheduleSave(t, 0);
}

function lastBackupText(t) {
  const l = t.lastBackup;
  if (!l) return "Zatím nezálohováno";
  const res = { ok: "", errors: " · s chybami", cancelled: " · přerušeno", failed: " · nepovedlo se" }[l.result] ?? "";
  return `Poslední záloha ${fmtWhen(l.at)}${l.result === "failed" ? res + (l.message ? ": " + esc(l.message) : "") : ` · ${fmtNum(l.uploaded)} ${l.uploaded === 1 ? "soubor" : l.uploaded >= 2 && l.uploaded <= 4 ? "soubory" : "souborů"}${res}`}`;
}

// same rules as cleanNote() in backup.go
const cleanNote = (s) => { let x = (s || "").trim().split(/\s+/).join(" ").replace(/[<>:"/\\|?*\x00-\x1f]+/g, "_").replace(/^[ ._]+|[ ._]+$/g, ""); if ([...x].length > 60) x = [...x].slice(0, 60).join("").replace(/[ ._]+$/, ""); return x; };

function confirmBackup(t) {
  // index = position in t.backups (the server matches the notes by it)
  const list = (t.backups || []).map((b, i) => ({ b, i })).filter(({ b }) => activeBackups(t).includes(b));
  if (!list.length) return toast("Karta nemá nastavenou žádnou zálohu", true);
  const miss = list.find(({ b }) => S.localOK[b.source] === false);
  if (miss) return toast(`Složka neexistuje: ${miss.b.source}`, true);
  const stamp = zipStampNow();
  const ask = !t.backupNoNote;
  const multi = list.length > 1;
  const ownName = (s) => cleanNote((s || "").trim().replace(/\.zip$/i, ""));
  const zipName = (b, txt, mode) => mode === "own" && ownName(txt)
    ? `<mark>${esc(ownName(txt))}</mark>.zip`
    : `${esc(zipPrefix(b))}_${txt && cleanNote(txt) ? `<mark>${esc(cleanNote(txt))}</mark>_` : ""}${stamp}.zip`;
  const PH = { note: (b) => `Poznámka k ${zipPrefix(b)} (nepovinné), např. nove cfg`, own: () => "Vlastní název ZIPu, např. fw-160 (bez data)" };
  modal(`
    <div class="modal-h"><h3>Zálohovat „${esc(t.name)}“?</h3><p>Každá složka se zabalí do ZIP v cílové složce.${ask ? " U každého ZIPu můžeš připsat poznámku (vloží se mezi název a datum) nebo zadat úplně vlastní název – prázdné pole = výchozí název." : ""}</p></div>
    <div class="modal-b">
      ${multi ? `<p class="note bpick-hint">Odškrtnutá záloha se tentokrát přeskočí (nastavení karty se nemění).</p>` : ""}
      <ul class="confirm-pairs bconfirm">${list.map(({ b, i }) => `<li data-i="${i}"><span class="bsrc">${multi ? `<input type="checkbox" class="bpick" data-pick="${i}" checked title="Zálohovat tuto složku">` : ""}<span>${esc(b.source)}</span></span>${icon("arrow")}<span>${esc(b.target.replace(/[\\/]+$/, ""))}\\<b class="zn">${zipName(b, "", "note")}</b></span>
        ${ask ? `<div class="bname" data-mode="note">
          <div class="seg bseg"><button type="button" data-m="note" aria-pressed="true" title="prefix_poznámka_datum.zip">Poznámka</button><button type="button" data-m="own" aria-pressed="false" title="Úplně vlastní název bez data">Vlastní název</button></div>
          <input class="bnote-in" data-note="${i}" maxlength="60" spellcheck="false" autocomplete="off" placeholder="${esc(PH.note(b))}">
        </div>` : ""}
        ${b.excludes.length ? `<div class="bex"><span class="lbl">Vyloučeno jen zde:</span>${b.excludes.map((x) => `<span class="chip ro">${esc(x)}</span>`).join("")}</div>` : ""}</li>`).join("")}</ul>
      ${(t.backupExcludes || []).length ? `<div class="bex all"><span class="lbl">Ve všech zálohách vyloučeno:</span>${t.backupExcludes.map((x) => `<span class="chip ro">${esc(x)}</span>`).join("")}</div>` : ""}
    </div>
    <div class="modal-f"><button class="ghost" data-close>Zrušit</button>
      <button class="backup-btn" id="m-ok">${icon("archive")}Zálohovat</button></div>`,
    (box, close) => {
      const ok = $("#m-ok", box);
      const inputs = [...box.querySelectorAll("[data-note]")];
      const picks = [...box.querySelectorAll("[data-pick]")];
      const okLabel = ok.innerHTML;
      const refresh = () => {
        picks.forEach((cb) => {
          const li = cb.closest("li");
          li.classList.toggle("off", !cb.checked);
          const inp = li.querySelector("[data-note]"); if (inp) inp.disabled = !cb.checked;
        });
        const n = picks.filter((cb) => cb.checked).length;
        ok.disabled = multi && n === 0;
        ok.innerHTML = multi && n < picks.length ? okLabel.replace("Zálohovat", `Zálohovat ${n} z ${picks.length}`) : okLabel;
      };
      picks.forEach((cb) => (cb.onchange = refresh));
      inputs.forEach((inp, k) => {
        const li = inp.closest("li"), b = t.backups[+li.dataset.i];
        const wrap = li.querySelector(".bname");
        const upd = () => { li.querySelector(".zn").innerHTML = zipName(b, inp.value, wrap.dataset.mode); };
        inp.oninput = upd;
        wrap.querySelectorAll("[data-m]").forEach((btn) => (btn.onclick = () => {
          wrap.dataset.mode = btn.dataset.m;
          wrap.querySelectorAll("[data-m]").forEach((x) => x.setAttribute("aria-pressed", x === btn));
          inp.placeholder = PH[btn.dataset.m](b);
          upd(); inp.focus();
        }));
        // Enter = next (ticked) note, Enter in the last one = Zálohovat
        inp.onkeydown = (e) => {
          if (e.key !== "Enter") return;
          e.preventDefault();
          const nx = inputs.slice(k + 1).find((x) => !x.disabled);
          nx ? nx.focus() : ok.click();
        };
      });
      (inputs[0] || ok).focus();
      ok.onclick = () => {
        if (ok.disabled) return;
        const notes = (t.backups || []).map(() => ""), names = (t.backups || []).map(() => "");
        inputs.forEach((inp) => {
          if (inp.disabled) return;
          if (inp.closest(".bname").dataset.mode === "own") names[+inp.dataset.note] = ownName(inp.value);
          else notes[+inp.dataset.note] = cleanNote(inp.value);
        });
        const only = multi ? picks.filter((cb) => cb.checked).map((cb) => +cb.dataset.pick) : null;
        close();
        startJob(t, { backup: true, notes, names, only });
      };
    });
}

/* ------------------------------------------------------------------ GitHub */

const gitShort = (u) => (u || "").replace(/^https?:\/\/(www\.)?/, "").replace(/^git@([^:]+):/, "$1/").replace(/\.git$/, "");
const gitInfoText = (st) => !st ? "…" : !st.ok ? st.error
  : `${st.detached ? "odpojená HEAD" : st.branch}${st.remote ? " → " + gitShort(st.remote) : " · bez vzdáleného serveru"}${st.files.length ? ` · ${fmtNum(st.files.length)} ${st.files.length === 1 ? "změna" : st.files.length < 5 ? "změny" : "změn"}` : " · beze změn"}${st.ahead ? ` · ${st.ahead} k odeslání` : ""}`;

function lastGitText(t) {
  const l = t.lastGit;
  if (!l) return "Zatím neodesláno";
  const res = { ok: "", errors: " · s chybami", cancelled: " · ukončeno", failed: " · nepovedlo se" }[l.result] ?? "";
  return `GitHub ${fmtWhen(l.at)} · ${esc(l.message || "")}${res}`;
}

function renderGitRepos(t) {
  const wrap = $("#gitrepos");
  if (!wrap) return;
  const list = t.git || [];
  if (!list.length) { wrap.innerHTML = `<p class="note">Žádný repozitář – přidej první.</p>`; return; }
  wrap.innerHTML = list.map((g, i) => `
    <div class="pair grepo${g.enabled ? "" : " off"}" data-i="${i}">
      <div class="pair-main grepo-main">
        <input type="checkbox" class="toggle" data-k="enabled" ${g.enabled ? "checked" : ""} title="Zahrnout do GitHub">
        <div class="path-field">
          <input data-k="path" value="${esc(g.path)}" placeholder="Složka s git repozitářem" spellcheck="false" aria-label="Složka repozitáře">
          ${S.canPickDir ? `<button data-pick title="Vybrat složku">${icon("folder")}</button>` : ""}
        </div>
        <button class="icon-btn small" data-up title="Posunout výš" ${i ? "" : "disabled"}>${icon("chev")}</button>
        <button class="icon-btn small" data-del title="Odebrat">${icon("x")}</button>
      </div>
      <p class="ginfo note" data-info>${g.path ? "…" : ""}</p>
    </div>`).join("");
  wrap.querySelectorAll(".grepo").forEach((el) => {
    const g = t.git[+el.dataset.i];
    const info = async () => {
      const box = el.querySelector("[data-info]");
      if (!g.path.trim()) { box.textContent = ""; return; }
      try { const st = await api("POST", "/api/git/status", { path: g.path }); box.textContent = gitInfoText(st); box.classList.toggle("err", !st.ok); } catch (e) { box.textContent = e.message; }
    };
    info();
    el.querySelector('[data-k="enabled"]').onchange = (e) => { g.enabled = e.target.checked; el.classList.toggle("off", !g.enabled); scheduleSave(t, 0); };
    const inp = el.querySelector('[data-k="path"]');
    inp.oninput = () => { g.path = inp.value; scheduleSave(t); };
    inp.onchange = info;
    el.querySelector("[data-pick]")?.addEventListener("click", async () => {
      try {
        const r = await api("POST", "/api/pickdir", { title: tr("Vyber složku s git repozitářem"), start: g.path });
        if (r.cancelled) return;
        g.path = r.path; inp.value = r.path; scheduleSave(t, 0); info();
      } catch (e) { toast(e.message, true); }
    });
    el.querySelector("[data-up]").onclick = () => { const i = +el.dataset.i; [t.git[i - 1], t.git[i]] = [t.git[i], t.git[i - 1]]; renderGitRepos(t); scheduleSave(t, 0); };
    el.querySelector("[data-del]").onclick = () => { t.git.splice(+el.dataset.i, 1); renderGitRepos(t); scheduleSave(t, 0); };
  });
}

// fills branch / remote / changes into the card view
function loadGitInfo(t) {
  document.querySelectorAll("#main .vgit li[data-gpath]").forEach(async (li) => {
    if (li.classList.contains("off") || !li.dataset.gpath) return;
    const box = li.querySelector(".ginfo");
    try { const st = await api("POST", "/api/git/status", { path: li.dataset.gpath }); box.textContent = gitInfoText(st); box.classList.toggle("err", !st.ok); } catch (e) { box.textContent = e.message; }
  });
}

const GKIND = { new: ["N", "nový"], modified: ["M", "změněný"], deleted: ["D", "smazaný"], renamed: ["R", "přejmenovaný"], conflict: ["!", "konflikt"] };

// walks the card's repositories one by one: skip clean ones, otherwise files + diff + message → commit / push
function openGitWindow(t) {
  if (!S.gitFound) return toast("Na tomto PC není nainstalovaný Git (Git for Windows)", true);
  const repos = activeGit(t);
  if (!repos.length) return toast("Karta nemá nastavený žádný repozitář", true);
  const G = { t, repos, i: -1, res: repos.map(() => ({ state: "wait", note: "" })), st: null, sel: new Set(), diffFile: null, busy: false, commits: 0, ended: false };
  modal(`
    <div class="modal-h"><h3>GitHub „${esc(t.name)}“</h3><p id="gw-sub"></p></div>
    <div class="modal-b gw-b">
      <ol class="gw-repos" id="gw-repos"></ol>
      <div class="gw-cur" id="gw-cur"></div>
    </div>
    <div class="modal-f" id="gw-foot"></div>`, (box, close) => { G.box = box; G.close = close; }, true);
  $("#overlay .modal").classList.add("gw");
  const box = G.box;
  const finish = async (manual) => {
    if (G.ended) return;
    G.ended = true;
    G.res.forEach((r) => { if (r.state === "wait" || r.state === "cur") { r.state = "skip"; r.note = manual ? "neprojito" : r.note; } });
    const bad = G.res.filter((r) => r.state === "err").length;
    const msg = G.commits || G.res.some((r) => r.pushed)
      ? `${G.commits} ${G.commits === 1 ? "commit" : G.commits < 5 && G.commits ? "commity" : "commitů"}${G.res.some((r) => r.pushed) ? ", odesláno" : ""}`
      : "nic k odeslání";
    const result = bad ? "errors" : manual && G.res.some((r) => r.note === "neprojito") ? "cancelled" : "ok";
    try { const r = await api("POST", `/api/tabs/${t.id}/gitdone`, { result, message: msg, commits: G.commits }); t.lastGit = r.lastGit; } catch {}
    const lg = $("#last-git"); if (lg) lg.innerHTML = lastGitText(t);
    updateTile(t.id);
    if (S.view === "card" && t.id === S.current && !editing(t.id)) loadGitInfo(t);
  };
  // closing the window (Esc, click outside) = end
  const ov = $("#overlay");
  const obs = new MutationObserver(() => { if (ov.hidden || !ov.contains(box)) { obs.disconnect(); finish(true); } });
  obs.observe(ov, { childList: true, attributes: true });

  const drawRepos = () => {
    $("#gw-repos", box).innerHTML = G.repos.map((g, k) => {
      const r = G.res[k];
      const ic = { ok: "✓", skip: "–", err: "✕", cur: "●", wait: "" }[r.state];
      return `<li class="${r.state}"><span class="ic">${ic}</span><span class="nm">${esc(g.path.replace(/[\\/]+$/, "").split(/[\\/]/).pop())}</span><span class="note">${esc(r.note)}</span></li>`;
    }).join("");
    $("#gw-sub", box).textContent = G.i < G.repos.length ? `Repozitář ${Math.min(G.i + 1, G.repos.length)} / ${G.repos.length}` : "Hotovo";
  };
  const foot = (html) => { $("#gw-foot", box).innerHTML = html; };
  const next = async () => {
    G.i++;
    G.st = null; G.diffFile = null;
    if (G.i >= G.repos.length) return done();
    G.res[G.i].state = "cur";
    drawRepos();
    $("#gw-cur", box).innerHTML = `<p class="note gw-wait">Zjišťuji stav${t.gitPull ? " (a co je nového na serveru)" : ""}…</p>`;
    foot(`<button class="ghost" id="gw-end">Konec</button>`);
    $("#gw-end", box).onclick = () => G.close();
    let st;
    try { st = await api("POST", "/api/git/status", { path: G.repos[G.i].path, fetch: !!t.gitPull }); } catch (e) { st = { ok: false, error: e.message, files: [] }; }
    if (G.ended) return;
    G.st = st;
    if (!st.ok) return showRepo();
    if (!st.files.length && !st.ahead) {
      G.res[G.i] = { state: "skip", note: "nic k commitu" };
      return next();
    }
    G.sel = new Set(st.files.filter((f) => f.kind !== "conflict").map((f) => f.path));
    showRepo();
  };
  const done = () => {
    drawRepos();
    const n = G.res.filter((r) => r.state === "ok").length;
    $("#gw-cur", box).innerHTML = `<div class="gw-done"><p class="${G.res.some((r) => r.state === "err") ? "warn" : "ok"}">${n ? `Hotovo – ${G.commits} ${G.commits === 1 ? "commit" : G.commits < 5 && G.commits ? "commity" : "commitů"}${G.res.some((r) => r.pushed) ? " odesláno na server" : ""}.` : "Nic k odeslání."}</p></div>`;
    foot(`<button class="primary" id="gw-close">Zavřít</button>`);
    $("#gw-close", box).onclick = () => G.close();
    $("#gw-close", box).focus();
    finish(false);
  };
  const showRepo = (err) => {
    const st = G.st, g = G.repos[G.i];
    const cur = $("#gw-cur", box);
    if (!st.ok) {
      cur.innerHTML = `<div class="gw-head"><b>${esc(g.path)}</b></div><p class="pv-err">${esc(st.error)}</p>`;
      foot(`<button class="ghost" id="gw-end">Konec</button><button class="ghost" id="gw-retry">Zkusit znovu</button><button class="plain" id="gw-skip">Přeskočit</button>`);
      $("#gw-end", box).onclick = () => G.close();
      $("#gw-retry", box).onclick = () => { G.i--; next(); };
      $("#gw-skip", box).onclick = () => { G.res[G.i] = { state: "err", note: st.error }; next(); };
      return;
    }
    const warns = [];
    if (st.fetchError) warns.push(`Nepodařilo se zjistit stav na serveru: ${esc(st.fetchError)}`);
    if (st.busy) warns.push(`V repozitáři probíhá ${esc(st.busy)} – dokonči ho ručně (např. v GitHub Desktop).`);
    if (st.detached) warns.push("Repozitář není na žádné větvi (odpojená HEAD) – push nepůjde.");
    if (!st.remote) warns.push("Repozitář nemá vzdálený server (origin) – půjde jen commit do PC.");
    if (st.behind && !t.gitPull) warns.push(`Na serveru ${st.behind === 1 ? "je 1 novější commit" : `je ${st.behind} novějších commitů`} – push selže. Zapni v nastavení karty „Před pushem stáhnout změny“, nebo je stáhni ručně.`);
    else if (st.behind) warns.push(`Na serveru ${st.behind === 1 ? "je 1 novější commit" : `je ${st.behind} novějších commitů`} – před pushem se stáhne (pull --rebase).`);
    if (st.files.some((f) => f.kind === "conflict")) warns.push("Některé soubory jsou v konfliktu – ty se commitovat nedají.");
    if (st.truncated) warns.push(`Změněných souborů je víc než ${fmtNum(st.files.length)} – zobrazuje se jen část. Nechybí v .gitignore výstup buildu?`);
    const head = `<div class="gw-head">
        <div><b>${esc(st.name)}</b> <span class="chip ro on">${esc(st.detached ? "odpojená HEAD" : st.branch)}</span>${st.remote ? `<span class="note"> → ${esc(gitShort(st.remote))}</span>` : ""}</div>
        <div class="note mono">${esc(st.root)}</div>
        ${st.ahead ? `<div class="note">${st.ahead} ${st.ahead === 1 ? "commit čeká" : st.ahead < 5 ? "commity čekají" : "commitů čeká"} na odeslání</div>` : ""}
      </div>
      ${warns.map((w) => `<p class="bwarn">${w}</p>`).join("")}
      ${err ? `<div class="gw-err"><p class="pv-err">${esc(err.error)}</p>${(err.steps || []).filter((x) => x.out).map((x) => `<details${x.ok ? "" : " open"}><summary>${x.ok ? "✓" : "✕"} ${esc(x.name)}</summary><pre>${esc(x.out)}</pre></details>`).join("")}</div>` : ""}`;
    if (!st.files.length) {
      cur.innerHTML = head + `<p class="note">Nic nového ke commitu – jen odeslat hotové commity na server.</p>`;
      foot(`<button class="ghost" id="gw-end">Konec</button><button class="plain" id="gw-skip">Přeskočit</button>
        <button class="git-btn" id="gw-push" ${st.remote && !st.detached ? "" : "disabled"}>${icon("up")}Push</button>`);
      bindFoot(false);
      return;
    }
    cur.innerHTML = head + `
      <div class="gw-split">
        <div class="gw-files">
          <label class="check gw-all"><input type="checkbox" id="gw-all">Vše (${fmtNum(st.files.length)})</label>
          <ul id="gw-list">${st.files.map((f, k) => `<li data-k="${k}" class="${f.kind}${G.diffFile === f.path ? " act" : ""}">
            <input type="checkbox" ${G.sel.has(f.path) ? "checked" : ""} ${f.kind === "conflict" ? "disabled" : ""} title="Zahrnout do commitu">
            <span class="gk ${f.kind}" title="${GKIND[f.kind][1]}">${GKIND[f.kind][0]}</span>
            <span class="gp" title="${esc(f.orig ? f.orig + " → " + f.path : f.path)}">${esc(f.path)}</span></li>`).join("")}</ul>
        </div>
        <div class="gw-diff" id="gw-diff"><p class="note">Klikni na soubor – ukáže se, co se v něm změnilo.</p></div>
      </div>
      <div class="gw-msg">
        <input id="gw-m" placeholder="Popis commitu (povinné) – např. Oprava přihlášení" maxlength="200" spellcheck="true" value="${esc(G.msg || "")}">
        <textarea id="gw-d" placeholder="Podrobnosti (nepovinné)" spellcheck="true">${esc(G.det || "")}</textarea>
      </div>`;
    foot(`<button class="ghost left" id="gw-end">Konec</button><button class="plain" id="gw-skip">Přeskočit</button>
      <button class="ghost" id="gw-commit">Jen commit</button>
      <button class="git-btn" id="gw-push" ${st.remote && !st.detached ? "" : "disabled"}>${icon("up")}Commit + Push</button>`);
    const list = $("#gw-list", box), all = $("#gw-all", box);
    const syncAll = () => {
      const can = st.files.filter((f) => f.kind !== "conflict");
      all.checked = can.length > 0 && can.every((f) => G.sel.has(f.path));
      all.indeterminate = !all.checked && can.some((f) => G.sel.has(f.path));
      const n = G.sel.size;
      $("#gw-commit", box).textContent = n ? `Jen commit (${n})` : "Jen commit";
    };
    all.onchange = () => {
      st.files.forEach((f) => { if (f.kind !== "conflict") all.checked ? G.sel.add(f.path) : G.sel.delete(f.path); });
      list.querySelectorAll("li").forEach((li) => { const cb = li.querySelector("input"); if (!cb.disabled) cb.checked = all.checked; });
      syncAll();
    };
    list.onclick = (e) => {
      const li = e.target.closest("li");
      if (!li) return;
      const f = st.files[+li.dataset.k];
      if (e.target.matches("input")) { e.target.checked ? G.sel.add(f.path) : G.sel.delete(f.path); syncAll(); return; }
      list.querySelectorAll("li.act").forEach((x) => x.classList.remove("act"));
      li.classList.add("act");
      showDiff(f);
    };
    $("#gw-m", box).oninput = (e) => (G.msg = e.target.value);
    $("#gw-d", box).oninput = (e) => (G.det = e.target.value);
    syncAll();
    bindFoot(true);
    if (!err) $("#gw-m", box).focus();
    const first = st.files.find((f) => f.path === G.diffFile) || st.files[0];
    if (first) { list.querySelector(`li[data-k="${st.files.indexOf(first)}"]`)?.classList.add("act"); showDiff(first); }
  };
  const showDiff = async (f) => {
    G.diffFile = f.path;
    const el = $("#gw-diff", box);
    el.innerHTML = `<p class="note">Načítám…</p>`;
    let d;
    try { d = await api("POST", "/api/git/diff", { path: G.repos[G.i].path, file: f }); } catch (e) { if (G.diffFile === f.path) el.innerHTML = `<p class="pv-err">${esc(e.message)}</p>`; return; }
    if (G.diffFile !== f.path) return;
    if (d.binary) { el.innerHTML = `<p class="note">${esc(f.path)} – binární soubor (${GKIND[f.kind][1]}), změny nejde zobrazit.</p>`; return; }
    if (f.kind === "deleted" && !d.text) { el.innerHTML = `<p class="note">Soubor je smazaný.</p>`; return; }
    const lines = d.text.split("\n").filter((l, k, a) => k < a.length - 1 || l);
    el.innerHTML = `<pre class="diff">${lines.map((l) => {
      const c = l.startsWith("+++") || l.startsWith("---") || l.startsWith("diff ") || l.startsWith("index ") || /^(similarity|rename|new file|deleted file|old mode|new mode)/.test(l) ? "meta" : l.startsWith("@@") ? "hunk" : l[0] === "+" ? "add" : l[0] === "-" ? "del" : "";
      return `<span class="${c}">${esc(l) || " "}</span>`;
    }).join("")}</pre>${d.truncated ? `<p class="note">… zkráceno</p>` : ""}${!lines.length ? `<p class="note">Změna jen v oprávněních nebo koncích řádků.</p>` : ""}`;
  };
  const bindFoot = (withFiles) => {
    $("#gw-end", box).onclick = () => G.close();
    $("#gw-skip", box).onclick = () => { G.res[G.i] = { state: "skip", note: "přeskočeno" }; G.msg = G.det = ""; next(); };
    const run = async (push, commit) => {
      if (G.busy) return;
      if (commit) {
        if (!G.sel.size) return toast("Vyber aspoň jeden soubor", true);
        if (!(G.msg || "").trim()) { $("#gw-m", box)?.focus(); return toast("Napiš popis commitu", true); }
      }
      G.busy = true;
      box.querySelectorAll("#gw-foot button, .gw-files input, .gw-msg input, .gw-msg textarea").forEach((b) => (b.disabled = true));
      const btn = $(push ? "#gw-push" : "#gw-commit", box);
      btn.classList.add("busy"); btn.textContent = push ? (commit ? "Commit + Push…" : "Push…") : "Commit…";
      let r;
      try {
        r = await api("POST", "/api/git/commit", { tab: t.id, path: G.repos[G.i].path, files: commit ? G.st.files.filter((f) => G.sel.has(f.path)) : [], message: G.msg || "", details: G.det || "", commit, push, pull: !!t.gitPull && push });
      } catch (e) { r = { ok: false, error: e.message, steps: [] }; }
      G.busy = false;
      if (G.ended) return;
      if (r.hash) G.commits++;
      if (!r.ok) {
        if (r.status) G.st = r.status;
        if (r.hash) { G.msg = G.det = ""; G.sel = new Set(G.st.files.filter((f) => f.kind !== "conflict").map((f) => f.path)); }
        G.res[G.i] = { state: "cur", note: r.hash ? `commit ${r.hash}, push selhal` : "chyba" };
        drawRepos();
        return showRepo(r);
      }
      G.res[G.i] = { state: "ok", note: `${r.hash ? "commit " + r.hash : ""}${r.hash && r.pushed ? " + " : ""}${r.pushed ? "push" : ""}`, pushed: r.pushed };
      G.msg = G.det = "";
      next();
    };
    $("#gw-push", box).onclick = () => run(true, withFiles);
    if (withFiles) $("#gw-commit", box).onclick = () => run(false, true);
    if (withFiles) $("#gw-m", box).onkeydown = (e) => { if (e.key === "Enter" && e.ctrlKey) $("#gw-push", box).click(); };
  };
  next();
}

/* ------------------------------------------------------------------ Build light (own script) */

function bindLight(t) {
  const sc = $("#f-lscript"), ar = $("#f-largs"), ll = $("#f-llabel");
  if (!sc) return;
  const cfg = () => (t.buildLight ||= { script: "", args: "", label: "" });
  const hint = () => { $("#light-hint").textContent = hasLight(t) ? lightName(t) : "nepovinné – vlastní skript (Python, BAT, PowerShell) místo buildu RAD Studia"; updateSyncButton(); };
  sc.oninput = () => { cfg().script = sc.value.trim(); hint(); scheduleSave(t); };
  ar.oninput = () => { cfg().args = ar.value; scheduleSave(t); };
  if (ll) ll.oninput = () => { cfg().label = ll.value; updateSyncButton(); scheduleSave(t); };
  $("#f-lscript-pick")?.addEventListener("click", async () => {
    try {
      const r = await api("POST", "/api/pickfile", { title: tr("Vyber skript"), start: sc.value, filterName: tr("Skript (*.py, *.bat, *.cmd, *.ps1)"), filterSpec: "*.py;*.bat;*.cmd;*.ps1" });
      if (!r.cancelled) { sc.value = r.path; cfg().script = r.path; hint(); scheduleSave(t, 0); }
    } catch (e) { toast(e.message, true); }
  });
}

/* ------------------------------------------------------------------ FW build (ESP-IDF) */

function fwCfg(t) { if (!t.fw) t.fw = { project: "", idfProfile: "", variants: [], baud: 115200 }; return t.fw; }

function renderFwEdit(t) {
  const box = $("#fwbox");
  if (!box) return;
  const f = t.fw || { project: "", idfProfile: "", variants: [], baud: 115200 };
  box.innerHTML = `
    <div class="f c12 checks"><label class="check"><input type="checkbox" id="f-fwmon" ${f.monitor ? "checked" : ""}><span><b>Monitor</b> – sériová konzole zařízení se vstupem: na kartě přibude tlačítko <i>Monitor</i> (vybereš COM port a jede to, i bez buildu a nahrávání)</span></label></div>
    <div class="f c12"><label for="f-fwproj">Složka projektu ESP-IDF (kde je CMakeLists.txt) <span class="note">– jen pro FW build, pro samotný monitor není potřeba</span></label>
      <div class="path-field"><input id="f-fwproj" value="${esc(f.project)}" placeholder="C:\\Projekty\\esp32\\firmware" spellcheck="false">
      ${S.canPickDir ? `<button id="f-fwproj-pick" title="Vybrat složku">${icon("folder")}</button>` : ""}</div></div>
    <div class="f c12"><label for="f-fwprof">Aktivace ESP-IDF – PowerShell profil z EIM <span class="note">(prázdné = automaticky: ${esc(S.idfProfile || "v C:\\Espressif\\tools nic nenalezeno")})</span></label>
      <div class="path-field"><input id="f-fwprof" value="${esc(f.idfProfile || "")}" placeholder="${esc(S.idfProfile || "C:\\Espressif\\tools\\Microsoft.v6.0.3.PowerShell_profile.ps1")}" spellcheck="false">
      ${S.canPickDir ? `<button id="f-fwprof-pick" title="Vybrat soubor">${icon("file")}</button>` : ""}</div></div>
    <div class="f c12"><span class="lab">Varianty buildu <span class="note">– skript se spustí s parametry <code>build</code> / <code>-p COMx flash</code>; Clean config smaže <code>&lt;složka buildu&gt;\\sdkconfig</code></span></span>
      <div class="fwvars" id="fw-vars"></div>
      <div class="row-btns"><button class="ghost small" id="fw-detect">${icon("file")}Načíst z tools\\build_*.cmd</button><button class="ghost small" id="fw-add">${icon("plus")}Přidat variantu</button><span class="note" id="fw-detnote"></span></div></div>
    <div class="f c3"><label for="f-fwbaud">Monitor – rychlost (Bd)</label><input id="f-fwbaud" type="number" min="1200" step="1" value="${f.baud || 115200}"></div>`;
  const drawVars = () => {
    const vs = (t.fw?.variants) || [];
    $("#fw-vars").innerHTML = vs.length ? `<div class="fwv head"><span>Název</span><span>Skript (v projektu)</span><span>Složka buildu</span><span></span><span></span></div>` + vs.map((v, i) => `
      <div class="fwv" data-i="${i}">
        <input data-k="name" value="${esc(v.name)}" placeholder="DEV" maxlength="40">
        <input data-k="script" value="${esc(v.script)}" placeholder="tools\\build_dev.cmd" spellcheck="false">
        <input data-k="buildDir" value="${esc(v.buildDir)}" placeholder="build_dev" spellcheck="false">
        <label class="check" title="PROD – skript nahrávání zakazuje"><input type="checkbox" data-k="noFlash" ${v.noFlash ? "checked" : ""}>bez nahrávání</label>
        <button class="icon-btn small" data-del title="Odebrat variantu">${icon("x")}</button>
      </div>`).join("") : `<p class="note">Žádná varianta – načti je ze složky tools, nebo přidej ručně.</p>`;
    $("#fw-vars").querySelectorAll(".fwv[data-i]").forEach((row) => {
      const v = t.fw.variants[+row.dataset.i];
      row.querySelectorAll("[data-k]").forEach((inp) => {
        const k = inp.dataset.k;
        if (k === "noFlash") inp.onchange = () => { v.noFlash = inp.checked; scheduleSave(t, 0); };
        else inp.oninput = () => { v[k] = inp.value; scheduleSave(t); };
      });
      row.querySelector("[data-del]").onclick = () => { t.fw.variants.splice(+row.dataset.i, 1); drawVars(); scheduleSave(t, 0); };
    });
  };
  drawVars();
  const proj = $("#f-fwproj"), prof = $("#f-fwprof");
  const detect = async (quiet) => {
    const p = proj.value.trim();
    if (!p) return quiet || toast("Nejdřív vyplň složku projektu", true);
    try {
      const r = await api("POST", "/api/fw/detect", { project: p });
      fwCfg(t).variants = r.variants;
      if (r.idfProfile) S.idfProfile = r.idfProfile;
      drawVars(); scheduleSave(t, 0);
      $("#fw-detnote").textContent = `Načteno: ${r.variants.map((v) => v.name).join(", ")}`;
    } catch (e) { if (!quiet) toast(e.message, true); else $("#fw-detnote").textContent = e.message; }
  };
  proj.oninput = () => { fwCfg(t).project = proj.value.trim(); scheduleSave(t); };
  proj.onchange = () => { if (!(t.fw?.variants || []).length) detect(true); };
  prof.oninput = () => { fwCfg(t).idfProfile = prof.value.trim(); scheduleSave(t); };
  $("#f-fwbaud").oninput = (e) => { fwCfg(t).baud = parseInt(e.target.value, 10) || 115200; scheduleSave(t); };
  $("#f-fwmon").onchange = (e) => { fwCfg(t).monitor = e.target.checked; scheduleSave(t, 0); };
  $("#fw-detect").onclick = () => {
    if ((t.fw?.variants || []).length && !confirmReplace()) return;
    detect(false);
  };
  const confirmReplace = () => { const b = $("#fw-detect"); if (b.dataset.sure) return true; b.dataset.sure = "1"; b.innerHTML = "Opravdu? Nahradí současné varianty"; return false; };
  $("#fw-add").onclick = () => { fwCfg(t).variants.push({ id: "", name: "", script: "tools\\build_.cmd", buildDir: "build", noFlash: false }); drawVars(); scheduleSave(t, 0); };
  $("#f-fwproj-pick")?.addEventListener("click", async () => {
    try {
      const r = await api("POST", "/api/pickdir", { title: tr("Vyber složku projektu ESP-IDF"), start: proj.value });
      if (r.cancelled) return;
      proj.value = r.path; fwCfg(t).project = r.path; scheduleSave(t, 0);
      if (!(t.fw.variants || []).length) detect(true);
    } catch (e) { toast(e.message, true); }
  });
  $("#f-fwprof-pick")?.addEventListener("click", async () => {
    try {
      const r = await api("POST", "/api/pickfile", { title: tr("Vyber PowerShell profil ESP-IDF (EIM)"), start: prof.value || S.idfProfile || "C:\\Espressif\\tools", filterName: tr("PowerShell profil (*.ps1)"), filterSpec: "*.ps1" });
      if (!r.cancelled) { prof.value = r.path; fwCfg(t).idfProfile = r.path; scheduleSave(t, 0); }
    } catch (e) { toast(e.message, true); }
  });
}

// background watch of a FW session whose window is hidden (tile shows it)
const fwBg = {};
function fwBgKick(id) {
  if (fwBg[id]) return;
  fwBg[id] = setInterval(async () => {
    if (fwWin && fwWin.id === id) return;
    let st;
    try { st = await api("GET", `/api/tabs/${id}/fw?log=999999999`); } catch { return; }
    const was = S.fwRunning[id];
    if (st.none || st.phase === "done") {
      delete S.fwRunning[id]; clearInterval(fwBg[id]); delete fwBg[id];
      if (was) { const t = S.tabs.find((x) => x.id === id); toast(`${t?.name || ""}: FW ${({ ok: "hotovo", failed: "se nepovedl", cancelled: "přerušen" })[st.result] || "skončil"}`, st.result === "failed"); }
    } else S.fwRunning[id] = st.phase;
    if (S.view === "overview") renderOverview();
  }, 2000);
}

let fwWin = null;
function openFwWindow(t, monOnly = false) {
  if (!hasFw(t) && !hasMon(t)) return toast("Karta nemá nastavený FW build ani monitor", true);
  if (!hasFw(t)) monOnly = true;
  const f = t.fw;
  f.variants = f.variants || [];
  // variants are picked by position: a just-added variant may not have its id yet (it is assigned on save)
  const lastVi = f.lastVariant ? f.variants.findIndex((v) => v.id === f.lastVariant) : -1;
  const st = { id: t.id, seen: 0, timer: null, vi: lastVi >= 0 ? lastVi : 0,
    action: monOnly ? "monitor" : f.lastAction && f.lastAction !== "monitor" ? f.lastAction : "flashmon", port: f.lastPort || "", hist: [], hi: -1, phase: "", running: false };
  fwWin = st;
  const ACTS = [["build", "Build"], ["flash", "Build + Nahrát"], ["flashmon", "Build + Nahrát + Monitor"], ["monitor", "Jen monitor"]];
  modal(`
    <div class="modal-h"><h3>${monOnly ? "Monitor" : "FW build"} „${esc(t.name)}“</h3><p class="mono" style="font-size:12px">${monOnly ? `sériová konzole zařízení · ${f.baud || 115200} Bd` : esc(f.project)}</p></div>
    <div class="modal-b fw-b">
      <fieldset class="fw-ctl" id="fw-ctl">
        <div class="fw-row" ${monOnly ? "hidden" : ""}><span class="lab">Varianta</span><div class="seg fwseg" id="fw-var">${f.variants.map((v, i) => `<button type="button" data-v="${i}">${esc(v.name || "?")}</button>`).join("")}</div></div>
        <div class="fw-row" ${monOnly ? "hidden" : ""}><span class="lab">Akce</span><div class="seg fwseg" id="fw-act">${ACTS.map(([k, n]) => `<button type="button" data-a="${k}">${n}</button>`).join("")}</div></div>
        <div class="fw-row"><span class="lab">Port</span><select id="fw-port"></select><button type="button" class="icon-btn small" id="fw-ports" title="Znovu načíst porty">↻</button><span class="note" id="fw-portnote"></span></div>
        <label class="check fw-clean"><input type="checkbox" id="fw-clean" ${f.lastClean ? "checked" : ""}><span>Clean config – před buildem smazat <code id="fw-cleanpath"></code> <span class="note">(po změně sdkconfig.defaults / profilu / Kconfig)</span></span></label>
        <label class="check fw-clean fw-reconf"><input type="checkbox" id="fw-reconf" ${f.lastReconfigure === false ? "" : "checked"}><span>Reconfigure – před buildem spustit <code id="fw-reconfcmd"></code> <span class="note">(znovu načte CMake / sdkconfig)</span></span></label>
      </fieldset>
      <ol class="bsteps" id="fw-steps"></ol>
      <div class="fw-logwrap">
        <ol class="act-log fw-log" id="fw-log" tabindex="0" translate="no"></ol>
        <div class="fw-logbar">
          <button type="button" class="fw-lb" id="fw-copy" title="Zkopírovat celý výpis do schránky (označený text: Ctrl+C)">${icon("copy")}Kopírovat vše</button>
          <button type="button" class="fw-lb" id="fw-clear" title="Vymazat výpis v okně (log karty zůstává)">${icon("x")}Vymazat</button>
        </div>
      </div>
      <form class="fw-in" id="fw-inform" autocomplete="off">
        <span class="fw-prompt">›</span><input id="fw-in" placeholder="Příkaz pro zařízení, např. INFO – Enter odešle, ↑ ↓ historie" spellcheck="false" disabled>
        <button type="button" class="plain small" data-q="INFO">INFO</button><button type="button" class="plain small" data-q="HELP">HELP</button>
        <button class="ghost small" id="fw-send" disabled>Odeslat</button>
      </form>
    </div>
    <div class="modal-f">
      <button class="plain small left" id="fw-term" ${f.project ? "" : "hidden"} title="Otevře PowerShell s aktivním ESP-IDF ve složce projektu (menuconfig, vlastní příkazy)">${icon("log")}Terminál ESP-IDF</button>
      <button class="plain small${f.project ? "" : " left"}" id="fw-folder" ${monOnly ? "hidden" : ""}>${icon("folder")}Složka buildu</button>
      <button class="ghost" id="fw-hide">Skrýt</button>
      <button class="ghost" id="fw-reset" hidden title="Restart zařízení (RTS) – jako Ctrl+T Ctrl+R v idf.py monitor">Reset zařízení</button>
      <button class="danger" id="fw-stop" hidden>Zastavit</button>
      <button class="fw-btn" id="fw-go">${icon("chip")}Spustit</button>
    </div>`, (box, close) => {
    st.box = box; st.close = close;
  }, true);
  const box = st.box;
  box.classList.add("fw");
  const variant = () => f.variants[st.vi];
  const drawCtl = () => {
    box.querySelectorAll("#fw-var [data-v]").forEach((b) => b.setAttribute("aria-pressed", +b.dataset.v === st.vi));
    const v = variant();
    if (v?.noFlash && st.action !== "build" && st.action !== "monitor") st.action = "build";
    box.querySelectorAll("#fw-act [data-a]").forEach((b) => {
      b.setAttribute("aria-pressed", b.dataset.a === st.action);
      b.disabled = !!v?.noFlash && (b.dataset.a === "flash" || b.dataset.a === "flashmon");
      b.title = b.disabled ? "Varianta má zakázané nahrávání (PROD)" : "";
    });
    $("#fw-cleanpath", box).textContent = `${v?.buildDir || "build"}\\sdkconfig`;
    $("#fw-reconfcmd", box).textContent = `${v?.script || "tools\\build.cmd"} reconfigure`;
    box.querySelectorAll(".fw-clean").forEach((el) => (el.hidden = st.action === "monitor" || monOnly));
    const needPort = st.action !== "build";
    $("#fw-port", box).disabled = !needPort; $("#fw-ports", box).disabled = !needPort;
    $("#fw-go", box).innerHTML = `${icon("chip")}${{ build: "Build", flash: "Build + Nahrát", flashmon: "Build + Nahrát + Monitor", monitor: "Otevřít monitor" }[st.action]}`;
  };
  const loadPorts = async () => {
    const sel = $("#fw-port", box);
    try {
      const r = await api("GET", "/api/serialports");
      const ports = r.ports || [];
      if (st.port && !ports.some((p) => p.name === st.port)) ports.unshift({ name: st.port, desc: "není připojený" });
      sel.innerHTML = ports.length ? ports.map((p) => `<option value="${esc(p.name)}" ${p.name === st.port ? "selected" : ""}>${esc(p.name)}${p.desc ? " – " + esc(p.desc) : ""}</option>`).join("") : `<option value="">žádný COM port</option>`;
      if (!st.port && ports.length) st.port = ports[0].name;
      $("#fw-portnote", box).textContent = ports.length ? "" : "Připoj zařízení přes USB a dej ↻";
    } catch (e) { $("#fw-portnote", box).textContent = e.message; }
  };
  box.querySelectorAll("#fw-var [data-v]").forEach((b) => (b.onclick = () => { st.vi = +b.dataset.v; drawCtl(); }));
  box.querySelectorAll("#fw-act [data-a]").forEach((b) => (b.onclick = () => { if (!b.disabled) { st.action = b.dataset.a; drawCtl(); } }));
  $("#fw-port", box).onchange = (e) => (st.port = e.target.value);
  // Clean config / Reconfigure: the card remembers them for next time (also without running)
  const saveChecks = () => { f.lastClean = $("#fw-clean", box).checked; f.lastReconfigure = $("#fw-reconf", box).checked; scheduleSave(t); };
  $("#fw-clean", box).onchange = saveChecks; $("#fw-reconf", box).onchange = saveChecks;
  $("#fw-ports", box).onclick = loadPorts;
  $("#fw-hide", box).onclick = () => st.close();
  $("#fw-term", box).onclick = () => api("POST", `/api/tabs/${t.id}/fw/terminal`).catch((e) => toast(e.message, true));
  $("#fw-folder", box).onclick = () => {
    const v = variant();
    const p = f.project.replace(/[\\/]+$/, "") + "\\" + (v?.buildDir || "");
    api("POST", "/api/openfolder", { path: p }).catch(() => api("POST", "/api/openfolder", { path: f.project }).catch((e) => toast(e.message, true)));
  };
  $("#fw-stop", box).onclick = () => api("POST", `/api/tabs/${t.id}/fw/stop`).catch(() => {});
  $("#fw-reset", box).onclick = () => api("POST", `/api/tabs/${t.id}/fw/reset`).catch((e) => toast(e.message, true));
  $("#fw-go", box).onclick = async () => {
    try {
      await flushSave(t);
      if (variant() && !variant().id) await saveTab(t); // fetch the id assigned by the server
      const vid = variant()?.id || "";
      let stopDropbox = false;
      if (st.action !== "monitor") { const g = await dropboxGate(t); if (!g) return; stopDropbox = g.stop; }
      await api("POST", `/api/tabs/${t.id}/fw/run`, { stopDropbox, variant: vid, action: st.action, port: st.port, cleanCfg: $("#fw-clean", box).checked, reconfigure: $("#fw-reconf", box).checked });
      f.lastVariant = vid; f.lastAction = st.action; if (st.port) f.lastPort = st.port;
      if (st.action !== "monitor") { f.lastClean = $("#fw-clean", box).checked; f.lastReconfigure = $("#fw-reconf", box).checked; } // remembered per card
      st.seen = 0; $("#fw-log", box).innerHTML = "";
      S.fwRunning[t.id] = "building";
      poll();
    } catch (e) { toast(e.message, true); }
  };
  const send = async (txt) => {
    txt = (txt ?? $("#fw-in", box).value).trim();
    if (!txt) return;
    try {
      await api("POST", `/api/tabs/${t.id}/fw/input`, { text: txt });
      if (st.hist[st.hist.length - 1] !== txt) st.hist.push(txt);
      st.hi = st.hist.length; $("#fw-in", box).value = "";
      setTimeout(poll, 80);
    } catch (e) { toast(e.message, true); }
  };
  $("#fw-inform", box).onsubmit = (e) => { e.preventDefault(); send(); };
  box.querySelectorAll("[data-q]").forEach((b) => (b.onclick = () => send(b.dataset.q)));
  $("#fw-in", box).onkeydown = (e) => {
    if (e.key === "ArrowUp" && st.hist.length) { st.hi = Math.max(0, st.hi - 1); e.target.value = st.hist[st.hi] || ""; e.preventDefault(); }
    if (e.key === "ArrowDown" && st.hist.length) { st.hi = Math.min(st.hist.length, st.hi + 1); e.target.value = st.hist[st.hi] || ""; e.preventDefault(); }
  };
  const drawRun = (j) => {
    const running = !!j && !j.none && j.phase !== "done";
    const mon = running && j.phase === "monitor" && j.portOpen;
    st.running = running;
    $("#fw-ctl", box).disabled = running;
    $("#fw-go", box).hidden = running;
    $("#fw-stop", box).hidden = !running;
    $("#fw-stop", box).textContent = mon ? "Ukončit monitor" : "Zastavit";
    $("#fw-reset", box).hidden = !mon;
    $("#fw-in", box).disabled = !mon; $("#fw-send", box).disabled = !mon;
    box.querySelectorAll("[data-q]").forEach((b) => (b.disabled = !mon));
    const sel = window.getSelection();
    if (mon && document.activeElement?.tagName !== "INPUT" && (!sel || sel.isCollapsed) && document.activeElement?.id !== "fw-log") $("#fw-in", box).focus();
    if (j && !j.none) {
      $("#fw-steps", box).innerHTML = (j.steps || []).map((s) => `<li class="${s.state}"><span class="ic">${{ ok: "✓", failed: "✕", running: "●", skipped: "–", pending: "" }[s.state] || ""}</span><span>${esc(s.name)}</span>${s.note ? `<span class="note">${esc(s.note)}</span>` : ""}</li>`).join("");
    }
    if (running) { S.fwRunning[t.id] = j.phase; } else if (S.fwRunning[t.id]) {
      delete S.fwRunning[t.id];
      if (j && !j.none) toast(`FW ${({ ok: "hotovo", failed: "se nepovedl – viz výpis", cancelled: "přerušen" })[j.result] || "skončil"}`, j.result === "failed");
    }
  };
  const addLog = (lines) => {
    if (!lines?.length) return;
    const ol = $("#fw-log", box);
    // while text is being selected in the log, keep it still (no scrolling, no trimming)
    const sel = window.getSelection();
    const selecting = sel && !sel.isCollapsed && ol.contains(sel.anchorNode);
    const atBottom = !selecting && ol.scrollHeight - ol.scrollTop - ol.clientHeight < 40;
    const frag = document.createDocumentFragment();
    for (const l of lines) {
      const li = document.createElement("li");
      li.innerHTML = `<time>${new Date(l.t).toLocaleTimeString(I18N.locale())}</time><span class="${l.kind}">${esc(l.text)}</span>`;
      frag.append(li);
    }
    ol.append(frag);
    if (!selecting) while (ol.children.length > 3000) ol.firstChild.remove();
    if (atBottom) ol.scrollTop = ol.scrollHeight;
  };
  const copyText = async (txt) => {
    try { await navigator.clipboard.writeText(txt); return true; } catch {}
    const ta = document.createElement("textarea");
    ta.value = txt; ta.style.position = "fixed"; ta.style.opacity = "0";
    document.body.append(ta); ta.select();
    let ok = false; try { ok = document.execCommand("copy"); } catch {}
    ta.remove(); return ok;
  };
  $("#fw-copy", box).onclick = async () => {
    const lines = [...$("#fw-log", box).children].map((li) => `${li.querySelector("time")?.textContent || ""}  ${li.querySelector("span")?.textContent || ""}`);
    if (!lines.length) return toast("Výpis je prázdný");
    toast(await copyText(lines.join("\r\n")) ? `Zkopírováno ${fmtNum(lines.length)} řádků` : "Kopírování se nepovedlo – označ text a dej Ctrl+C", false);
  };
  $("#fw-clear", box).onclick = () => { $("#fw-log", box).innerHTML = ""; $("#fw-in", box).focus(); };
  const poll = async () => {
    clearTimeout(st.timer);
    if (fwWin !== st || !document.body.contains(box)) return;
    let j;
    try { j = await api("GET", `/api/tabs/${t.id}/fw?log=${st.seen}`); } catch { st.timer = setTimeout(poll, 1000); return; }
    if (fwWin !== st) return;
    if (!j.none) { addLog(j.log); st.seen = j.logTotal; }
    drawRun(j);
    if (!j.none && j.phase !== "done") st.timer = setTimeout(poll, j.phase === "monitor" ? 250 : 400);
  };
  // closing the window keeps the session running (tile shows it)
  const ov = $("#overlay");
  const obs = new MutationObserver(() => {
    if (ov.hidden || !ov.contains(box)) {
      obs.disconnect(); clearTimeout(st.timer);
      if (fwWin === st) fwWin = null;
      if (st.running) { fwBgKick(t.id); toast(S.fwRunning[t.id] === "monitor" ? "Monitor běží dál – otevřeš ho na dlaždici / kartě" : "FW build běží dál na pozadí"); }
      if (S.view === "overview") renderOverview(); else updateTile(t.id);
    }
  });
  obs.observe(ov, { childList: true, attributes: true });
  drawCtl(); loadPorts(); poll();
}

/* ------------------------------------------------------------------ builds: progress window */

let bw = null; // { id, logSeen, timer }
function openBuildWindow(id, kind) {
  const t = S.tabs.find((x) => x.id === id);
  if (!t) return;
  if (!kind) kind = S.jobData[id]?.backup ? "backup" : "build";
  if (bw) clearTimeout(bw.timer);
  bw = { id, logSeen: 0, timer: null };
  modal(`
    <div class="modal-h bw-h"><h3 id="bw-title">${kind === "backup" ? "Záloha" : scriptOnly(t) ? "Skript" : "Build"} „${esc(t.name)}“</h3><p id="bw-sub">Spouštím…</p></div>
    <div class="modal-b bw-b">
      <ol class="bsteps" id="bw-steps"></ol>
      <div class="bw-prog" id="bw-prog" hidden><span class="fill"></span></div>
      <ol class="act-log bw-log" id="bw-log" translate="no"></ol>
    </div>
    <div class="modal-f">
      <button class="plain small left" id="bw-fulllog">${icon("log")}Celý log</button>
      <button class="ghost" id="bw-hide">Skrýt</button>
      <button class="danger" id="bw-stop">Zastavit</button>
    </div>`, (box, close) => {
    $("#bw-hide", box).onclick = close;
    $("#bw-stop", box).onclick = () => { api("POST", `/api/tabs/${id}/cancel`).catch(() => {}); toast("Zastavuji…"); };
    $("#bw-fulllog", box).onclick = () => api("POST", `/api/tabs/${id}/openlog`).catch((e) => toast(e.message, true));
  }, true);
  $("#overlay .modal").classList.add("bw");
  pollBuildWindow();
}

async function pollBuildWindow() {
  if (!bw) return;
  const { id } = bw;
  const bwTab = S.tabs.find((v) => v.id === id);
  const box = $("#overlay .modal.bw");
  if (!box) { bw = null; return; }
  let j;
  try { j = await api("GET", `/api/tabs/${id}/job?log=${bw.logSeen}`); } catch { bw.timer = setTimeout(pollBuildWindow, 1000); return; }
  if (!bw || bw.id !== id || !$("#overlay .modal.bw")) return;
  if (j.none) { $("#bw-sub").textContent = "Nic neběží."; return; }
  const icons = { pending: "○", running: "●", ok: "✓", failed: "✕", skipped: "–" };
  $("#bw-steps").innerHTML = (j.steps || []).map((x) => `<li class="${x.state}"><span class="si">${icons[x.state] || ""}</span><span class="sn">${esc(x.name)}</span>${x.note ? `<span class="note">${esc(x.note)}</span>` : ""}</li>`).join("");
  const phases = j.backup
    ? { scanning: `Prohledávám… ${fmtNum(j.scanFiles)} souborů`, zipping: `Balím ZIP ${fmtNum(j.filesDone)} / ${fmtNum(j.filesTotal)} souborů · ${fmtBytes(j.bytesDone)} / ${fmtBytes(j.bytesTotal)}${j.active?.length ? " · " + j.active[0] : ""}` }
    : { building: "Build…", connecting: "Připojuji k serveru…", scanning: `Porovnávám… ${fmtNum(j.scanFiles)} souborů`, uploading: `Nahrávám ${fmtNum(j.filesDone)} / ${fmtNum(j.filesTotal)} souborů · ${fmtBytes(j.bytesDone)} / ${fmtBytes(j.bytesTotal)}` };
  const prog = $("#bw-prog");
  prog.classList.toggle("bk", !!j.backup);
  if (j.phase === "uploading" || j.phase === "zipping") {
    prog.hidden = false;
    const pct = j.bytesTotal > 0 ? (j.bytesDone / j.bytesTotal) * 100 : (j.filesTotal ? (j.filesDone / j.filesTotal) * 100 : 0);
    prog.querySelector(".fill").style.width = Math.min(100, pct).toFixed(1) + "%";
  } else prog.hidden = true;
  const sub = $("#bw-sub");
  if (j.phase !== "done") { sub.className = ""; sub.textContent = phases[j.phase] || ""; }
  else {
    const bad = (j.steps || []).find((x) => x.state === "failed");
    const r = j.backup
      ? j.result === "ok" ? ["ok", `Záloha hotová – ${fmtNum(j.filesDone)} souborů (${fmtBytes(j.bytesDone)} před zabalením)`]
        : j.result === "errors" ? ["warn", `Záloha hotová s chybami – ${fmtNum(j.filesFail)} souborů vynecháno (viz výpis)`]
        : j.result === "cancelled" ? ["warn", "Záloha přerušena – nedokončený ZIP smazán"] : ["err", bad ? `Selhalo: ${bad.note || bad.name}` : "Záloha se nepovedla"]
      : j.result === "ok" ? ["ok", j.syncAfter ? `Hotovo – build i synchronizace (${fmtNum(j.filesDone)} souborů nahráno)` : bwTab && syncReady(bwTab) ? "Build hotový – nic se nenahrávalo" : "Hotovo – doběhlo bez chyby"]
      : j.result === "cancelled" ? ["warn", "Přerušeno"] : ["err", bad ? `Selhalo: ${bad.note || bad.name}` : "Nepovedlo se – viz výpis"];
    sub.className = "bw-res " + r[0]; sub.textContent = r[1];
    $("#bw-stop").hidden = true;
    $("#bw-hide").textContent = "Zavřít";
  }
  if (j.log?.length) {
    const ol = $("#bw-log");
    const atBottom = ol.scrollHeight - ol.scrollTop - ol.clientHeight < 40;
    const frag = document.createDocumentFragment();
    for (const l of j.log) {
      const li = document.createElement("li");
      li.innerHTML = `<time>${new Date(l.t).toLocaleTimeString(I18N.locale())}</time><span class="${l.kind}">${esc(l.text)}</span>`;
      frag.append(li);
    }
    ol.append(frag);
    while (ol.children.length > 3000) ol.firstChild.remove();
    if (atBottom) ol.scrollTop = ol.scrollHeight;
  }
  bw.logSeen = j.logTotal;
  if (j.phase !== "done") bw.timer = setTimeout(pollBuildWindow, 350);
}

/* ------------------------------------------------------------------ read-only view */

function startEdit(t) {
  if (running(t.id)) return toast("Během synchronizace nejde kartu upravovat", true);
  beginEdit(t);
  renderTabs();
  renderMain();
  checkLocal();
}

async function finishEdit(t) { return commitEdit(t); }

function renderView(t) {
  const c = t.conn;
  const port = c.port || DEFPORT[c.protocol];
  const yes = (b) => (b ? "ano" : "ne");
  const chips = (list) => list.map((x) => `<span class="chip ro">${esc(x)}</span>`).join("");
  const pairs = t.pairs.length ? t.pairs.map((p) => `
      <li class="vpair${p.enabled ? "" : " off"}">
        <span class="state" title="${p.enabled ? "Synchronizuje se" : "Vypnuto – při synchronizaci se přeskočí"}">${p.enabled ? "" : "vypnuto"}</span>
        <div class="vpath local${p.local && S.localOK[p.local] === false ? " missing" : ""}" data-local="${esc(p.local)}">
          <span>${esc(p.local || "(složka v PC není vybraná)")}</span>
          ${p.local ? `<button class="icon-btn small" data-open="${esc(p.local)}" title="Otevřít v Průzkumníku">${icon("folder")}</button>` : ""}
        </div>
        <span class="flow">${icon("arrow")}</span>
        <div class="vpath"><span>${esc(p.remote || "(domovská složka)")}</span></div>
        ${p.excludes?.length ? `<div class="vex"><span class="lbl">Vyloučeno:</span>${chips(p.excludes)}</div>` : ""}
      </li>`).join("") : `<li class="note">Žádná dvojice složek.</li>`;
  const unset = cardUnset(t);
  const ftp = syncSet(t), mm = mainMode(t), ready = syncReady(t);
  const ftpHalf = ftp && !ready;
  const extra = `${hasBackups(t) ? `<button class="backup-btn${mm ? " small" : ""}" id="btn-backup">${icon("archive")}Zálohovat</button>` : ""}${hasGit(t) ? `<button class="git-btn${mm ? " small" : ""}" id="btn-git">${icon("branch")}GitHub</button>` : ""}${hasFw(t) ? `<button class="fw-btn${mm ? " small" : ""}" id="btn-fw">${icon("chip")}FW build</button>` : ""}`;

  $("#main").innerHTML = `
  <div class="page">
    <section class="hero">
      <h1 class="title" translate="no">${esc(t.name)}</h1>
      <div class="hero-actions">
        ${mm ? `<button class="sync" id="btn-sync"><span class="fill"></span><span class="lbl">${icon(mm === "build" ? "gear" : "up")}<span id="sync-text">${mainLabel(t)}</span></span></button>` : extra ? `<div class="row big-extra">${extra}</div>` : ""}
        <div class="row">
          ${ready ? `<button class="plain small" id="btn-preview">Náhled změn</button>` : ""}
          ${hasBuilds(t) && ready ? `<button class="plain small" id="btn-onlybuild">${scriptOnly(t) ? "Jen skript" : "Jen build"}</button><button class="plain small" id="btn-onlysync">Jen nahrát</button>` : ""}
          <button class="plain small" id="btn-log">${icon("log")}Log</button>
          <button class="plain small" id="btn-edit">${icon("gear")}Upravit kartu</button>
          <button class="plain small" id="btn-dup" title="Nová karta se stejným nastavením">${icon("copy")}Duplikovat</button>
        </div>
        ${mm && extra ? `<div class="row">${extra}</div>` : ""}
      </div>
      ${hasMon(t) ? `<div class="hero-left"><button class="mon-btn small${S.fwRunning?.[t.id] === "monitor" ? " live" : ""}" id="btn-mon" title="Monitor – sériová konzole zařízení (COM port)">${icon("log")}Monitor</button></div>` : ""}
      <div class="hero-sub">
        ${groupOf(t) ? `<span><span class="k">Skupina</span> ${esc(groupName(groupOf(t)))}</span>` : ""}
        ${ftp ? `<span>${c.host ? `<span class="k">Server</span> ${esc(c.user ? c.user + "@" : "")}${esc(c.host)} · ${PROTO[c.protocol]}` : `<span class="k">Server zatím není nastavený</span>`}</span>
        <span class="last${t.lastSync && t.lastSync.result !== "ok" ? " err" : ""}" id="last">${lastText(t)}</span>` : `<span>${esc(cardKinds(t))}</span>`}
      </div>
    </section>

    ${unset ? `<div class="setup-hint">Karta ještě není nastavená – může synchronizovat na FTP, buildovat, zálohovat, posílat na GitHub nebo buildovat a nahrávat firmware ESP32 (stačí cokoli z toho). <button class="primary small" id="btn-edit2">${icon("gear")}Nastavit kartu</button></div>`
      : ftpHalf ? `<div class="setup-hint">Synchronizace není dokončená – ${esc(syncHalfText(t))}. <button class="primary small" id="btn-edit2">${icon("gear")}Upravit kartu</button></div>` : ""}

    ${ftp ? `<section class="sec">
      <div class="sec-h"><h2>Složky</h2><span class="hint">${t.pairs.filter((p) => p.enabled).length} z ${t.pairs.length} zapnuto</span></div>
      <ul class="vpairs">${pairs}</ul>
    </section>

    <section class="sec">
      <div class="sec-h"><h2>Server</h2></div>
      <dl class="vgrid">
        <dt>Protokol</dt><dd>${PROTO[c.protocol]}</dd>
        <dt>Server</dt><dd class="mono">${esc(c.host || "—")}${c.host ? `:${port}` : ""}</dd>
        <dt>Uživatel</dt><dd class="mono">${esc(c.user || "—")}</dd>
        <dt>Heslo</dt><dd>${c.hasPass ? "uloženo" : "nezadáno"}</dd>
        ${c.protocol === "sftp" && c.keyFile ? `<dt>Klíč</dt><dd class="mono">${esc(c.keyFile)}</dd>` : ""}
        ${c.protocol === "sftp" && c.hostKey ? `<dt>Klíč serveru</dt><dd class="mono">${esc(c.hostKey)}</dd>` : ""}
        <dt>Přes dočasný soubor</dt><dd>${yes(t.safeUpload)}</dd>
        ${c.protocol !== "sftp" && c.disableEPSV ? `<dt>EPSV</dt><dd>vypnuto</dd>` : ""}
        ${c.protocol.startsWith("ftps") && c.insecureTLS ? `<dt>Certifikát</dt><dd>chyba se ignoruje</dd>` : ""}
      </dl>
      <div class="test-row" style="margin-top:12px">
        <button class="ghost small" id="btn-test" ${c.host ? "" : "disabled"}>${icon("server")}Otestovat připojení</button>
        <span class="test-msg" id="test-msg"></span>
      </div>
    </section>` : ""}

    ${hasBuilds(t) ? `<section class="sec">
      <div class="sec-h"><h2>${scriptOnly(t) ? "Skript" : "Build"}</h2><span class="hint">${t.lastBuild ? lastBuildText(t) : ftp ? "pustí se před synchronizací v tomto pořadí" : "pustí se v tomto pořadí"}</span></div>
      <ol class="vbuilds">${cardBuilds(t).map((b) => `<li><span class="bn" translate="no">${esc(b.name)}</span><span class="note">${esc(b.config)}${b.crlf ? " · CRLF" : ""}${b.postBuild ? " · post-build" : ""}${b.postBuild ? ` · info: ${cardBuildInfo(t, b) ? "„" + esc(cardBuildInfo(t, b)) + "“" : "bez info"}` : ""}</span>
        <span class="sw-line">${b.defines.map((d) => `<span class="chip ro on">${esc(d)}</span>`).join("") || `<span class="note">bez přepínačů</span>`}</span></li>`).join("")}${hasLight(t) ? `<li><span class="bn">${esc(lightName(t))}</span><span class="note">${scriptOnly(t) ? "" : "skript · "}${esc(lightKind(t))}${t.buildLight.args ? " · " + esc(t.buildLight.args) : ""}</span><span class="sw-line"><span class="note mono">${esc(t.buildLight.script)}</span></span></li>` : ""}</ol>
    </section>` : ""}

    ${(t.backups || []).length ? `<section class="sec">
      <div class="sec-h"><h2>Zálohování</h2><span class="hint" id="last-backup">${lastBackupText(t)}</span></div>
      <ul class="vpairs">${(t.backups || []).map((b) => `
        <li class="vpair${b.enabled ? "" : " off"}">
          <span class="state">${b.enabled ? "" : "vypnuto"}</span>
          <div class="vpath local${b.source && S.localOK[b.source] === false ? " missing" : ""}" data-local="${esc(b.source)}"><span>${esc(b.source || "(složka není vybraná)")}</span>
            ${b.source ? `<button class="icon-btn small" data-open="${esc(b.source)}" title="Otevřít v Průzkumníku">${icon("folder")}</button>` : ""}</div>
          <span class="flow bflow">${icon("arrow")}</span>
          <div class="vpath"><span>${esc((b.target || "(cíl není vybraný)").replace(/[\\/]+$/, ""))}\\${esc(zipPrefix(b))}_…zip</span>
            ${b.target ? `<button class="icon-btn small" data-open="${esc(b.target)}" title="Otevřít cílovou složku">${icon("folder")}</button>` : ""}</div>
          ${b.excludes?.length ? `<div class="vex"><span class="lbl">Vyloučeno:</span>${b.excludes.map((x) => `<span class="chip ro">${esc(x)}</span>`).join("")}</div>` : ""}
        </li>`).join("")}</ul>
      ${(t.backupExcludes || []).length ? `<div class="vex" style="margin-top:8px"><span class="lbl">Ve všech zálohách vyloučeno:</span>${t.backupExcludes.map((x) => `<span class="chip ro">${esc(x)}</span>`).join("")}</div>` : ""}
    </section>` : ""}

    ${(t.git || []).length ? `<section class="sec">
      <div class="sec-h"><h2>GitHub</h2><span class="hint" id="last-git">${lastGitText(t)}</span></div>
      <ul class="vgit">${(t.git || []).map((g) => `
        <li class="${g.enabled ? "" : "off"}" data-gpath="${esc(g.path)}">
          ${icon("branch")}<div class="vpath local"><span>${esc(g.path || "(složka není vybraná)")}</span>
            ${g.path ? `<button class="icon-btn small" data-open="${esc(g.path)}" title="Otevřít v Průzkumníku">${icon("folder")}</button>` : ""}</div>
          <span class="ginfo note">${g.enabled ? "…" : "vypnuto"}</span>
        </li>`).join("")}</ul>
      ${t.gitPull ? `<p class="note" style="margin-top:8px">Před pushem se stáhnou změny ze serveru (pull --rebase).</p>` : ""}
    </section>` : ""}

    ${hasFw(t) ? `<section class="sec">
      <div class="sec-h"><h2>FW build (ESP-IDF)</h2><span class="hint">${esc(t.fw.lastPort ? "naposledy " + t.fw.lastPort : "")}</span></div>
      <dl class="vgrid">
        <dt>Projekt</dt><dd class="mono">${esc(t.fw.project || "—")}</dd>
        <dt>ESP-IDF</dt><dd class="mono">${esc(t.fw.idfProfile || (S.idfProfile ? S.idfProfile + " (auto)" : "nenalezeno"))}</dd>
        <dt>Varianty</dt><dd>${(t.fw.variants || []).map((v) => `<span class="chip ro">${esc(v.name)}${v.noFlash ? " · bez nahrání" : ""}</span>`).join(" ") || "—"}</dd>
        <dt>Monitor</dt><dd>${hasMon(t) ? `zapnutý · ${t.fw.baud || 115200} Bd` : "vypnutý"}</dd>
      </dl>
    </section>` : ""}

    ${ftp && (t.checklist || []).length ? `<section class="sec">
      <div class="sec-h"><h2>Kontrola před synchronizací</h2><span class="hint">potvrdíš před každou synchronizací</span></div>
      <ul class="vcheck">${t.checklist.map((x) => `<li>${icon("lock")}<span>${esc(x)}</span></li>`).join("")}</ul>
    </section>` : ""}

    ${ftp ? `<section class="sec">
      <div class="sec-h"><h2>Vyloučeno ve všech složkách</h2><span class="hint">${t.excludes.length} pravidel</span></div>
      <div class="vex wide">${t.excludes.length ? chips(t.excludes) : `<span class="note">nic</span>`}</div>
    </section>` : ""}
  </div>`;

  const m = $("#main");
  m.querySelectorAll("[data-open]").forEach((b) => b.onclick = () =>
    api("POST", "/api/openfolder", { path: b.dataset.open }).catch((e) => toast(e.message, true)));
  $("#btn-edit").onclick = () => startEdit(t);
  $("#btn-edit2")?.addEventListener("click", () => startEdit(t));
  $("#btn-dup").onclick = () => duplicateTab(t);
  $("#btn-test")?.addEventListener("click", () => testConn(t));
  $("#btn-sync")?.addEventListener("click", () => !running(t.id) && confirmSync(t));
  $("#btn-preview")?.addEventListener("click", () => runPreview(t));
  $("#btn-git")?.addEventListener("click", () => openGitWindow(t));
  $("#btn-fw")?.addEventListener("click", () => openFwWindow(t));
  $("#btn-mon")?.addEventListener("click", () => openFwWindow(t, true));
  loadGitInfo(t);
  $("#btn-onlybuild")?.addEventListener("click", () => !running(t.id) && confirmSync(t, "build"));
  $("#btn-backup")?.addEventListener("click", () => !running(t.id) && confirmBackup(t));
  $("#btn-onlysync")?.addEventListener("click", () => !running(t.id) && confirmSync(t, "sync"));
  $("#btn-log").onclick = () => api("POST", `/api/tabs/${t.id}/openlog`).catch((e) => toast(e.message, true));
  updateSyncButton();
}

function renderTabsName(t) {
  const el = document.querySelector(`.tab[data-id="${t.id}"] .name`);
  if (el) el.textContent = t.name || "…";
}

/* ------------------------------------------------------------------ actions */

async function addTab(group = "") {
  if (typeof group !== "string") group = "";
  try {
    const t = await api("POST", "/api/tabs", { name: "", group });
    S.tabs.push(t);
    beginEdit(t);
    select(t.id);
    setTimeout(() => { $("#title")?.focus(); $("#title")?.select(); }, 30);
  } catch (e) { toast(e.message, true); }
}

async function testConn(t) {
  const msg = $("#test-msg"), btn = $("#btn-test");
  btn.disabled = true; msg.className = "test-msg"; msg.textContent = "Připojuji…";
  try {
    await flushSave(t);
    const r = await api("POST", `/api/tabs/${t.id}/test`);
    msg.className = "test-msg " + (r.ok ? "ok" : "err");
    msg.innerHTML = esc(r.message) + (r.warning ? `<span class="warn">${esc(r.warning)}</span>` : "");
    if (r.hostKeyChanged) {
      const b = document.createElement("button");
      b.className = "danger small"; b.textContent = "Důvěřovat novému klíči"; b.style.marginLeft = "8px";
      b.onclick = async () => { await api("POST", `/api/tabs/${t.id}/trustkey`); t.conn.hostKey = ""; testConn(t); };
      msg.append(b);
    }
    if (r.ok) { const st = await api("GET", "/api/state"); const f = st.tabs.find((x) => x.id === t.id); if (f) t.conn.hostKey = f.conn.hostKey; }
  } catch (e) { msg.className = "test-msg err"; msg.textContent = e.message; }
  btn.disabled = false;
}

// afterBuild: the folders may not exist yet – the build creates them
function validate(t, afterBuild = false) {
  if (!t.conn.host) return "Nejdřív vyplň server";
  const on = t.pairs.filter((p) => p.enabled && p.local.trim());
  if (!on.length) return "Karta nemá žádnou zapnutou dvojici složek";
  const miss = !afterBuild && on.find((p) => S.localOK[p.local] === false);
  if (miss) return `Složka v PC neexistuje: ${miss.local}`;
  return "";
}

// mode: "buildsync" (build, then synchronize) | "sync" (only upload) | "build" (only build)
async function confirmSync(t, mode) {
  if (!mode) mode = mainMode(t) || "sync";
  const doBuild = mode !== "sync", doSync = mode !== "build";
  if (doSync) { const err = validate(t, doBuild); if (err) return toast(err, true); }
  if (doBuild && !hasBuilds(t)) return toast("Karta nemá vybraný žádný build", true);
  const on = t.pairs.filter((p) => p.enabled && p.local.trim());
  const checks = doSync ? t.checklist || [] : [];
  const bl = doBuild ? cardBuilds(t) : [];
  const lt = doBuild && hasLight(t);
  const title = lightLabel(t) && (mode === "buildsync" || (mode === "build" && !syncReady(t))) ? lightLabel(t) : (scriptOnly(t) ? { buildsync: "Skript + Synchronizovat", sync: "Synchronizovat", build: syncReady(t) ? "Jen skript" : "Spustit skript" } : { buildsync: "Build + Synchronizovat", sync: "Synchronizovat", build: syncReady(t) ? "Jen build" : "Build" })[mode];
  modal(`
    <div class="modal-h"><h3>${title} „${esc(t.name)}“?</h3>
      <p>${doSync ? `${esc(t.conn.user ? t.conn.user + "@" : "")}${esc(t.conn.host)} · ${PROTO[t.conn.protocol]}` : bl.length ? "Zkompiluje se, nic se nenahraje." : syncReady(t) ? "Spustí se skript, na server se nic nenahraje." : "Spustí se skript (Leonzilla sama nic nenahrává)."}</p></div>
    <div class="modal-b">
      ${bl.length || lt ? `<div class="bsteps-c">
        <p class="cl-h">${!bl.length ? "Skript:" : bl.length + (lt ? 1 : 0) > 1 ? "Buildy v tomto pořadí:" : "Build:"}</p>
        <ol>${bl.map((b) => `<li><b translate="no">${esc(b.name)}</b> <span class="note">konfigurace ${esc(b.config)}${b.crlf ? " · CRLF" : ""}${b.postBuild ? " · post-build" : ""}</span>
          ${b.postBuild ? `<label class="binfo"><span>Info k verzi</span><input data-binfo="${esc(b.id)}" value="${esc(cardBuildInfo(t, b))}" maxlength="80" spellcheck="false" placeholder="prázdné = bez info" title="Karta si text zapamatuje pro příště"><button type="button" class="icon-btn small" data-binfo-reset="${esc(b.id)}" title="Vrátit výchozí text z nastavení buildu: ${esc(buildInfo(b) || "(bez info)")}">↺</button></label>` : ""}
          <div class="sw-line">${b.defines.length ? b.defines.map((d) => `<span class="chip ro on">${esc(d)}</span>`).join("") : `<span class="note">žádný přepínač</span>`}</div></li>`).join("")}${lt ? `<li><b>${esc(lightName(t))}</b> <span class="note">${scriptOnly(t) ? "" : "skript · "}${esc(lightKind(t))}${t.buildLight.args ? " · " + esc(t.buildLight.args) : ""}</span></li>` : ""}</ol>
        <div id="m-bwarn"></div>
        ${doSync ? `<p class="note">Synchronizace začne, až všechny buildy doběhnou bez chyby.</p>` : ""}
      </div>` : ""}
      ${checks.length ? `<div class="checklist" id="m-checks">
        <p class="cl-h">Než se pustíš do nahrávání, potvrď:</p>
        ${checks.map((c, i) => `<label class="check cl"><input type="checkbox" data-ci="${i}"><span>${esc(c)}</span></label>`).join("")}
      </div>` : ""}
      ${doSync ? `<ul class="confirm-pairs">${on.map((p) => `<li><span>${esc(p.local)}</span>${icon("arrow")}<span>${esc(p.remote || "(domovská složka)")}</span></li>`).join("")}</ul>
      <div class="rule">Nahrají se nové a změněné soubory. Na serveru se nic nemaže.</div>
      <label class="check"><input type="checkbox" id="m-force">Nahrát znovu všechny soubory (bez porovnání)</label>` : ""}
    </div>
    <div class="modal-f"><button class="ghost" data-close>Zrušit</button>
      <button class="sync" id="m-ok" style="min-width:0;height:40px"><span class="lbl">${icon("up")}${title}</span></button></div>`,
    (box, close) => {
      const ok = $("#m-ok", box);
      box.querySelectorAll("[data-binfo-reset]").forEach((btn) => (btn.onclick = (e) => {
        e.preventDefault();
        const b = builds().find((x) => x.id === btn.dataset.binfoReset);
        const inp = box.querySelector(`[data-binfo="${btn.dataset.binfoReset}"]`);
        if (b && inp) { inp.value = buildInfo(b); inp.focus(); }
      }));
      const boxes = [...box.querySelectorAll("#m-checks input")];
      const refresh = () => {
        const done = boxes.every((b) => b.checked);
        ok.disabled = !done;
        ok.title = done ? "" : "Nejdřív odškrtni všechny kontrolní body";
        boxes.forEach((b) => b.closest("label").classList.toggle("done", b.checked));
      };
      boxes.forEach((b) => (b.onchange = refresh));
      refresh();
      (boxes[0] || ok).focus();
      ok.onclick = () => {
        if (!boxes.every((b) => b.checked)) return;
        const force = $("#m-force", box)?.checked || false;
        const buildInfoRun = {};
        box.querySelectorAll("[data-binfo]").forEach((inp) => {
          const id = inp.dataset.binfo, v = inp.value.replace(/\s+/g, " ").trim().slice(0, 80);
          buildInfoRun[id] = v;
          // the card remembers the text; same as the build setting = nothing to remember
          const b = builds().find((x) => x.id === id);
          t.buildInfo = { ...(t.buildInfo || {}) };
          if (b && v === buildInfo(b)) delete t.buildInfo[id]; else t.buildInfo[id] = v;
        });
        if (Object.keys(buildInfoRun).length) scheduleSave(t, 0);
        close();
        startJob(t, { force, build: doBuild, sync: doSync, buildInfo: buildInfoRun });
      };
      if (bl.length) buildWarnings(bl).then((html) => { const el = $("#m-bwarn", box); if (el) el.innerHTML = html; });
    });
}

// warnings shown before a build: new switches in Predefines.inc, missing LEONZILLA block, RAD Studio running
async function buildWarnings(bl) {
  const out = [];
  if (bl.some((b) => b.crlf)) {
    try { if ((await api("GET", "/api/radstudio")).running) out.push("RAD Studio běží. Před převodem konců řádků ho zavři <b>bez uložení</b>, jinak může soubory přepsat."); } catch {}
  }
  for (const b of bl) {
    try {
      const r = await api("POST", "/api/builds/inspect", { project: b.project, incFile: b.incFile, config: b.config, platform: b.platform });
      const inc = r.inc;
      if (inc?.exists && !inc.hasBlock) out.push(`${esc(b.name)}: ${esc(inc.path)} nemá horní část v bloku <code>{$IFNDEF LEONZILLA}</code> – build skončí chybou.`);
      const known = new Set(b.switches.map((x) => x.name.toUpperCase()));
      const fresh = (inc?.switches || []).filter((x) => !known.has(x.name.toUpperCase())).map((x) => x.name);
      if (fresh.length) out.push(`${esc(b.name)}: v Predefines.inc ${fresh.length === 1 ? "přibyl přepínač" : "přibyly přepínače"} <b>${fresh.map(esc).join(", ")}</b> – v buildu ${fresh.length === 1 ? "není nastavený" : "nejsou nastavené"} (Nastavení aplikace → Buildy → Načíst přepínače).`);
      if (r.configError) out.push(`${esc(b.name)}: ${esc(r.configError)}`);
    } catch (e) { out.push(`${esc(b.name)}: ${esc(e.message)}`); }
  }
  return out.map((x) => `<p class="bwarn">${x}</p>`).join("");
}

async function runPreview(t) {
  const err = validate(t);
  if (err) return toast(err, true);
  startJob(t, { preview: true });
}

async function startJob(t, o) {
  const preview = !!o.preview;
  let stopDropbox = false;
  if (o.build && !preview) {
    const g = await dropboxGate(t);
    if (!g) return;
    stopDropbox = g.stop;
  }
  try {
    await flushSave(t);
    await api("POST", `/api/tabs/${t.id}/sync`, { stopDropbox, preview, force: !!o.force, build: !!o.build, backup: !!o.backup, notes: o.notes || [], names: o.names || [], only: o.only || null, buildInfo: o.buildInfo || null, sync: o.sync !== false });
    S.jobs[t.id] = o.backup ? "scanning" : o.build ? "building" : "connecting";
    if (o.build || o.backup) openBuildWindow(t.id, o.backup ? "backup" : "build");
    S.jobData[t.id] = null;
    if (preview) delete S.checked[t.id];
    renderTabs();
    if (S.view === "card" && t.id === S.current) {
      S.logSeen = 0;
      $("#act-log").innerHTML = "";
      watchJob(t.id, true);
    } else {
      updateTile(t.id);
      bgKick();
    }
  } catch (e) { toast(e.message, true); }
}

/* ------------------------------------------------------------------ job progress */

function watchJob(id, fresh = false) {
  if (S.jobTab !== id || fresh) { S.jobTab = id; S.logSeen = 0; $("#act-log").innerHTML = ""; }
  clearTimeout(S.pollTimer);
  poll();
}

async function poll() {
  const id = S.jobTab;
  if (!id || S.view !== "card") return;
  let j;
  try { j = await api("GET", `/api/tabs/${id}/job?log=${S.logSeen}`); } catch { S.pollTimer = setTimeout(poll, 1500); return; }
  if (id !== S.jobTab || S.view !== "card") return;
  if (j.none) { hideActivity(); return; }
  const wasRunning = running(id);
  S.jobs[id] = j.phase;
  S.job = j;
  if (id === S.current) { showActivity(j); updateSyncButton(); }
  if (wasRunning !== running(id)) renderTabs();
  if (j.phase !== "done") { S.pollTimer = setTimeout(poll, 400); return; }
  if (wasRunning) jobFinished(id, j);
}

async function jobFinished(id, j, bg = false) {
  const t = S.tabs.find((x) => x.id === id);
  if (j.preview) {
    S.checked[id] = j;
    if (j.result === "failed") toast(`${t ? t.name + ": " : ""}${j.lastMessage || j.log?.at(-1)?.text || "kontrola se nepovedla"}`, true);
    else if (!bg && id === S.current) showPreview(t, j);
    updateTile(id);
    return;
  }
  delete S.checked[id];
  try {
    const st = await api("GET", "/api/state");
    const f = st.tabs.find((x) => x.id === id);
    if (f && t) { t.lastSync = f.lastSync; t.lastBuild = f.lastBuild; t.lastBackup = f.lastBackup; t.conn.hostKey = f.conn.hostKey; }
    if (j.build && !syncReady(t) && id === S.current && S.view === "card" && !editing(id)) renderView(t);
    if (j.backup && id === S.current) { const el = $("#last-backup"); if (el) el.innerHTML = lastBackupText(t); }
    if (id === S.current) { const el = $("#last"); if (el) { el.innerHTML = lastText(t); el.classList.toggle("err", t.lastSync?.result !== "ok"); } }
  } catch {}
  updateTile(id);
  const name = t ? t.name : "";
  if (j.backup) {
    if (j.result === "ok") return toast(`${name}: záloha hotová (${fmtNum(j.filesDone)} souborů)`);
    if (j.result === "cancelled") return toast(`${name}: záloha přerušena`);
    return toast(`${name}: ${j.result === "errors" ? "záloha hotová s chybami" : "záloha se nepovedla"} – viz výpis`, true);
  }
  if (j.build) {
    const bad = (j.steps || []).find((x) => x.state === "failed" && x.name !== "Synchronizace");
    if (bad) return toast(`${name}: build selhal – ${bad.note || bad.name}`, true);
    if (!j.syncAfter) return j.result === "ok" ? toast(`${name}: build hotový`) : toast(`${name}: build přerušen`, true);
  }
  if (j.result === "ok") toast(`${name}: ${j.filesDone ? `nahráno ${fmtNum(j.filesDone)} souborů` : "vše aktuální"}`);
  else if (j.result === "cancelled") toast(`${name}: přerušeno`);
  else toast(`${name}: ${j.result === "failed" ? "synchronizace se nepovedla" : "dokončeno s chybami"} – viz log`, true);
}

function updateSyncButton() {
  const t = cur();
  const btn = $("#btn-sync");
  if (!t) return;
  const j = S.job && S.jobTab === t.id ? S.job : null;
  const busy = running(t.id);
  ["#btn-preview", "#btn-edit", "#btn-onlybuild", "#btn-onlysync", "#btn-backup"].forEach((sel) => { const b = $(sel); if (b) b.disabled = busy; });
  if (!btn) return;
  const txt = $("#sync-text"), fill = btn.querySelector(".fill");
  btn.classList.toggle("busy", busy);
  btn.classList.remove("indet");
  if (!busy) { txt.textContent = mainLabel(t); fill.style.width = "0"; btn.title = ""; return; }
  btn.title = "Probíhá – zastavíš to v panelu dole";
  if (j?.backup) { txt.textContent = "Záloha…"; btn.classList.add("indet"); return; }
  if (j?.phase === "building") { const k = (j.steps || []).findIndex((x) => x.state === "running"); txt.textContent = scriptOnly(t) ? "Skript běží…" : `Build ${k + 1} / ${(j.steps || []).length}…`; btn.classList.add("indet"); return; }
  if (!j || j.phase === "connecting") { txt.textContent = "Připojuji…"; btn.classList.add("indet"); return; }
  if (j.phase === "scanning") { txt.textContent = `Porovnávám… ${fmtNum(j.scanFiles)}`; btn.classList.add("indet"); return; }
  if (j.preview) { txt.textContent = "Náhled…"; btn.classList.add("indet"); return; }
  const pct = j.bytesTotal > 0 ? Math.min(100, (j.bytesDone / j.bytesTotal) * 100) : (j.filesTotal ? (j.filesDone / j.filesTotal) * 100 : 0);
  fill.style.width = pct.toFixed(1) + "%";
  txt.textContent = `${Math.floor(pct)} % · ${fmtNum(j.filesDone)} / ${fmtNum(j.filesTotal)}`;
}

function showActivity(j) {
  const act = $("#activity");
  act.hidden = false;
  const title = $("#act-title");
  const phases = j.backup ? { scanning: "Záloha – prohledávám složky…", zipping: "Záloha – balím ZIP…" }
    : { building: "Build…", connecting: "Připojuji…", scanning: "Porovnávám složky…", uploading: "Nahrávám…" };
  if (j.phase !== "done") {
    title.className = "act-title";
    title.textContent = (j.preview ? "Náhled · " : "") + (phases[j.phase] || "");
  } else {
    const r = { ok: ["ok", j.preview ? "Náhled hotový" : "Hotovo"], errors: ["warn", "Hotovo s chybami"], cancelled: ["warn", "Přerušeno"], failed: ["err", "Nepovedlo se"] }[j.result] || ["", ""];
    title.className = "act-title " + r[0]; title.textContent = r[1];
  }
  const parts = [];
  if (j.phase === "scanning") parts.push(`${fmtNum(j.scanFiles)} souborů prověřeno`);
  if (j.phase === "uploading" || (j.phase === "done" && !j.preview && j.filesTotal)) {
    parts.push(`${fmtNum(j.filesDone)} / ${fmtNum(j.filesTotal)} souborů`, `${fmtBytes(j.bytesDone)} / ${fmtBytes(j.bytesTotal)}`);
    if (j.phase === "uploading" && j.speed > 0) {
      parts.push(fmtBytes(Math.round(j.speed)) + "/s");
      const left = (j.bytesTotal - j.bytesDone) / j.speed;
      if (isFinite(left) && left > 2) parts.push("zbývá " + fmtDur(left));
    }
  }
  if (j.filesFail) parts.push(`${fmtNum(j.filesFail)} chyb`);
  if (j.phase === "done" && j.ended) parts.push(fmtDur((new Date(j.ended) - new Date(j.started)) / 1000));
  $("#act-stats").textContent = parts.join(" · ");
  $("#act-current").textContent = j.phase === "uploading" && j.active?.length ? "↑ " + j.active.join("   ↑ ") : "";

  const btn = $("#btn-close-act");
  if (j.phase !== "done") {
    btn.title = "Zastavit"; btn.innerHTML = icon("x"); btn.dataset.mode = "stop";
    btn.className = "danger small"; btn.innerHTML = "Zastavit";
  } else {
    btn.className = "icon-btn small"; btn.title = "Skrýt"; btn.innerHTML = icon("x"); btn.dataset.mode = "hide";
  }

  if (j.log?.length) {
    const ol = $("#act-log");
    const atBottom = ol.scrollHeight - ol.scrollTop - ol.clientHeight < 30;
    const frag = document.createDocumentFragment();
    for (const l of j.log) {
      const li = document.createElement("li");
      li.innerHTML = `<time>${new Date(l.t).toLocaleTimeString(I18N.locale())}</time><span class="${l.kind}">${esc(l.text)}</span>`;
      frag.append(li);
    }
    ol.append(frag);
    while (ol.children.length > 600) ol.firstChild.remove();
    if (atBottom) ol.scrollTop = ol.scrollHeight;
  }
  S.logSeen = j.logTotal;
}

function hideActivity() { $("#activity").hidden = true; }

$("#btn-close-act").onclick = (e) => {
  const id = S.jobTab;
  if (e.currentTarget.dataset.mode === "stop" && id) {
    api("POST", `/api/tabs/${id}/cancel`).catch(() => {});
    toast("Zastavuji…");
  } else hideActivity();
};
$("#btn-open-log").onclick = () => S.jobTab && api("POST", `/api/tabs/${S.jobTab}/openlog`).catch((e) => toast(e.message, true));

function showPreview(t, j) {
  const plans = (j.plans || []).map((p) => ({ ...p, files: p.files || [], dirs: p.dirs || [] }));
  const total = plans.reduce((a, p) => a + p.files.length, 0);
  const bytes = plans.reduce((a, p) => a + p.files.reduce((b, f) => b + f.size, 0), 0);
  const dirs = plans.reduce((a, p) => a + p.dirs.length, 0);
  const reason = { new: "nový", changed: "změněný", forced: "znovu" };
  const body = plans.map((p) => `
    <div class="pv-group">
      <h4><span>${esc(p.local)}</span>${icon("arrow")}<span>${esc(p.remote)}</span></h4>
      ${p.error ? `<p class="pv-err">${esc(p.error)}</p>` : ""}
      <p class="note">${fmtNum(p.files.length)} k nahrání · ${fmtNum(p.same)} beze změny${p.dirs.length ? ` · ${fmtNum(p.dirs.length)} nových složek` : ""}${p.excluded ? ` · ${fmtNum(p.excluded)} vyloučeno` : ""}</p>
      ${p.files.length ? `<ul class="pv-list">${p.files.slice(0, 500).map((f) => `<li><span class="badge ${f.reason}">${reason[f.reason] || f.reason}</span><span>${esc(f.rel)}</span><span class="sz">${fmtBytes(f.size)}</span></li>`).join("")}${p.files.length > 500 ? `<li><span></span><span>… a dalších ${fmtNum(p.files.length - 500)}</span><span></span></li>` : ""}</ul>` : ""}
    </div>`).join("");
  modal(`
    <div class="modal-h"><h3>${total ? `K nahrání: ${fmtNum(total)} souborů (${fmtBytes(bytes)})` : "Vše je aktuální"}</h3>
      <p>${dirs ? `Vytvoří se ${fmtNum(dirs)} složek. ` : ""}Na serveru se nic nemaže.</p></div>
    <div class="modal-b">${body}</div>
    <div class="modal-f"><button class="ghost" data-close>Zavřít</button>
      ${total || dirs ? `<button class="sync" id="m-go" style="min-width:0;height:40px"><span class="lbl">${icon("up")}Synchronizovat</span></button>` : ""}</div>`,
    (box, close) => { const go = $("#m-go", box); if (go) go.onclick = () => { close(); confirmSync(t, "sync"); }; }, true);
}

/* ------------------------------------------------------------------ remote browser */

function browseRemote(t, pair, onPick) {
  if (!t.conn.host) return toast("Nejdřív vyplň server", true);
  let curPath = pair.remote || "";
  modal(`
    <div class="modal-h"><h3>Složka na serveru</h3><p>${esc(t.conn.host)}</p></div>
    <div class="modal-b">
      <div class="crumbs" id="b-crumbs"></div>
      <ul class="dirlist" id="b-list"><li class="meta">Připojuji…</li></ul>
    </div>
    <div class="modal-f">
      <button class="plain small left" id="b-new">${icon("plus")}Nová složka</button>
      <button class="ghost" data-close>Zrušit</button>
      <button class="primary" id="b-ok" disabled>Vybrat tuto složku</button>
    </div>`, (box, close) => {
    const list = $("#b-list", box);
    const go = async (path) => {
      list.innerHTML = `<li class="meta">Načítám…</li>`;
      try {
        await flushSave(t);
        const r = await api("POST", `/api/tabs/${t.id}/browse`, { path });
        curPath = r.path;
        const parts = r.path.split("/").filter(Boolean);
        $("#b-crumbs", box).innerHTML = `<button data-p="/">/</button>` + parts.map((s, i) =>
          `${i ? '<span class="sep">/</span>' : ""}<button data-p="/${parts.slice(0, i + 1).map(encodeURIComponent).join("/")}">${esc(s)}</button>`).join("");
        $("#b-crumbs", box).querySelectorAll("button").forEach((b) => b.onclick = () => go(b.dataset.p.split("/").map(decodeURIComponent).join("/")));
        list.innerHTML = (r.path !== "/" ? `<li><button data-up>${icon("up")}..</button></li>` : "") +
          (r.dirs.length ? r.dirs.map((d) => `<li><button data-d="${esc(d)}">${icon("folder")}${esc(d)}</button></li>`).join("") : `<li class="meta">Žádné podsložky</li>`) +
          (r.files ? `<li class="meta">${fmtNum(r.files)} souborů</li>` : "");
        list.querySelector("[data-up]")?.addEventListener("click", () => go(r.parent));
        list.querySelectorAll("[data-d]").forEach((b) => b.onclick = () => go((r.path === "/" ? "" : r.path) + "/" + b.dataset.d));
        $("#b-ok", box).disabled = false;
      } catch (e) {
        list.innerHTML = `<li class="meta" style="color:var(--danger)">${esc(e.message)}</li>`;
      }
    };
    $("#b-ok", box).onclick = () => { onPick(curPath); close(); };
    $("#b-new", box).onclick = async () => {
      const name = prompt(tr("Název nové složky"));
      if (!name) return;
      try { const r = await api("POST", `/api/tabs/${t.id}/mkdir`, { path: curPath, name }); go(r.path); }
      catch (e) { toast(e.message, true); }
    };
    go(curPath);
  });
}

/* ------------------------------------------------------------------ settings */

function openSettings(focus = false) {
  const focusGroups = focus === true || focus === "groups";
  const s = S.settings;
  modal(`
    <div class="modal-h"><h3>Nastavení aplikace</h3><p>Leonzilla ${esc(S.version)} · <button class="link" id="s-about" style="padding:0">O aplikaci</button> · data: <span style="font-family:var(--mono);font-size:12px" translate="no">${esc(S.dataDir)}</span></p></div>
    <div class="modal-b set-grid">
      <div class="f"><span class="lab">Vzhled</span>
        <div class="seg" id="s-theme">${[["auto", "Podle Windows"], ["light", "Světlý"], ["dark", "Tmavý"]].map(([k, v]) => `<button data-v="${k}" aria-pressed="${s.theme === k}">${v}</button>`).join("")}</div></div>
      <div class="f"><span class="lab">Jazyk · Language</span>
        <div class="seg" id="s-lang">${[["", "Podle Windows"], ["cs", "Čeština"], ["en", "English"]].map(([k, v]) => `<button data-v="${k}" aria-pressed="${(s.langSetting || "") === k}"${k ? ' translate="no"' : ""}>${v}</button>`).join("")}</div></div>
      ${S.autostartSupported ? `<div class="f"><span class="lab">Spouštění</span>
        <label class="check"><input type="checkbox" id="s-autostart" ${S.autostart ? "checked" : ""}>Spouštět Leonzillu při startu Windows (minimalizovaně)</label>
        <span class="note">Platí jen pro tento PC a uživatele Windows (zápis do registru), ne pro ostatní PC s Dropboxem. Výchozí: vypnuto.</span></div>` : ""}
      <div class="f"><label for="s-workers">Souběžné nahrávání (počet spojení)</label>
        <select id="s-workers" style="width:120px">${[1, 2, 3, 4, 5, 6, 8].map((n) => `<option ${n === s.workers ? "selected" : ""}>${n}</option>`).join("")}</select>
        <span class="note">Víc spojení = rychlejší nahrávání hodně malých souborů. Někteří provideři povolují jen 2–4.</span></div>
      <div class="f"><span class="lab">Hlavní heslo</span>
        <span class="note">${s.master ? "Je nastavené – hesla k serverům jsou zašifrovaná a při spuštění se zeptám na hlavní heslo." :
          "Není nastavené – hesla jsou v konfiguraci jen zamaskovaná (kdo má soubor, heslo z něj dostane). Hlavní heslo je zašifruje; ptám se na něj při každém spuštění."}</span>
        <div style="display:flex;gap:8px;flex-wrap:wrap;margin-top:4px">
          ${s.master ? `<input type="password" id="s-old" placeholder="Současné hlavní heslo" style="flex:1;min-width:160px">` : ""}
          <input type="password" id="s-new" placeholder="${s.master ? "Nové (prázdné = zrušit)" : "Nové hlavní heslo"}" style="flex:1;min-width:160px">
          <input type="password" id="s-new2" placeholder="Znovu nové" style="flex:1;min-width:160px">
          <button class="ghost" id="s-master">${s.master ? "Změnit" : "Nastavit"}</button>
        </div></div>
      <div class="f" id="s-pcs"><span class="lab">Více počítačů (Dropbox)</span>
        <span class="note"><b>Společné pro všechna PC</b> (<code>Leonzilla_data\\config.json</code>): karty a jejich pořadí, skupiny, servery a hesla, buildy a kompilátor, Dropbox u karet, jazyk, vzhled, souběžné nahrávání, hlavní heslo; také stav nahrání a logy.<br><b>Jen tento PC</b>: sbalené skupiny, otevřené záložky a poslední karta (<code>Leonzilla_data\\ui\\${esc(S.pc || "PC")}.json</code>), velikost a poloha okna (<code>%LOCALAPPDATA%\\Leonzilla</code>) a spouštění s Windows.</span></div>
      <div class="f" id="s-groups-box"><span class="lab">Skupiny karet</span>
        <span class="note">V Přehledu jsou skupiny pod sebou v tomto pořadí. Dlaždice mezi nimi přetáhneš myší, nebo skupinu vybereš při úpravě karty.</span>
        <ul class="glist" id="s-groups"></ul>
        <form class="gadd" id="s-gadd"><input id="s-gname" placeholder="Název nové skupiny, např. Klienti" maxlength="60"><button class="ghost">${icon("plus")}Přidat skupinu</button></form>
      </div>
      <div class="f" id="s-builds-box"><span class="lab">Buildy (RAD Studio / TMS WEB Core)</span>
        <span class="note">Build nastavíš jednou a v kartách ho jen vybereš (Upravit kartu → Build před synchronizací).</span>
        <ul class="glist blist" id="s-builds"></ul>
        <div><button class="ghost" id="s-badd">${icon("plus")}Přidat build</button></div>
        <label for="s-compiler" style="margin-top:8px">Kompilátor TMSWebCompiler.exe</label>
        <div class="path-field"><input id="s-compiler" value="${esc(s.compiler || "")}" placeholder="${esc(s.compilerFound || "nenalezen – vyber ručně")}" spellcheck="false">
          <button id="s-compiler-pick" title="Vybrat soubor">${icon("file")}</button></div>
        <label class="check" style="margin-top:8px"><input type="checkbox" id="s-ignoreout" ${s.ignoreOutput !== false ? "checked" : ""}>Výstup buildu (složku TMSWeb v projektu) vyřadit z Dropboxu</label>
        <span class="note">Dropbox jinak zamyká čerstvě zkompilované soubory a kompilace skončí chybou „soubor využívá jiný proces“. Vyřazená složka zůstane na tomto PC, z Dropboxu a ostatních PC zmizí.</span>
        <span class="note">Vypnutí nebo hlídání Dropboxu během buildu se nastavuje u každé karty zvlášť: Upravit kartu → <i>Dropbox</i>.</span>
        <label for="s-vivar" style="margin-top:8px">Proměnná prostředí pro Info k verzi</label>
        <input id="s-vivar" value="${esc(s.versionInfoVar || "")}" placeholder="LEONZILLA_VERSION_INFO" spellcheck="false" maxlength="60" style="max-width:320px">
        <span class="note">Info k verzi dostane post-build vždy v proměnné LEONZILLA_VERSION_INFO. Když ho skript projektu čte pod jiným názvem, napiš ho sem – nastaví se obě.</span>
        <span class="note" id="s-compiler-note">${s.compilerFound ? `Použije se: ${esc(s.compilerFound)}${s.compiler ? "" : " (nalezen automaticky)"}` : "Kompilátor nebyl nalezen – bez něj build nepůjde. Na PC bez TMS WEB Core jde jen synchronizace."}</span>
      </div>
    </div>
    <div class="modal-f"><button class="primary" data-close>Hotovo</button></div>`, (box) => {
    $("#s-about", box).onclick = () => openAbout();
    box.querySelectorAll("#s-lang button").forEach((b) => b.onclick = async () => {
      if ((s.langSetting || "") === b.dataset.v) return;
      try { await api("POST", "/api/settings", { theme: s.theme, workers: s.workers, lang: b.dataset.v }); location.reload(); }
      catch (e) { toast(e.message, true); }
    });
    $("#s-vivar", box).onchange = async (e) => {
      const v = e.target.value.trim();
      try { await api("POST", "/api/settings", { theme: s.theme, workers: s.workers, versionInfoVar: v }); s.versionInfoVar = v; toast(v ? `Info k verzi: LEONZILLA_VERSION_INFO + ${v}` : "Info k verzi: LEONZILLA_VERSION_INFO"); }
      catch (err) { toast(err.message, true); e.target.value = s.versionInfoVar || ""; }
    };
    box.querySelectorAll("#s-theme button").forEach((b) => b.onclick = async () => {
      s.theme = b.dataset.v; applyTheme();
      box.querySelectorAll("#s-theme button").forEach((x) => x.setAttribute("aria-pressed", x === b));
      api("POST", "/api/settings", { theme: s.theme, workers: s.workers }).catch((e) => toast(e.message, true));
    });
    const saveGroups = async (list) => {
      try {
        const r = await api("POST", "/api/groups", { groups: list });
        s.groups = r.groups;
        const ids = new Set(r.groups.map((g) => g.id));
        S.tabs.forEach((t) => { if (t.group && !ids.has(t.group)) t.group = ""; });
        drawGroups(); renderTabs(); if (S.view === "overview") renderOverview();
      } catch (e) { toast(e.message, true); }
    };
    const drawGroups = () => {
      const ul = $("#s-groups", box);
      const gs = groups();
      ul.innerHTML = gs.length ? gs.map((g, i) => {
        const n = S.tabs.filter((t) => t.group === g.id).length;
        return `<li data-i="${i}">
          <input value="${esc(g.name)}" aria-label="Název skupiny" maxlength="60">
          <span class="note">${n} ${n === 1 ? "karta" : n > 1 && n < 5 ? "karty" : "karet"}</span>
          <button class="icon-btn small" data-up title="Posunout nahoru" ${i === 0 ? "disabled" : ""}>${icon("up")}</button>
          <button class="icon-btn small" data-del title="Smazat skupinu (karty zůstanou, jen bez skupiny)">${icon("x")}</button>
        </li>`;
      }).join("") : `<li class="note">Zatím žádné skupiny.</li>`;
      ul.querySelectorAll("li[data-i]").forEach((li) => {
        const i = +li.dataset.i;
        const inp = li.querySelector("input");
        inp.onchange = () => { const list = groups().map((g) => ({ ...g })); if (inp.value.trim()) { list[i].name = inp.value.trim(); saveGroups(list); } else inp.value = list[i].name; };
        inp.onkeydown = (e) => { if (e.key === "Enter") { e.preventDefault(); inp.blur(); } };
        li.querySelector("[data-up]").onclick = () => { const list = groups().map((g) => ({ ...g })); list.splice(i - 1, 0, list.splice(i, 1)[0]); saveGroups(list); };
        li.querySelector("[data-del]").onclick = () => saveGroups(groups().filter((_, k) => k !== i));
      });
    };
    drawGroups();
    $("#s-gadd", box).onsubmit = (e) => {
      e.preventDefault();
      const name = $("#s-gname", box).value.trim();
      if (!name) return;
      $("#s-gname", box).value = "";
      saveGroups([...groups(), { id: "", name }]);
    };
    if (focusGroups) setTimeout(() => $("#s-gname", box)?.focus(), 30);
    if (focus === "builds") setTimeout(() => $("#s-builds-box", box)?.scrollIntoView({ block: "start" }), 30);
    const drawBuilds = () => {
      const ul = $("#s-builds", box);
      const bl = builds();
      ul.innerHTML = bl.length ? bl.map((b, i) => {
        const n = S.tabs.filter((t) => (t.builds || []).includes(b.id)).length;
        return `<li data-i="${i}"><span class="bname"><b translate="no">${esc(b.name)}</b><span class="note">${esc(b.config)} · ${esc((b.project || "").split(/[\\/]/).pop())} · ${b.defines.map(esc).join(", ") || "bez přepínačů"}</span></span>
          <span class="note">${n} ${n === 1 ? "karta" : n > 1 && n < 5 ? "karty" : "karet"}</span>
          <button class="ghost small" data-edit>Upravit</button></li>`;
      }).join("") : `<li class="note">Zatím žádný build.</li>`;
      ul.querySelectorAll("[data-edit]").forEach((btn) => (btn.onclick = () => openBuildEditor(builds()[+btn.closest("li").dataset.i])));
    };
    drawBuilds();
    $("#s-badd", box).onclick = () => openBuildEditor(null);
    const saveCompiler = async (v) => {
      try {
        await api("POST", "/api/settings", { theme: s.theme, workers: s.workers, compiler: v });
        s.compiler = v;
        const st = await api("GET", "/api/state");
        s.compilerFound = st.settings.compilerFound;
        const note = $("#s-compiler-note", box);
        if (note) note.textContent = s.compilerFound ? `Použije se: ${s.compilerFound}` : "Kompilátor na této cestě není.";
      } catch (e) { toast(e.message, true); }
    };
    $("#s-compiler", box).onchange = (e) => saveCompiler(e.target.value.trim());
    $("#s-ignoreout", box).onchange = (e) => {
      s.ignoreOutput = e.target.checked;
      api("POST", "/api/settings", { theme: s.theme, workers: s.workers, ignoreOutput: s.ignoreOutput }).catch((er) => toast(er.message, true));
    };
    $("#s-autostart", box)?.addEventListener("change", async (e) => {
      const on = e.target.checked;
      try {
        await api("POST", "/api/settings", { theme: s.theme, workers: s.workers, autostart: on });
        S.autostart = on;
        toast(on ? "Leonzilla se bude spouštět s Windows (minimalizovaně)" : "Spouštění s Windows vypnuto");
      } catch (er) { e.target.checked = !on; toast(er.message, true); }
    });
    $("#s-compiler-pick", box).onclick = async () => {
      try {
        const r = await api("POST", "/api/pickfile", { title: tr("Vyber TMSWebCompiler.exe"), start: s.compilerFound || "", filterName: "TMSWebCompiler.exe", filterSpec: "TMSWebCompiler.exe;*.exe" });
        if (!r.cancelled) { $("#s-compiler", box).value = r.path; saveCompiler(r.path); }
      } catch (e) { toast(e.message, true); }
    };
    $("#s-workers", box).onchange = (e) => { s.workers = +e.target.value; api("POST", "/api/settings", { theme: s.theme, workers: s.workers }).catch((er) => toast(er.message, true)); };
    $("#s-master", box).onclick = async () => {
      const nw = $("#s-new", box).value, nw2 = $("#s-new2", box).value;
      if (nw !== nw2) return toast("Nová hesla se neshodují", true);
      if (!s.master && !nw) return toast("Zadej nové hlavní heslo", true);
      try {
        await api("POST", "/api/master", { old: $("#s-old", box)?.value || "", new: nw });
        s.master = !!nw;
        toast(nw ? "Hlavní heslo nastaveno" : "Hlavní heslo zrušeno");
        openSettings();
      } catch (e) { toast(e.message, true); }
    };
  });
}

/* ------------------------------------------------------------------ modal helper */

/* Dropbox check before a build (setting "Před buildem hlídat Dropbox", default on):
   Dropbox touches files even in ignored folders and the build then fails on "file is being used by another process".
   null = cancelled, {stop:false} = go, {stop:true} = go and stop Dropbox for the build (started again afterwards). */
async function dropboxGate(t) {
  if (t?.dropbox !== "ask") return { stop: false }; // "" = not watched, "stop" = the server stops Dropbox itself
  let st;
  try { st = await api("GET", "/api/dropbox"); } catch { return { stop: false }; }
  if (!st.running || (st.known && st.paused)) return { stop: false };
  return new Promise((resolve) => {
    const ov = $("#overlay2");
    ov.innerHTML = `<div class="modal dbgate" role="dialog" aria-modal="true">
      <div class="modal-h"><h3>Dropbox běží</h3><p>${st.known ? "Synchronizace není pozastavená." : "Nepodařilo se zjistit, jestli je pozastavený."}</p></div>
      <div class="modal-b">
        <p>Dropbox sahá na soubory i ve vyřazených složkách a build pak může spadnout na <i>„soubor využívá jiný proces“</i>.</p>
        <p>Leonzilla ho může <b>na dobu buildu vypnout</b> a pak znovu spustit (za pár sekund je zpět), nebo ho <b>pozastav</b> sám (ikona Dropboxu u hodin → Pozastavit synchronizaci).</p>
        ${st.tray ? `<p class="tray" translate="no">${esc(st.tray)}</p>` : ""}
        <label class="check"><input type="checkbox" id="db-always">U této karty příště vypínat Dropbox automaticky, bez ptaní</label>
        <p><button type="button" class="link" data-r="off" title="Upravit kartu → Dropbox">U této karty Dropbox nehlídat</button></p>
      </div>
      <div class="modal-f">
        <button class="ghost" data-r="cancel">Zrušit</button>
        <button class="ghost" data-r="go">Pozastaveno, pokračovat</button>
        <button class="primary" data-r="stop">Vypnout Dropbox na dobu buildu</button>
      </div></div>`;
    ov.hidden = false;
    const done = (r) => { ov.hidden = true; ov.innerHTML = ""; document.removeEventListener("keydown", onKey, true); resolve(r); };
    const onKey = (e) => { if (e.key === "Escape") { e.stopPropagation(); done(null); } };
    document.addEventListener("keydown", onKey, true);
    ov.onmousedown = (e) => { if (e.target === ov) done(null); };
    ov.querySelectorAll("[data-r]").forEach((b) => (b.onclick = async () => {
      const r = b.dataset.r;
      if (r === "cancel") return done(null);
      if (r === "stop") {
        if (ov.querySelector("#db-always")?.checked) {
          t.dropbox = "stop"; saveTab(t);
          toast("U této karty se Dropbox při buildu vypne automaticky (Upravit kartu → Dropbox)");
        }
        return done({ stop: true });
      }
      if (r === "off") {
        t.dropbox = ""; saveTab(t);
        toast("U této karty se Dropbox už nehlídá (Upravit kartu → Dropbox)");
        return done({ stop: false });
      }
      // "paused, go on" – check once more; if the tooltip still does not say "paused" (or is in a language Leonzilla
      // does not know), warn once and let the second click go on anyway
      if (b.dataset.sure) return done({ stop: false });
      const again = await api("GET", "/api/dropbox").catch(() => null);
      if (again && again.running && !(again.known && again.paused)) {
        ov.querySelector(".modal-h p").innerHTML = "<b>Dropbox pořád nehlásí pozastavení.</b> Pozastav ho, nebo pokračuj přesto.";
        const t2 = ov.querySelector(".tray"); if (t2 && again.tray) t2.textContent = again.tray;
        b.dataset.sure = "1"; b.textContent = tr("Přesto pokračovat");
        return;
      }
      done({ stop: false });
    }));
    ov.querySelector('[data-r="stop"]').focus();
  });
}

function modal(html, bind, wide = false) {
  const ov = $("#overlay");
  ov.innerHTML = `<div class="modal${wide ? " wide" : ""}" role="dialog" aria-modal="true">${html}</div>`;
  ov.hidden = false;
  const box = ov.firstElementChild;
  const close = () => { ov.hidden = true; ov.innerHTML = ""; document.removeEventListener("keydown", onKey); };
  const onKey = (e) => { if (e.key === "Escape") close(); };
  document.addEventListener("keydown", onKey);
  ov.onmousedown = (e) => { if (e.target === ov) close(); };
  box.querySelectorAll("[data-close]").forEach((b) => (b.onclick = close));
  bind?.(box, close);
  if (!box.contains(document.activeElement)) box.querySelector("input, button.primary, button.sync, button")?.focus();
}

/* ------------------------------------------------------------------ boot */

$("#btn-add-tab").onclick = addTab;
// files dropped on the window must not navigate away from the app
document.addEventListener("dragover", (e) => e.preventDefault());
document.addEventListener("drop", (e) => e.preventDefault());
$("#btn-settings").onclick = () => !S.locked && openSettings();
document.addEventListener("keydown", (e) => {
  if (e.ctrlKey && e.key.toLowerCase() === "t") { e.preventDefault(); addTab(); }
  if (e.ctrlKey && e.key === "Tab") {
    // Přehled + open tabs, in the order shown
    e.preventDefault();
    const list = [null, ...orderedTabs().filter((t) => S.open.includes(t.id)).map((t) => t.id)];
    if (list.length < 2) return;
    const i = S.view === "overview" ? 0 : Math.max(0, list.indexOf(S.current));
    const n = list[(i + (e.shiftKey ? -1 : 1) + list.length) % list.length];
    n ? select(n) : showOverview();
  }
  if (e.ctrlKey && e.key.toLowerCase() === "w" && S.view === "card" && S.current && $("#overlay").hidden) { e.preventDefault(); closeTab(S.current); }
});
// keep-alive (used when running in a normal browser) + pick up config changes from other PCs (Dropbox)
setInterval(() => api("POST", "/api/ping").catch(() => {}), 5000);
// Picks up changes made on another PC (Dropbox). Never while a card is being edited or a request
// (folder / file dialog …) is running – the answer would land in a card object that was just replaced.
window.addEventListener("focus", async () => {
  if (S.locked || Object.keys(S.saveTimers).length || S.pendingSave || S.inflight > 0 || S.editing.size) return;
  if (document.activeElement && document.activeElement.matches("input, textarea, select")) return;
  try {
    const st = await api("GET", "/api/state");
    if (S.editing.size || S.pendingSave || Object.keys(S.saveTimers).length) return;
    if (canonJSON(st.tabs) !== canonJSON(S.tabs.map(stripLocal))) {
      S.tabs = st.tabs; S.jobs = st.jobs || {};
      if (!S.tabs.some((t) => t.id === S.current)) S.current = S.tabs[0]?.id ?? null;
      S.open = S.open.filter((id) => S.tabs.some((t) => t.id === id));
      render();
    }
  } catch {}
});
function stripLocal(t) { const c = JSON.parse(JSON.stringify(t)); delete c.conn.passNew; return c; }
// JSON with sorted keys – the server and the page may order object keys differently
function canonJSON(v) {
  const norm = (x) => Array.isArray(x) ? x.map(norm) : x && typeof x === "object" ? Object.keys(x).sort().reduce((o, k) => { if (x[k] !== undefined) o[k] = norm(x[k]); return o; }, {}) : x;
  return JSON.stringify(norm(v));
}

load();
