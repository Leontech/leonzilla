"use strict";
/* Leonzilla – "O aplikaci" (click on the logo in the top bar, link in the app settings). */

const LZ_REPO = "https://github.com/Leontech/leonzilla";
const LZ_AUTHOR_WEB = "https://leotech.cz";
// "Support development" link (Ko-fi / Buy Me a Coffee / GitHub Sponsors …); empty = the button is hidden
const LZ_SUPPORT_URL = "https://ko-fi.com/leotechcz";

let leotechLogo = null;
async function leotechSvg() {
  if (leotechLogo === null) {
    try { leotechLogo = await (await fetch("leotech.svg")).text(); } catch { leotechLogo = ""; }
  }
  return leotechLogo;
}

async function openAbout() {
  const logo = await leotechSvg();
  const year = Math.max(2026, new Date().getFullYear());
  modal(`
    <div class="about">
      <button class="icon-btn about-x" data-close title="Zavřít (Esc)">${icon("x")}</button>
      <div class="about-hero">
        <div class="about-mark"><svg><use href="#i-mark"/></svg></div>
        <div>
          <h3>Leonzilla</h3>
          <p class="about-ver" translate="no">${esc(I18N.lang === "en" ? "version" : "verze")} ${esc(S.version || "")}</p>
          <p class="about-lead">Jedním klikem z PC na web – FTP, FTPS a SFTP, build, zálohy do ZIP a GitHub. Na serveru nic nemaže.</p>
        </div>
      </div>
      <div class="about-links">
        <a href="${LZ_REPO}" data-url>${icon("branch")}<span>Zdrojový kód na GitHubu</span></a>
        <a href="${LZ_REPO}/releases/latest" data-url>${icon("up")}<span>Nejnovější verze</span></a>
        <a href="${LZ_REPO}/issues" data-url>${icon("help")}<span>Nahlásit chybu / nápad</span></a>
      </div>
      ${LZ_SUPPORT_URL ? `<a class="about-support" href="${esc(LZ_SUPPORT_URL)}" data-url><span class="cup">☕</span><span><b>Podpořit vývoj</b><small>Leonzilla je zdarma. Když vám šetří čas, můžete autorovi koupit kafe.</small></span></a>` : ""}
      <a class="about-author" href="${LZ_AUTHOR_WEB}" data-url title="leotech.cz">
        <span class="about-by">Vytvořil</span>
        <span class="about-logo">${logo || "LEOTECH"}</span>
        <span class="about-web">vývoj hardwaru, firmwaru a softwaru · <u translate="no">leotech.cz</u></span>
      </a>
      <p class="about-copy" translate="no">© ${year} Leoš Vilím · ${I18N.lang === "en" ? "MIT License" : "licence MIT"}</p>
    </div>`, (box) => {
    box.classList.add("about-modal");
    box.querySelectorAll("a[data-url]").forEach((a) => (a.onclick = (e) => {
      e.preventDefault();
      api("POST", "/api/openurl", { url: a.href }).catch((err) => toast(err.message, true));
    }));
  });
}

$("#btn-about").onclick = () => openAbout();
