"use strict";
/* Leonzilla – built-in guide in English (the Czech one is in help.js). */

const HELP_SECTIONS_EN = [
  ["start", "Quick start"],
  ["buttons", "Buttons"],
  ["patterns", "Exclusions and patterns"],
  ["changes", "What gets uploaded"],
  ["build", "Build"],
  ["backup", "Backups"],
  ["git", "GitHub"],
  ["fw", "FW build and monitor"],
  ["security", "Passwords and security"],
  ["portable", "Portable and more PCs"],
  ["keys", "Keys and tricks"],
];

function helpHTMLen() {
  const k = (x) => `<kbd>${x}</kbd>`;
  const c = (x) => `<code>${esc(x)}</code>`;
  const pat = (p, what, ex) => `<tr><td>${c(p)}</td><td>${what}</td><td class="hx">${ex}</td></tr>`;
  return `
  <div class="hp" translate="no">
    <nav class="hp-nav" id="hp-nav">
      <div class="hp-brand"><svg><use href="#i-mark"/></svg><span>Guide</span></div>
      ${HELP_SECTIONS_EN.map(([id, name], i) => `<a href="#hp-${id}" data-sec="${id}"><span class="n">${String(i + 1).padStart(2, "0")}</span>${name}</a>`).join("")}
      <a href="#hp-thanks" data-sec="thanks" class="hp-nav-end"><span class="n">♥</span>Thanks</a>
    </nav>

    <div class="hp-body" id="hp-body">
      <button class="icon-btn hp-close" data-close title="Close (Esc)">${icon("x")}</button>

      <header class="hp-hero">
        <div class="hp-mark"><svg><use href="#i-mark"/></svg></div>
        <div>
          <h1>Leonzilla <span class="hp-ver">version ${esc(S.version || "")}</span></h1>
          <p>Uploads your projects from the PC to the web with one click – over FTP, FTPS or SFTP. It also builds, backs up to ZIP and pushes to GitHub. It never deletes anything on the server.</p>
          <div class="hp-pills"><span>${icon("lock")}passwords protected by a master password</span><span>${icon("folder")}portable – nothing to install</span><span>${icon("up")}only uploads, never deletes</span></div>
        </div>
      </header>

      <section id="hp-start">
        <h2><span class="hp-ic sp">${icon("plus")}</span>Quick start</h2>
        <ol class="hp-steps">
          <li><b>New card</b> – the ${k("+")} button in the bar or ${k("Ctrl")}+${k("T")}. Every project (website, firmware…) has its own card.</li>
          <li><b>Fill in what the card should do</b> – any of these is enough: <i>synchronization to a server</i> (server + folder pairs PC → server), <i>build</i>, <i>backup</i>, <i>GitHub</i>, <i>FW build</i>. Everything is saved immediately. The <i>Synchronize</i> button appears once both a server and a folder are filled in; a backup-only card leaves the folder section empty.</li>
          <li><b>Settings tabs.</b> In <i>Edit card</i> the settings are split into tabs <i>Synchronization</i> (folders, server, check, exclusions), <i>Own script</i>, <i>Backups</i>, <i>GitHub</i>, <i>FW ESP32</i> and finally <i>Build (RAD Studio)</i>. <i>App settings</i> are split the same way: <i>General</i> (theme, language, start with Windows, parallel uploads, master password, export / import, more PCs), <i>Groups</i> and <i>Builds (RAD Studio)</i>.</li>
          <li><b>Save.</b> Card changes are written only with <i>Save</i> – then the card is locked and only shows its settings (it cannot be changed by accident). <i>Close without saving</i> puts the card back as it was. Switching to another card keeps unfinished changes (● on the tab); closing the tab asks. Change it again with <i>Edit card</i>.</li>
          <li><b>Overview</b> – all cards as tiles, sorted into groups (Settings → Groups). Drag a tile to move it to another group. Click a group name to collapse it – each computer remembers its own collapsed groups (and open tabs).</li>
          <li><b>Tabs at the top</b> = open cards only. Open a card by clicking its tile, close it with ✕ on the tab (or ${"<kbd>Ctrl</kbd>+<kbd>W</kbd>"}, middle mouse button) – it stays in the Overview. When the tabs do not fit, arrows ‹ › appear (the mouse wheel scrolls too).</li>
        </ol>
        <p class="hp-tip">${icon("copy")}<span>A similar project? <b>Duplicate</b> copies the whole card including the password – then just change what differs. Delete a card you no longer need in <i>Edit card → Delete</i> or with a right click on its tab or tile (files on the PC and on the server are not touched).</span></p>
      </section>

      <section id="hp-buttons">
        <h2><span class="hp-ic am">${icon("up")}</span>Buttons</h2>
        <div class="hp-btns">
          <div><span class="hp-b am">${icon("up")}Synchronize</span><p>Compares the folders with the server and uploads new and changed files. You confirm before it starts (and tick the checklist items, if any are set).</p></div>
          <div><span class="hp-b am">${icon("gear")}Build + Synchronize</span><p>Compiles the selected builds first and uploads only when all of them succeed. <i>Build only</i> / <i>Upload only</i> does just one part.</p></div>
          <div><span class="hp-b gh">Check</span><p>A preview without uploading – shows what would be uploaded.</p></div>
          <div><span class="hp-b bl">${icon("archive")}Back up</span><p>Packs the configured folders into ZIP files with the date and time in the name.</p></div>
          <div><span class="hp-b gt">${icon("branch")}GitHub</span><p>Goes through the card's repositories, shows the changes and commits and pushes.</p></div>
          <div><span class="hp-b gh">${icon("copy")}Duplicate</span><p>A new card with the same settings, right after the original.</p></div>
        </div>
      </section>

      <section id="hp-patterns">
        <h2><span class="hp-ic rd">${icon("x")}</span>Exclusions and patterns</h2>
        <p>What should not be uploaded or backed up. Exclusions can be set for each folder pair / backup (buttons <i>folder</i>, <i>file</i>, <i>pattern</i>) and for the whole card in <i>Exclude in all folders</i> (or <i>in all backups</i>) – one rule per line.</p>
        <table class="hp-tab">
          <thead><tr><th>Pattern</th><th>Excludes</th><th>Example</th></tr></thead>
          <tbody>
            ${pat(".git/", "a folder with this name <b>anywhere</b>", "<code>.git/</code> <code>managed_components/</code>")}
            ${pat("build*/", "folders whose name <b>starts</b> with build – anywhere", "build, build_dev, build_prod")}
            ${pat("*.psd", "files by mask – anywhere", "<code>*.tmp</code> <code>~$*</code>")}
            ${pat("img/raw/", "one <b>specific</b> folder (a slash in the middle = path from the source folder)", "only img/raw, not other raw")}
            ${pat("firmware/esp32/build*/", "a mask in the given path only", "build folders only in firmware/esp32")}
            ${pat("docs/manual.pdf", "one specific file", "")}
            ${pat("photo?.jpg", "<code>?</code> = exactly one character", "photo1.jpg, photoA.jpg")}
            ${pat("# note", "a line starting with # is ignored", "")}
          </tbody>
        </table>
        <ul class="hp-list">
          <li><b>A trailing slash</b> = folders only (a file with the same name stays).</li>
          <li><code>*</code> does not cross a slash: <code>a/*/</code> matches <code>a/x/</code>, not <code>a/x/y/</code>. A pattern without a slash in the middle applies on every level.</li>
          <li>Letter case does not matter, <code>\\</code> and <code>/</code> mean the same.</li>
          <li>An excluded folder is not walked at all – this makes the comparison faster.</li>
        </ul>
      </section>

      <section id="hp-changes">
        <h2><span class="hp-ic sp">${icon("log")}</span>What gets uploaded</h2>
        <ul class="hp-list">
          <li>The file is <b>missing</b> on the server or has a <b>different size</b> → it is uploaded.</li>
          <li>The file on the PC <b>changed since the last upload</b> (Leonzilla remembers the size and time) → it is uploaded even with the same size.</li>
          <li><b>Nothing is deleted</b> on the server – files deleted on the PC stay there.</li>
          <li>Files are uploaded through a temporary file that is renamed when complete (the website never sees a half-uploaded file), and the size is verified.</li>
          <li>To be sure, tick <i>Upload all files again</i> in the confirmation.</li>
        </ul>
      </section>

      <section id="hp-build">
        <h2><span class="hp-ic am">${icon("gear")}</span>Build (RAD Studio / TMS WEB Core)</h2>
        <ul class="hp-list">
          <li>Builds are created once in <i>App settings → Builds</i> (a <code>.dproj</code> project, configuration, switches); a card just selects and orders them.</li>
          <li>Switches are read from the top part of <code>Predefines.inc</code> in a <code>{$IFNDEF LEONZILLA} … {$ENDIF}</code> block: RAD Studio uses the file as it is, a Leonzilla build skips the block and passes <code>LEONZILLA</code> + the switches ticked in the build. When a new one appears, load it with <i>Load switches</i>.</li>
          <li>Optionally line endings are converted to CRLF and the project's post-build event runs. The output (TMSWeb) is excluded from Dropbox so that Dropbox does not lock the files.</li>
          <li><b>Dropbox during build</b> (Edit card → <i>Dropbox</i>, for own script, FW ESP32 and RAD Studio; default <i>don't watch</i>): Dropbox touches files even in ignored folders and the build can then fail with “file is being used by another process”. <i>Check whether it is paused</i> – before the build Leonzilla reads the tooltip of the Dropbox tray icon; if it is not paused, it asks (stop it for the build – back within seconds – or pause it). <i>Always stop for the build</i> – stops it without asking and starts it again after the build.</li>
          <li><b>Version info</b> (in the build settings, 1 line, max 80 characters, default = build name): the post-build and the command after build get it in the <code>LEONZILLA_VERSION_INFO</code> variable – e.g. to write it into <code>version.json</code>. If your project's script reads a different variable name, add it in <i>App settings → Builds</i>. Empty = no info. The text can be changed in the build confirmation – <b>the card remembers it</b> and offers it next time; ↺ restores the default from the build settings.</li>
          <li>The progress window shows the steps and the compiler output; <i>Hide</i> keeps the build running in the background.</li>
          <li><b>Own script</b> (a section in the card settings; the card then says “Script” instead of “Build”): instead of a RAD Studio project just a <b>path to a script</b> – Python (<code>.py</code>), batch (<code>.bat</code>, <code>.cmd</code>) or PowerShell (<code>.ps1</code>) – and optional parameters. The script runs in its own folder (Python from PATH, otherwise the <code>py</code> launcher); when it ends without an error (exit code 0), the card uploads the folders to the server. On the card it looks and works like a build – <i>Build + Synchronize</i>, <i>Build only</i>, <i>Upload only</i>. When the card also has RAD Studio builds, Build light runs after them. An own <b>button label</b> (e.g. <i>Build and upload to the web</i>) replaces the default text – handy for a script that uploads by itself (such as publishing a program on a website); such a card needs no server or folders.
            <b>More scripts</b> (button <i>Add script</i>): the card then shows <i>Run scripts</i> and in the confirmation you choose <b>which one</b> script runs; the card remembers the choice. With more scripts the label field is the <i>name in the choice</i>.
            <b>More sets of parameters</b> (button <i>Add parameters</i> at a script; a set may be empty = no parameters): in the confirmation you choose which parameters the script runs with (remembered). When you remove scripts and parameters down to one, the card works as before, including its own button label – existing cards are not changed.</li>
        </ul>
      </section>

      <section id="hp-backup">
        <h2><span class="hp-ic bl">${icon("archive")}</span>Backups</h2>
        <ul class="hp-list">
          <li>Each backup = folder → target folder. It creates <code>prefix_YYYY-MM-DD_hh-mm-ss.zip</code> (empty prefix = folder name).</li>
          <li>ZIP name in the confirmation – for each backup separately, with the <i>Note | Own name</i> switch:
            <br>empty field = default <code>web_2026-10-18_10-41-14.zip</code>
            · <i>Note</i> = inserted between the name and the date <code>web_new cfg_2026-10-18_10-41-14.zip</code>
            · <i>Own name</i> = exactly your name without a date <code>fw-160.zip</code> (if it exists, <code>fw-160 (2).zip</code> is created – nothing is overwritten).
            You see the resulting name immediately; ${"<kbd>Enter</kbd>"} jumps to the next field, in the last one it starts the backup.</li>
          <li>When a card has more backups, each one has a <b>checkbox</b> in the confirmation (ticked by default). An unticked backup is skipped this time – the card settings do not change.</li>
          <li>The name fields can be turned off for the whole card: <i>Edit card → Backups → Offer a note or own ZIP name when backing up</i>. The confirmation then only shows the default ZIP names.</li>
          <li>Exclusions work as with synchronization – typically <code>.git/</code>, <code>build*/</code>, <code>managed_components/</code>.</li>
          <li>The ZIP is written as <code>.zip.part</code> and renamed only when complete. A locked file is skipped with a warning. Leonzilla never deletes old backups.</li>
        </ul>
      </section>

      <section id="hp-git">
        <h2><span class="hp-ic gt">${icon("branch")}</span>GitHub</h2>
        <ul class="hp-list">
          <li>Select the folders with git repositories in the card. Each repository knows where it belongs (<code>origin</code>).</li>
          <li>The GitHub button goes through them one by one and <b>skips those without changes</b>. Otherwise it shows the changed files (click = changes in the file), a commit message and the buttons <i>Commit + Push</i>, <i>Commit only</i>, <i>Skip</i>.</li>
          <li><b>Only ticked files</b> are committed. Push goes to the current branch (usually <code>main</code>).</li>
          <li><i>Pull changes before push</i> – when you also commit from another PC.</li>
          <li>Needs <b>Git for Windows</b>. Git handles the sign-in itself – Leonzilla stores no token.</li>
        </ul>
      </section>

      <section id="hp-fw">
        <h2><span class="hp-ic fw">${icon("chip")}</span>FW build and monitor (ESP-IDF / ESP32)</h2>
        <ul class="hp-list">
          <li><b>Monitor</b> (a checkbox in the <i>FW build and monitor</i> section): the card and its tile get a <i>Monitor</i> button – pick a COM port, <i>Open monitor</i> and it runs, even without a build or flashing. The monitor alone needs no project folder.</li>
          <li>In the card's <i>FW build</i> section: the project folder (with <code>CMakeLists.txt</code>), the ESP-IDF activation (PowerShell profile from EIM – empty = found in <code>C:\\Espressif\\tools</code>) and <b>variants</b>. The <i>Load from tools\\build_*.cmd</i> button creates them (name, build folder from <code>-B</code>, PROD scripts as “no flashing”).</li>
          <li>The <b>FW build</b> button: pick a variant, an action (<i>Build</i>, <i>Build + Flash</i>, <i>Build + Flash + Monitor</i>, <i>Monitor only</i>) and a COM port. The choice is remembered per card.</li>
          <li><b>Clean config</b> is ticked at start time (every time again) – it deletes <code>&lt;build folder&gt;\\sdkconfig</code>, which is then recreated from <code>sdkconfig.defaults</code>. Needed after changing <code>sdkconfig.defaults</code>, the profile or Kconfig.</li>
          <li><b>Reconfigure</b> (ticked by default, again each time the window opens): runs <code>tools\\build_dev.cmd reconfigure</code> before the build (reloads CMake / sdkconfig).</li>
          <li>The <b>monitor</b> is right in the window: a coloured device output (errors red, warnings yellow, <code>@…</code> replies blue) and a command line at the bottom – ${"<kbd>Enter</kbd>"} sends, ${"<kbd>↑</kbd> <kbd>↓</kbd>"} history, <i>Device reset</i> = restart via RTS. Select text in the output with the mouse and copy it with ${"<kbd>Ctrl</kbd>+<kbd>C</kbd>"}; top right <i>Copy all</i> and <i>Clear</i>. The output does not scroll while you select. <i>Hide</i> keeps the monitor running (the tile shows “FW monitor running”).</li>
          <li><i>ESP-IDF terminal</i> opens a normal PowerShell with ESP-IDF active in the project folder – for <code>menuconfig</code> and other commands.</li>
          <li>No other program (e.g. a VS Code monitor) may have the port open – Leonzilla tells you.</li>
        </ul>
      </section>

      <section id="hp-security">
        <h2><span class="hp-ic sp">${icon("lock")}</span>Passwords and security</h2>
        <div class="hp-cards">
          <div class="hp-card">
            <h3>Without a master password</h3>
            <p>Server passwords in <code>config.json</code> are <b>obfuscated</b> (AES with a key inside the app). They are not readable in the file, but whoever has the file and Leonzilla can get them.</p>
          </div>
          <div class="hp-card on">
            <h3>${icon("lock")}With a master password</h3>
            <p>Passwords are <b>encrypted</b> – the key is derived from the master password (scrypt) and encrypted with AES-256-GCM. Without the master password nobody can read them from <code>config.json</code>. Turn it on in <i>App settings</i>; Leonzilla asks for it at start.</p>
          </div>
        </div>
        <ul class="hp-list">
          <li><b>A forgotten master password cannot be recovered</b> – you then enter the server passwords again.</li>
          <li>SFTP: the server key is remembered on the first connection; when it changes, Leonzilla refuses to connect and asks.</li>
          <li>The window talks only to Leonzilla on this PC (<code>127.0.0.1</code>, a random port and token). Nothing is sent anywhere else.</li>
        </ul>
      </section>

      <section id="hp-portable">
        <h2><span class="hp-ic sp">${icon("folder")}</span>Portable and more PCs</h2>
        <ul class="hp-list">
          <li>Nothing to install: <code>Leonzilla.exe</code> and the <code>Leonzilla_data</code> folder next to it (cards, settings, upload state, logs). All of it can live in Dropbox and run on several PCs.</li>
          <li>Only the window size and position and the window cache are kept per PC (outside Dropbox, in <code>%LOCALAPPDATA%\\Leonzilla</code>).</li>
          <li>Changes from another PC are loaded when you return to the window. Do not synchronize the same card on two PCs at the same time.</li>
          <li>One Leonzilla runs per PC – starting it again switches to the open window.</li>
          <li><b>Start with Windows</b> (App settings → Start, off by default): Leonzilla starts minimized in the taskbar after you sign in. Set per PC.</li>
          <li>Needs Windows 10/11 with WebView2 (always present in Windows 11).</li>
          <li><b>Export / import.</b> <i>App settings → General → Export</i> saves cards, groups, builds and general settings into one file (default folder <code>Leonzilla_data\\export</code>) – for a backup or moving to another installation. Server passwords only when you tick <i>Including server passwords</i>, then encrypted with an export password. <i>Import…</i> shows what the file contains and only <b>adds</b> the ticked cards, groups and builds – it overwrites nothing (a group or build with the same name is reused) and backs up <code>config.json</code> to <code>Leonzilla_data\\backup</code> first. A tab that has something set is green with a dot and a short summary (e.g. “2 scripts”); tabs with nothing set are grey. The arrow keys ← → switch between the tabs.</li>
          <li><b>Language</b> – Czech or English: <i>App settings → Language</i> (default by the Windows display language).</li>
        </ul>
      </section>

      <section id="hp-keys">
        <h2><span class="hp-ic sp">${icon("sliders")}</span>Keys and tricks</h2>
        <div class="hp-keys">
          <div>${k("Ctrl")}+${k("T")}<span>new card</span></div>
          <div>${k("Ctrl")}+${k("Tab")}<span>next card (${k("Shift")} = previous)</span></div>
          <div>${k("Ctrl")}+${k("Enter")}<span>in a commit message = Commit + Push</span></div>
          <div>${k("Esc")}<span>closes a window / dialog</span></div>
          <div>${k("Ctrl")}+${k("W")}<span>closes the card tab (the card stays in the Overview)</span></div>
          <div>${k("right click")}<span>on a tab or tile: edit, duplicate, colour, delete (the colour can also be changed at the top of the card editor)</span></div>
          <div>${k("double click")}<span>on the tab of a card being edited = rename</span></div>
        </div>
        <ul class="hp-list">
          <li>Tabs and tiles can be dragged (order, groups).</li>
          <li><b>Click the logo</b> top left = <i>About</i> (version, links to GitHub and the latest version, bug reports).</li>
          <li>The <i>Log</i> button opens the card's full log; after a crash the output is in <code>Leonzilla_data\\logs\\crash.log</code>.</li>
        </ul>
      </section>

      <footer class="hp-thanks" id="hp-thanks">
        <div class="hp-mark big"><svg><use href="#i-mark"/></svg></div>
        <p class="hp-ty">Thank you for using Leonzilla.<br>Enjoy it!</p>
        <p class="hp-sign">© ${new Date().getFullYear()} Leoš Vilím · <a href="https://leotech.cz" data-url>leotech.cz</a> · <a href="https://github.com/Leontech/leonzilla/issues" data-url>report a bug / idea</a></p>
        <p class="hp-sign"><a href="https://ko-fi.com/leotechcz" data-url>☕ Support development – buy the author a coffee</a></p>
      </footer>
    </div>
  </div>`;
}
