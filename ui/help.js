"use strict";
/* Leonzilla – built-in guide (button "?" in the top bar), Czech. The English guide is in help_en.js.
   Uses modal(), icon(), esc(), api(), S from app.js. */

const HELP_SECTIONS = [
  ["start", "Rychlý start"],
  ["buttons", "Tlačítka"],
  ["patterns", "Vyloučení a vzory"],
  ["changes", "Co se nahraje"],
  ["build", "Build"],
  ["backup", "Zálohy"],
  ["git", "GitHub"],
  ["fw", "FW build a monitor"],
  ["security", "Hesla a bezpečnost"],
  ["portable", "Portable a více PC"],
  ["keys", "Klávesy a triky"],
];

function helpHTMLcs() {
  const k = (x) => `<kbd>${x}</kbd>`;
  const c = (x) => `<code>${esc(x)}</code>`;
  const pat = (p, what, ex) => `<tr><td>${c(p)}</td><td>${what}</td><td class="hx">${ex}</td></tr>`;
  return `
  <div class="hp" translate="no">
    <nav class="hp-nav" id="hp-nav">
      <div class="hp-brand"><svg><use href="#i-mark"/></svg><span>Návod</span></div>
      ${helpSections().map(([id, name], i) => `<a href="#hp-${id}" data-sec="${id}"><span class="n">${String(i + 1).padStart(2, "0")}</span>${name}</a>`).join("")}
      <a href="#hp-thanks" data-sec="thanks" class="hp-nav-end"><span class="n">♥</span>Poděkování</a>
    </nav>

    <div class="hp-body" id="hp-body">
      <button class="icon-btn hp-close" data-close title="Zavřít (Esc)">${icon("x")}</button>

      <header class="hp-hero">
        <div class="hp-mark"><svg><use href="#i-mark"/></svg></div>
        <div>
          <h1>Leonzilla <span class="hp-ver">verze ${esc(S.version || "")}</span></h1>
          <p>Jedním klikem nahraje tvoje projekty z PC na web – přes FTP, FTPS nebo SFTP. Umí i build, zálohy do ZIP a odeslání na GitHub. Na serveru nikdy nic nemaže.</p>
          <div class="hp-pills"><span>${icon("lock")}hesla chráněná hlavním heslem</span><span>${icon("folder")}portable – nic se neinstaluje</span><span>${icon("up")}jen nahrává, nemaže</span></div>
        </div>
      </header>

      <section id="hp-start">
        <h2><span class="hp-ic sp">${icon("plus")}</span>Rychlý start</h2>
        <ol class="hp-steps">
          <li><b>Nová karta</b> – tlačítko ${k("+")} v liště nebo ${k("Ctrl")}+${k("T")}. Každý projekt (web, firmware…) má svou kartu.</li>
          <li><b>Vyplň, co karta má umět</b> – stačí cokoli z toho: <i>synchronizace na server</i> (server + dvojice složek PC → server), <i>build</i>, <i>záloha</i>, <i>GitHub</i>, <i>FW build</i>. Vše se ukládá hned. Tlačítko <i>Synchronizovat</i> se objeví, až je vyplněný server i složka; karta jen na zálohy nechá sekci složek prázdnou.</li>
          <li><b>Hotovo – zamknout.</b> Zamčená karta jen ukazuje nastavení a nejde omylem změnit. Úpravy přes <i>Upravit kartu</i>.</li>
          <li><b>Přehled</b> – všechny karty jako dlaždice, rozdělené do skupin (Nastavení → Skupiny). Dlaždice jde přetáhnout do jiné skupiny. Skupinu sbalíš klikem na její název – které skupiny jsou sbalené (i otevřené záložky), si pamatuje každý počítač zvlášť.</li>
          <li><b>Záložky nahoře</b> = jen otevřené karty. Kartu otevřeš kliknutím na dlaždici, zavřeš ✕ na záložce (nebo ${"<kbd>Ctrl</kbd>+<kbd>W</kbd>"}, prostřední tlačítko myši) – v Přehledu zůstane. Když se záložky nevejdou, objeví se šipky ‹ › (posouvá i kolečko myši).</li>
        </ol>
        <p class="hp-tip">${icon("copy")}<span>Podobný projekt? <b>Duplikovat</b> zkopíruje celou kartu včetně hesla – pak jen přepíšeš, co se liší. Nepotřebnou kartu smažeš v <i>Upravit kartu → Smazat</i> nebo pravým tlačítkem na záložce či dlaždici (soubory v PC ani na serveru se nemění).</span></p>
      </section>

      <section id="hp-buttons">
        <h2><span class="hp-ic am">${icon("up")}</span>Tlačítka</h2>
        <div class="hp-btns">
          <div><span class="hp-b am">${icon("up")}Synchronizovat</span><p>Porovná složky se serverem a nahraje nové a změněné soubory. Před startem potvrdíš (a odškrtáš kontrolní body, jsou-li nastavené).</p></div>
          <div><span class="hp-b am">${icon("gear")}Build + Synchronizovat</span><p>Nejdřív zkompiluje vybrané buildy, a když všechny projdou, nahraje. <i>Jen build</i> / <i>Jen nahrát</i> udělá jen jednu část.</p></div>
          <div><span class="hp-b gh">Zkontrolovat</span><p>Náhled bez nahrávání – ukáže, co by se nahrálo.</p></div>
          <div><span class="hp-b bl">${icon("archive")}Zálohovat</span><p>Zabalí nastavené složky do ZIP s datem a časem v názvu.</p></div>
          <div><span class="hp-b gt">${icon("branch")}GitHub</span><p>Projde repozitáře karty, ukáže změny a udělá commit a push.</p></div>
          <div><span class="hp-b gh">${icon("copy")}Duplikovat</span><p>Nová karta se stejným nastavením hned za originálem.</p></div>
        </div>
      </section>

      <section id="hp-patterns">
        <h2><span class="hp-ic rd">${icon("x")}</span>Vyloučení a vzory</h2>
        <p>Co se nemá nahrávat ani zálohovat. Vyloučení jde zadat u každé dvojice složek / zálohy (tlačítka <i>složku</i>, <i>soubor</i>, <i>vzor</i>) a pro celou kartu v poli <i>Vyloučit ve všech složkách</i> (resp. <i>ve všech zálohách</i>) – jedno pravidlo na řádek.</p>
        <table class="hp-tab">
          <thead><tr><th>Vzor</th><th>Co vyloučí</th><th>Příklad</th></tr></thead>
          <tbody>
            ${pat(".git/", "složku s tímto názvem <b>kdekoli</b>", "<code>.git/</code> <code>managed_components/</code>")}
            ${pat("build*/", "složky, jejichž název <b>začíná</b> na build – kdekoli", "build, build_dev, build_prod")}
            ${pat("*.psd", "soubory podle masky – kdekoli", "<code>*.tmp</code> <code>~$*</code>")}
            ${pat("img/raw/", "jednu <b>konkrétní</b> složku (lomítko uprostřed = cesta od zdrojové složky)", "jen img/raw, ne jiné raw")}
            ${pat("firmware/esp32/build*/", "maska jen v dané cestě", "build složky jen ve firmware/esp32")}
            ${pat("docs/navod.pdf", "jeden konkrétní soubor", "")}
            ${pat("foto?.jpg", "<code>?</code> = právě jeden znak", "foto1.jpg, fotoA.jpg")}
            ${pat("# poznámka", "řádek s # se ignoruje", "")}
          </tbody>
        </table>
        <ul class="hp-list">
          <li><b>Lomítko na konci</b> = jen složky (soubor stejného jména zůstane).</li>
          <li><code>*</code> nepřeskočí lomítko: <code>a/*/</code> chytí <code>a/x/</code>, ne <code>a/x/y/</code>. Vzor bez lomítka uprostřed platí v každé úrovni.</li>
          <li>Velikost písmen se nerozlišuje, <code>\\</code> i <code>/</code> znamenají totéž.</li>
          <li>Vyloučená složka se vůbec neprochází – zrychlí to porovnání.</li>
        </ul>
      </section>

      <section id="hp-changes">
        <h2><span class="hp-ic sp">${icon("log")}</span>Co se nahraje</h2>
        <ul class="hp-list">
          <li>Soubor na serveru <b>chybí</b> nebo má <b>jinou velikost</b> → nahraje se.</li>
          <li>Soubor se v PC <b>změnil od posledního nahrání</b> (Leonzilla si pamatuje velikost a čas) → nahraje se i při stejné velikosti.</li>
          <li>Na serveru se <b>nic nemaže</b> – smazané soubory v PC tam zůstanou.</li>
          <li>Nahrává se přes dočasný soubor, který se po dokončení přejmenuje (web nikdy neuvidí napůl nahraný soubor), a ověřuje se velikost.</li>
          <li>Pro jistotu jde v potvrzení zaškrtnout <i>Nahrát znovu všechny soubory</i>.</li>
        </ul>
      </section>

      <section id="hp-build">
        <h2><span class="hp-ic am">${icon("gear")}</span>Build (RAD Studio / TMS WEB Core)</h2>
        <ul class="hp-list">
          <li>Buildy se zakládají jednou v <i>Nastavení aplikace → Buildy</i> (projekt <code>.dproj</code>, konfigurace, přepínače) a v kartě se jen vyberou a seřadí.</li>
          <li>Přepínače se čtou z horní části <code>Predefines.inc</code> v bloku <code>{$IFNDEF LEONZILLA} … {$ENDIF}</code>; když přibude nový, načteš ho tlačítkem <i>Načíst přepínače</i>.</li>
          <li>Volitelně převod konců řádků na CRLF a post-build z projektu. Výstup (TMSWeb) se vyřadí z Dropboxu, aby nezamykal soubory.</li>
          <li><b>Hlídání Dropboxu</b> (Nastavení aplikace → Buildy, výchozí zapnuto): před každým buildem (RAD Studio, vlastní skript, FW ESP32) Leonzilla zjistí, jestli Dropbox běží a jestli je pozastavený (z popisku jeho ikony u hodin). Když synchronizuje, zeptá se: Leonzilla ho na dobu buildu vypne a pak znovu spustí (za pár sekund je zpět), nebo ho pozastav sám. Volba <i>Během buildu Dropbox vždy vypnout</i> to dělá bez ptaní. Dropbox totiž sahá na soubory i ve vyřazených složkách a build pak padá na „soubor využívá jiný proces“.</li>
          <li><b>Info k verzi</b> (v nastavení buildu, 1 řádek, max 80 znaků, přednastavené = název buildu): post-build a příkaz po buildu ho dostanou v proměnné <code>LEONZILLA_VERSION_INFO</code> – např. pro zápis do <code>version.json</code>. Když skript projektu čte jiný název proměnné, doplň ho v <i>Nastavení aplikace → Buildy</i>. Prázdné = bez info. V potvrzení buildu jde text přepsat – <b>karta si ho zapamatuje</b> a příště ho nabídne znovu; ↺ vrátí výchozí text z nastavení buildu.</li>
          <li>Okno průběhu ukazuje kroky a výpis kompilátoru; <i>Skrýt</i> nechá build běžet na pozadí.</li>
          <li><b>Vlastní skript</b> (sekce v nastavení karty; na kartě se pak místo „Build“ píše „Skript“): místo projektu RAD Studia jen <b>cesta ke skriptu</b> – Python (<code>.py</code>), dávka (<code>.bat</code>, <code>.cmd</code>) nebo PowerShell (<code>.ps1</code>) – a případné parametry. Skript se spustí ve své složce (Python z PATH, jinak spouštěč <code>py</code>); když skončí bez chyby (kód 0), karta nahraje složky na server. Na kartě vypadá a chová se jako build – <i>Build + Synchronizovat</i>, <i>Jen build</i>, <i>Jen nahrát</i>. Když má karta i buildy RAD Studia, Build light jde až po nich. Vlastní <b>popisek tlačítka</b> (např. <i>Build a nahrát na web</i>) nahradí výchozí text – hodí se pro skript, který nahrává sám (třeba vydání programu na web); taková karta nemusí mít server ani složky.</li>
        </ul>
      </section>

      <section id="hp-backup">
        <h2><span class="hp-ic bl">${icon("archive")}</span>Zálohy</h2>
        <ul class="hp-list">
          <li>Každá záloha = složka → cílová složka. Vznikne <code>prefix_RRRR-MM-DD_hh-mm-ss.zip</code> (prázdný prefix = název složky).</li>
          <li>Název ZIPu při potvrzení – u každé zálohy zvlášť, přepínačem <i>Poznámka | Vlastní název</i>:
            <br>prázdné pole = výchozí <code>web_2026-10-18_10-41-14.zip</code>
            · <i>Poznámka</i> = vloží se mezi název a datum <code>web_nove cfg_2026-10-18_10-41-14.zip</code>
            · <i>Vlastní název</i> = přesně tvůj název bez data <code>fw-160.zip</code> (když už existuje, vznikne <code>fw-160 (2).zip</code> – nic se nepřepíše).
            Výsledný název hned vidíš; ${"<kbd>Enter</kbd>"} skočí na další pole, v posledním spustí zálohu.</li>
          <li>Má-li karta víc záloh, je v potvrzení u každé <b>zaškrtávátko</b> (standardně zaškrtnuté). Odškrtnutá záloha se tentokrát přeskočí – nastavení karty se nemění.</li>
          <li>Pole pro název jdou pro celou kartu vypnout: <i>Upravit kartu → Zálohování → Při zálohování nabídnout poznámku nebo vlastní název ZIP</i>. Potvrzení pak jen ukáže výchozí názvy ZIPů.</li>
          <li>Vyloučení stejně jako u synchronizace – typicky <code>.git/</code>, <code>build*/</code>, <code>managed_components/</code>.</li>
          <li>ZIP se píše jako <code>.zip.part</code> a přejmenuje se až po dokončení. Zamčený soubor se přeskočí s varováním. Staré zálohy Leonzilla nemaže.</li>
        </ul>
      </section>

      <section id="hp-git">
        <h2><span class="hp-ic gt">${icon("branch")}</span>GitHub</h2>
        <ul class="hp-list">
          <li>V kartě vybereš složky s git repozitáři. Kam patří, ví každý repozitář sám (<code>origin</code>).</li>
          <li>Tlačítko GitHub je projde postupně, <b>bez změn přeskočí</b>. Jinak ukáže změněné soubory (klik = změny v souboru), popis commitu a tlačítka <i>Commit + Push</i>, <i>Jen commit</i>, <i>Přeskočit</i>.</li>
          <li>Commitují se <b>jen zaškrtnuté soubory</b>. Push jde do aktuální větve (obvykle <code>main</code>).</li>
          <li><i>Před pushem stáhnout změny</i> – když commituješ i z jiného PC.</li>
          <li>Potřebuje <b>Git for Windows</b>. Přihlášení řeší Git sám – Leonzilla žádný token neukládá.</li>
        </ul>
      </section>

      <section id="hp-fw">
        <h2><span class="hp-ic fw">${icon("chip")}</span>FW build a monitor (ESP-IDF / ESP32)</h2>
        <ul class="hp-list">
          <li><b>Monitor</b> (zaškrtávátko v sekci <i>FW build a monitor</i>): na kartě i dlaždici přibude tlačítko <i>Monitor</i> – vybereš COM port, <i>Otevřít monitor</i> a jede to, i bez buildu a nahrávání. Na samotný monitor není potřeba složka projektu.</li>
          <li>V kartě sekce <i>FW build</i>: složka projektu (kde je <code>CMakeLists.txt</code>), aktivace ESP-IDF (PowerShell profil z EIM – prázdné = najde se v <code>C:\Espressif\tools</code>) a <b>varianty</b>. Tlačítko <i>Načíst z tools\build_*.cmd</i> je vytvoří samo (název, složka buildu z <code>-B</code>, PROD skripty jako „bez nahrávání“).</li>
          <li>Tlačítko <b>FW build</b>: vybereš variantu, akci (<i>Build</i>, <i>Build + Nahrát</i>, <i>Build + Nahrát + Monitor</i>, <i>Jen monitor</i>) a COM port. Volba se pamatuje u karty.</li>
          <li><b>Clean config</b> se zaškrtává až při spuštění (pokaždé znovu) – smaže <code>&lt;složka buildu&gt;\sdkconfig</code>, který se pak vytvoří ze <code>sdkconfig.defaults</code>. Potřeba po změně <code>sdkconfig.defaults</code>, profilu nebo Kconfig.</li>
          <li><b>Reconfigure</b> (standardně zaškrtnuté, při každém otevření okna znovu): před buildem spustí <code>tools\build_dev.cmd reconfigure</code> (znovu načte CMake / sdkconfig).</li>
          <li><b>Monitor</b> je přímo v okně: barevný výpis zařízení (chyby červeně, varování žlutě, odpovědi <code>@…</code> modře) a dole řádek na příkazy – ${"<kbd>Enter</kbd>"} odešle, ${"<kbd>↑</kbd> <kbd>↓</kbd>"} historie, <i>Reset zařízení</i> = restart přes RTS. Text ve výpisu jde označit myší a zkopírovat ${"<kbd>Ctrl</kbd>+<kbd>C</kbd>"}; vpravo nahoře <i>Kopírovat vše</i> a <i>Vymazat</i>. Během označování se výpis neposouvá. <i>Skrýt</i> nechá monitor běžet (dlaždice ukazuje „FW monitor běží“).</li>
          <li><i>Terminál ESP-IDF</i> otevře normální PowerShell s aktivním ESP-IDF ve složce projektu – na <code>menuconfig</code> a další příkazy.</li>
          <li>Port nesmí mít otevřený jiný program (monitor ve VS Code) – Leonzilla to řekne.</li>
        </ul>
      </section>

      <section id="hp-security">
        <h2><span class="hp-ic sp">${icon("lock")}</span>Hesla a bezpečnost</h2>
        <div class="hp-cards">
          <div class="hp-card">
            <h3>Bez hlavního hesla</h3>
            <p>Hesla k serverům jsou v <code>config.json</code> <b>zamaskovaná</b> (AES s klíčem uvnitř aplikace). V souboru nejsou čitelná, ale kdo má soubor i Leonzillu, heslo získá.</p>
          </div>
          <div class="hp-card on">
            <h3>${icon("lock")}S hlavním heslem</h3>
            <p>Hesla se <b>zašifrují</b> – klíč se odvodí z hlavního hesla (scrypt) a šifruje AES-256-GCM. Bez hlavního hesla je z <code>config.json</code> nikdo nepřečte. Zapneš v <i>Nastavení aplikace</i>; Leonzilla se na něj ptá při spuštění.</p>
          </div>
        </div>
        <ul class="hp-list">
          <li><b>Zapomenuté hlavní heslo nejde obnovit</b> – hesla k serverům pak zadáš znovu.</li>
          <li>SFTP: klíč serveru se při prvním připojení zapamatuje; když se změní, Leonzilla připojení odmítne a zeptá se.</li>
          <li>Okno komunikuje jen s Leonzillou na tomto PC (<code>127.0.0.1</code>, náhodný port a token). Nikam jinam se nic neposílá.</li>
        </ul>
      </section>

      <section id="hp-portable">
        <h2><span class="hp-ic sp">${icon("folder")}</span>Portable a více PC</h2>
        <ul class="hp-list">
          <li>Nic se neinstaluje: <code>Leonzilla.exe</code> a vedle něj složka <code>Leonzilla_data</code> (karty, nastavení, stav nahrání, logy). Celé to může ležet v Dropboxu a spouštět se na více PC.</li>
          <li>Na každém PC zvlášť (mimo Dropbox, v <code>%LOCALAPPDATA%\\Leonzilla</code>) je jen velikost a poloha okna a cache okna.</li>
          <li>Změny z jiného PC se načtou při návratu do okna. Stejnou kartu nesynchronizuj na dvou PC současně.</li>
          <li>Na jednom PC běží jedna Leonzilla – druhé spuštění přepne do otevřeného okna.</li>
          <li><b>Spouštění s Windows</b> (Nastavení aplikace → Spouštění, výchozí vypnuto): Leonzilla se po přihlášení spustí minimalizovaná na hlavním panelu. Nastavuje se na každém PC zvlášť.</li>
          <li>Potřebuje Windows 10/11 s WebView2 (ve Windows 11 vždy je).</li>
          <li><b>Jazyk</b> – čeština nebo angličtina: <i>Nastavení aplikace → Jazyk</i> (výchozí podle jazyka Windows).</li>
        </ul>
      </section>

      <section id="hp-keys">
        <h2><span class="hp-ic sp">${icon("sliders")}</span>Klávesy a triky</h2>
        <div class="hp-keys">
          <div>${k("Ctrl")}+${k("T")}<span>nová karta</span></div>
          <div>${k("Ctrl")}+${k("Tab")}<span>další karta (${k("Shift")} = předchozí)</span></div>
          <div>${k("Ctrl")}+${k("Enter")}<span>v popisu commitu = Commit + Push</span></div>
          <div>${k("Esc")}<span>zavře okno / dialog</span></div>
          <div>${k("Ctrl")}+${k("W")}<span>zavře záložku karty (karta zůstane v Přehledu)</span></div>
          <div>${k("pravé tlačítko")}<span>na záložce či dlaždici: upravit, duplikovat, barva, smazat (barva jde změnit i nahoře v úpravách karty)</span></div>
          <div>${k("dvojklik")}<span>na záložku upravované karty = přejmenovat</span></div>
        </div>
        <ul class="hp-list">
          <li>Záložky i dlaždice jde přetahovat (pořadí, skupiny).</li>
          <li><b>Klik na logo</b> vlevo nahoře = <i>O aplikaci</i> (verze, odkaz na GitHub a nejnovější verzi, nahlášení chyby).</li>
          <li>Tlačítko <i>Log</i> otevře celý záznam karty; při pádu je výpis v <code>Leonzilla_data\\logs\\crash.log</code>.</li>
        </ul>
      </section>

      <footer class="hp-thanks" id="hp-thanks">
        <div class="hp-mark big"><svg><use href="#i-mark"/></svg></div>
        <p class="hp-ty">Děkuji, že používáte Leonzillu.<br>Užívejte ji!</p>
        <p class="hp-sign">© ${new Date().getFullYear()} Leoš Vilím · <a href="https://leotech.cz" data-url>leotech.cz</a> · <a href="https://github.com/Leontech/leonzilla/issues" data-url>nahlásit chybu / nápad</a></p>
        <p class="hp-sign"><a href="https://ko-fi.com/leotechcz" data-url>☕ Podpořit vývoj – kafe pro autora</a></p>
      </footer>
    </div>
  </div>`;
}

function helpSections() { return I18N.lang === "en" && typeof HELP_SECTIONS_EN !== "undefined" ? HELP_SECTIONS_EN : HELP_SECTIONS; }
function helpHTML() { return I18N.lang === "en" && typeof helpHTMLen === "function" ? helpHTMLen() : helpHTMLcs(); }

function openHelp(section) {
  modal(helpHTML(), (box) => {
    box.classList.add("help");
    const body = $("#hp-body", box), nav = $("#hp-nav", box);
    const links = [...nav.querySelectorAll("a")];
    const mark = (id) => links.forEach((a) => a.classList.toggle("on", a.dataset.sec === id));
    nav.onclick = (e) => {
      const a = e.target.closest("a");
      if (!a) return;
      e.preventDefault();
      const el = box.querySelector(a.getAttribute("href"));
      if (el) body.scrollTo({ top: el.offsetTop - 18, behavior: "smooth" });
      mark(a.dataset.sec);
    };
    // highlight the section being read
    const secs = [...body.querySelectorAll("section, footer")];
    body.onscroll = () => {
      const y = body.scrollTop + 90;
      let cur = secs[0];
      for (const s of secs) if (s.offsetTop <= y) cur = s;
      if (body.scrollTop + body.clientHeight >= body.scrollHeight - 4) cur = secs[secs.length - 1];
      mark(cur.id.replace("hp-", ""));
    };
    mark("start");
    box.querySelectorAll("a[data-url]").forEach((a) => (a.onclick = (e) => {
      e.preventDefault();
      api("POST", "/api/openurl", { url: a.href }).catch((err) => toast(err.message, true));
    }));
    if (section) { const el = box.querySelector("#hp-" + section); if (el) body.scrollTop = el.offsetTop - 18; }
    body.focus();
  }, true);
}

$("#btn-help").onclick = () => !S.locked && openHelp();
document.addEventListener("keydown", (e) => { if (e.key === "F1") { e.preventDefault(); if (!S.locked && $("#overlay").hidden) openHelp(); } });
