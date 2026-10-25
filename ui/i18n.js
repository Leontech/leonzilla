"use strict";
/* Leonzilla – UI language.
   The UI is written in Czech. For English, the texts the UI renders (text nodes and the title /
   placeholder / aria-label attributes) are translated in place right after they appear in the page,
   using the catalog in lang_en.js (Czech text → English; "{}" = a value filled in by the code).
   Missing translations stay Czech; I18N.missing() lists them (developer console).
   Elements with translate="no" (names, paths, logs …) are never touched. */

const I18N = (() => {
  let lang = "cs";
  const exact = new Map();   // normalized Czech text → English
  const patterns = [];       // [RegExp, English template with {1} {2} …]
  const cache = new Map();
  const missing = new Set();
  const CZ = /[áčďéěíňóřšťúůýžÁČĎÉĚÍŇÓŘŠŤÚŮÝŽ„]|\b(Synchronizovat|Synchronizace|Zkontrolovat|Hotovo|nebo|karta|karty|Jen)\b/;
  const ATTRS = ["title", "placeholder", "aria-label"];

  const norm = (s) => s.replace(/\s+/g, " ").trim();
  const reEsc = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

  function add(dict) {
    for (const [cz, en] of Object.entries(dict)) {
      const k = norm(cz);
      if (!k.includes("{}")) { exact.set(k, en); continue; }
      let n = 0;
      const re = new RegExp("^" + k.split("{}").map(reEsc).join("(.*?)") + "$");
      // English template: "{}" in order, or explicit {1} {2} … to reorder
      const tpl = /\{\d\}/.test(en) ? en : en.replace(/\{\}/g, () => `{${++n}}`);
      patterns.push([re, tpl, k.replace(/\{\}/g, "").length]);
    }
    patterns.sort((a, b) => b[2] - a[2]); // the most specific (longest literal part) first
    cache.clear();
  }

  // translation of one (normalized) text; "" parts and texts without letters stay as they are
  function core(s, depth = 0) {
    if (!s || !/\p{L}/u.test(s) || depth > 4) return s;
    if (exact.has(s)) return exact.get(s);
    if (cache.has(s)) return cache.get(s);
    let out = null;
    for (const [re, tpl] of patterns) {
      const m = re.exec(s);
      if (m) { out = tpl.replace(/\{(\d)\}/g, (_, i) => part(m[+i] ?? "", depth + 1)); break; }
    }
    if (out === null) {
      // generic shapes: "12 souborů", "Text: rest", "a · b"
      let m;
      if ((m = /^([\d\s.,%/+-]+)\s(\D.*)$/u.exec(s))) out = m[1] + " " + part(m[2], depth + 1);
      else if (s.includes(" · ")) out = s.split(" · ").map((x) => part(x, depth + 1)).join(" · ");
      else if ((m = /^(.+?)(:\s+)(.+)$/.exec(s)) && exact.has(m[1])) out = exact.get(m[1]) + m[2] + part(m[3], depth + 1);
    }
    if (out === null) { out = s; if (CZ.test(s)) missing.add(s); }
    cache.set(s, out);
    return out;
  }
  // keeps the leading / trailing whitespace and punctuation of a captured part
  function part(s, depth) {
    const m = /^(\s*)([\s\S]*?)(\s*)$/.exec(s);
    return m[1] + core(norm(m[2]), depth) + m[3];
  }
  function tr(s) {
    if (lang !== "en" || s == null) return s;
    s = String(s);
    if (!/\p{L}/u.test(s)) return s;
    return part(s, 0);
  }

  function skip(el) { return el.closest && el.closest('[translate="no"], textarea, script, style'); }

  function doNode(n) {
    if (n.nodeType === 3) {
      const p = n.parentElement;
      if (!p || skip(p)) return;
      const v = n.nodeValue;
      if (!v || !/\p{L}/u.test(v)) return;
      const t = tr(v);
      if (t !== v) n.nodeValue = t;
    } else if (n.nodeType === 1) {
      if (skip(n)) return;
      for (const a of ATTRS) {
        const v = n.getAttribute(a);
        if (v && /\p{L}/u.test(v)) { const t = tr(v); if (t !== v) n.setAttribute(a, t); }
      }
      const w = document.createTreeWalker(n, NodeFilter.SHOW_ELEMENT | NodeFilter.SHOW_TEXT, {
        acceptNode: (x) => (x.nodeType === 1 && x.matches('[translate="no"], textarea, script, style') ? NodeFilter.FILTER_REJECT : NodeFilter.FILTER_ACCEPT),
      });
      let x = w.nextNode();
      while (x) {
        if (x.nodeType === 3) doNode(x);
        else for (const a of ATTRS) {
          const v = x.getAttribute(a);
          if (v && /\p{L}/u.test(v)) { const t = tr(v); if (t !== v) x.setAttribute(a, t); }
        }
        x = w.nextNode();
      }
    }
  }

  let obs = null;
  function start() {
    if (obs) return;
    doNode(document.body);
    document.title = tr(document.title);
    obs = new MutationObserver((list) => {
      for (const m of list) {
        if (m.type === "childList") m.addedNodes.forEach(doNode);
        else if (m.type === "characterData") doNode(m.target);
        else if (m.type === "attributes" && !skip(m.target)) {
          const v = m.target.getAttribute(m.attributeName);
          if (v && /\p{L}/u.test(v)) { const t = tr(v); if (t !== v) m.target.setAttribute(m.attributeName, t); }
        }
      }
    });
    obs.observe(document.body, { childList: true, subtree: true, characterData: true, attributes: true, attributeFilter: ATTRS });
  }

  // native dialogs show translated texts too
  const _prompt = window.prompt.bind(window), _confirm = window.confirm.bind(window), _alert = window.alert.bind(window);
  window.prompt = (m, d) => _prompt(tr(m), d);
  window.confirm = (m) => _confirm(tr(m));
  window.alert = (m) => _alert(tr(m));

  return {
    add, tr,
    get lang() { return lang; },
    locale: () => (lang === "en" ? "en-GB" : "cs-CZ"),
    setLang(l) {
      lang = l === "en" ? "en" : "cs";
      document.documentElement.lang = lang;
      if (lang === "en") start();
    },
    missing: () => [...missing].sort(),
  };
})();
const tr = I18N.tr;
