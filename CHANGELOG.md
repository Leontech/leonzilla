# Changelog

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
