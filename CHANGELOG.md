# Changelog

## 1.11.20 – 2026-10-29

**Remote access from a phone**
- *App settings → Remote access* (its own tab): run cards from your phone over the local network or a VPN (e.g. WireGuard). Off by default, switched on per PC; set a password, open the shown address on the phone (QR code), add it to the home screen.
- **Only running** what the cards have: synchronize, builds, own scripts (choice of script and parameters), switches for one build, backups, GitHub, FW build and flash – with progress and log. No card or app settings from the phone, nothing is opened on the PC; the server enforces it.
- **Security:** only private / VPN addresses are accepted (never forward the port to the internet – plain HTTP, the VPN encrypts it); the password is stored as a scrypt hash; per-device sessions that can be signed out; 10-minute block after 5 wrong attempts.
- The phone shows the Dropbox status of the PC in the top bar (*Up to date*, syncing, paused – the text of the Dropbox tray icon, or *stopped for a build* while Leonzilla has it stopped), nothing when Dropbox does not run.
- The tiles and dialogs are laid out for a phone (one column, bigger buttons, dialogs from the bottom).

**Other**
- A job started from the phone (or from the window) is shown on the other side too – progress while it runs, and the "last run" line when it is done.
- Remote access has its own tab in the app settings (on / off, number of signed-in devices at a glance).
- The phone sign-in tells clearly which password goes where: step 1 the remote-access password, step 2 (only with a master password) the Leonzilla master password. When Leonzilla on the PC does not respond, the phone says so instead of clearing the field.
- The phone sign-in form no longer rebuilds itself every few seconds (it cleared the password being typed).
- Signing in again from the same phone / browser replaces its old session – the list of signed-in devices shows each device once.
- The QR code of the remote-access address is square again (it was stretched to the full width of the settings and phones could not read it).
- Switching the tabs of the card settings or the app settings no longer moves the page or the window – the tabs stay where they are.
- Leonzilla writes `%LOCALAPPDATA%\Leonzilla\activity.json` (a job is running, time of the last request from the phone), so another program – e.g. one that puts the PC to sleep after a remote session – knows Leonzilla is still in use.

## 1.11.19 – 2026-10-28

- **Switches for one build only.** In the confirmation of a RAD Studio build, click a switch to strike it out for this run (click again to put it back), or add another one with *+ switch* (the known switches of the build are offered, a new name can be typed). The changed switches are used only for this one build – the build setting is not changed, and the next time the original switches are back. The build log shows the change (e.g. `−DEBUG_PANEL +NEW_FEATURE`).

## 1.11.18 – 2026-10-28

- **Balanced buttons on the tiles.** A card that synchronizes to a server keeps *Check* + *Synchronize* as the main buttons with small chips for backup, GitHub or FW. A card **without** synchronization has no single main action any more: its own script, FW build, Monitor, Back up and GitHub are buttons of the same size, two per row – a button with a long label, or an odd last one, takes the whole row. Before, the first action was big and the others were tiny chips (e.g. a big script button next to a tiny FW button).
- Only the layout of the tiles changed – no settings and no cards are touched.

## 1.11.17 – 2026-10-27

**Own scripts**
- **More scripts per card** – *Edit card → Own script → Add script*. With more than one script the card's button says *Run scripts* and the confirmation lets you choose **which one** runs; the card remembers the choice.
- **More sets of parameters per script** – *Add parameters* (a set may be empty = no parameters). With more sets the confirmation asks which one to use; remembered as well.
- Cards with one script and one set of parameters work exactly as before, including their own button label.

**Clearer settings**
- **Card settings in tabs** – *Synchronization*, *Own script*, *Backups*, *GitHub*, *FW ESP32* and *Build (RAD Studio)* last. A tab that has something set is highlighted with a green dot and a short summary (e.g. "2 scripts"), so you see at a glance what the card does.
- **App settings in tabs** – *General* (theme, language, start with Windows, parallel uploads, master password, export / import), *Groups*, *Builds (RAD Studio)*.

**Export / import**
- *App settings → General → Export* saves cards, groups, builds and general settings into one JSON file (default folder `Leonzilla_data\export`) – for backups or moving to another installation.
- Server passwords only on request (*Including server passwords*), then encrypted with an export password (scrypt + AES-256-GCM). Without it the file contains no passwords.
- *Import…* shows what the file contains; tick the cards, groups and builds to **add**. Nothing existing is overwritten: imported cards are new cards, groups and builds that already exist are reused, and `config.json` is backed up to `Leonzilla_data\backup` first.

**Other**
- An unchanged server password is no longer re-encrypted on every save, so `config.json` changes only when something really changed (fewer Dropbox uploads and conflicts).
- Existing cards are not changed by this version – new settings are stored only when you use the new features.

## 1.11.16 – 2026-10-27

- **Card changes are saved only with Save.** Editing a card is now a draft: *Save* writes it and locks the card, *Close without saving* puts the card back as it was (with a confirmation if something changed). Unsaved changes stay when you switch to another card (● on the tab); closing the tab asks whether to save. Before, every change was saved immediately.
- **App settings** start with the everyday options – theme, language, start with Windows, parallel uploads, master password – followed by groups and builds. A new section explains what is shared by all PCs through Dropbox (cards and their order, groups, servers, builds, language, theme …) and what is kept per PC (collapsed groups, open tabs, last card, window size and position, start with Windows).
- The global "stop Dropbox during RAD Studio builds" option is gone from App settings – Dropbox is set per card (*Edit card → Dropbox*). Existing cards are not changed.

## 1.11.14 – 2026-10-26

- The Dropbox check is now **per card** instead of one global setting: *Edit card → Dropbox* – *don't watch* (default), *check pause, otherwise ask*, or *always stop it for the build*. It applies to the card's own script, FW build (ESP32) and RAD Studio builds; synchronization, backups and GitHub are never affected.

## 1.11.13 – 2026-10-26

- **Dropbox check before a build** (App settings → Builds, on by default) for all builds – RAD Studio, own script and FW build (ESP32). Dropbox touches files even in ignored folders, and builds then fail with "file is being used by another process". If Dropbox runs and syncing is not paused, Leonzilla asks before the build: **stop Dropbox for the build** (it is back and up to date within seconds) or pause it yourself. *Next time stop it automatically* – or the setting *Always stop Dropbox during a build* – skips the question.
- The paused state is read from the tooltip of the Dropbox tray icon ("Syncing paused") in several languages; if it cannot be recognised, *Paused, continue* warns once and then lets you continue anyway.
- *FW build* window: **Clean config** and **Reconfigure** are remembered per card.
- Card settings → *FW build and monitor*: the **build folder** column is as wide as the script column, so longer paths fit.
- Fix: in the *FW build* window both variants could look selected (and the run got no variant) after adding a variant with *Add variant* in the same session.

## 1.11.8 – 2026-10-25

- **"Build light" is now called "Own script"** in the card settings. A card that only runs its own script says *Script* everywhere instead of *Build* (tile, card, button *Run script*, *Script only*, *Last script run*, confirmation, progress window, log). Cards with RAD Studio builds keep *Build*. Nothing changes in `config.json` – existing cards work as before.

## 1.11.7 – 2026-10-25

- A card that only runs its own script (Build light, no server) now ends with *Script “…” finished without errors* instead of the misleading *nothing uploaded (build only)* – Leonzilla does not know whether the script uploads by itself. The *nothing uploaded* note stays for cards with a server when you choose *Build only*.

## 1.11.6 – 2026-10-25

- **Card colour at the top of the card editor** (next to the group) – before it was only in the tab's right-click menu.

## 1.11.5 – 2026-10-25

- Build light: the build list and the confirmation show the real script type (*Python*, *BAT*, *CMD*, *PowerShell*) instead of always *Python*.

## 1.11.4 – 2026-10-25

- **Build light runs batch and PowerShell scripts too**: besides Python (`.py`) the script can be `.bat` / `.cmd` (through cmd) or `.ps1` (PowerShell with `-ExecutionPolicy Bypass`); the file picker offers all of them.
- **Reliable autosave in a Dropbox folder**: while typing e.g. a card name, Dropbox (or OneDrive) briefly locks the just-saved `config.json` while uploading it, which showed "Saving failed" bubbles. Saving now waits for quiet typing a bit longer and retries the write for a few seconds (plus one quiet retry in the window) before reporting an error.

## 1.11.3 – 2026-10-25

- **Collapsed groups are remembered per computer** (also open tabs and the last card): saved in `Leonzilla_data\ui\<computer>.json`. Before, the window storage was used, which was lost on every start because the window runs on a random local port.

## 1.11.2 – 2026-10-25

- **Own button label for Build light**: *Card settings → Build light → Button label*, e.g. *Build and upload to my web*. The main card button then shows this text instead of *Build + Synchronize* / *Build* – handy for a script that uploads by itself (such as publishing a program to a website); such a card needs no server or folders.

## 1.11.1 – 2026-10-25

- **Report a bug / idea** in the About window now opens a short form on [leotech.cz](https://leotech.cz/download#leonzilla) with the program and version filled in – no GitHub account needed (GitHub Issues still work too).

## 1.11.0 – 2026-10-25

First public release.

- **English UI** next to Czech: *App settings → Language* (by default follows the Windows display language). Built-in guide and PDF manual in both languages.
- **Version info** for builds is passed to the post-build and the command after build in `LEONZILLA_VERSION_INFO`; an extra variable name for your own scripts can be set in *App settings → Builds*.
- `Predefines.inc` without a `{$IFNDEF LEONZILLA}` block offers the switches of the whole file.
- **About** window – click the logo in the top bar (also linked from the app settings): version, GitHub, latest release, bug reports, author and a ☕ [Ko-fi](https://ko-fi.com/leotechcz) link to support development.
- Tile buttons wrap instead of overflowing; build list in the settings aligned.
- Generic examples in input fields, author contact via [leotech.cz](https://leotech.cz) and GitHub Issues.
- MIT license.

## 1.10.0 – 2026-10-24

- **Build light**: a card can run its own Python script (path + parameters in the card settings) and upload to the server when it succeeds; on the card it behaves like a build.

## 1.9.0 – 1.9.3 – 2026-10-21

- **FW build for ESP32 / ESP-IDF**: variants from `tools\build_*.cmd`, Build / Build + Flash / + Monitor / Monitor only, COM ports, Clean config and Reconfigure, ESP-IDF terminal.
- Own **serial monitor** with an input line, history, device reset (RTS), text selection, *Copy all* and *Clear*; *Monitor* button also for cards without a FW build.
- Fix: a value picked in a file / folder dialog could be lost when the cards were reloaded at the same moment.

## 1.8.0 – 1.8.3 – 2026-10-19

- Tabs at the top only for open cards (✕, Ctrl+W, middle click, scroll arrows), remembered per PC.
- *Version info* per build, editable in the build confirmation and remembered per card.
- Optional start with Windows (minimized, per PC).

## 1.7.0 – 1.7.7 – 2026-10-17 / 18

- Built-in guide (**?** / F1) with chapters, pattern table and security notes; PDF version.
- Backups: a note or a completely own name for each ZIP, untick single backups for one run, clearer exclusions in the confirmation.
- Fixes: the ⋯ menu of a folder pair closed immediately; every pair now also has a direct ✕.

## 1.6.0 – 1.6.2 – 2026-10-17

- **GitHub**: repositories per card, changed files with diffs, Commit + Push / Commit only / Skip, optional pull --rebase before push.
- *Duplicate* a card (button and right click).

## 1.5.0 – 2026-10-17

- **Backups** of folders to ZIP files with date and time in the name, exclusions, progress window.

## 1.4.0 – 1.4.3 – 2026-10-15

- **Builds of RAD Studio / TMS WEB Core projects** without the IDE: build definitions in the app settings, switches from `Predefines.inc`, CRLF conversion, the project's post-build event, *Build + Synchronize / Build only / Upload only*.
- Build output excluded from Dropbox (so Dropbox cannot lock fresh files), optional Dropbox stop during builds, automatic retry when a file is locked.

## 1.0.0 – 1.3.0 – 2026-10-13

- First version: one-way synchronization of folder pairs to FTP / FTPS / SFTP, temporary-file uploads, exclusions, master password.
- Overview with tiles, groups, locked cards, checklist before synchronization, remembered window size.
