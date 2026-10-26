package main

import "regexp"

// DropboxStatus – for "Watch Dropbox before a build" (GET /api/dropbox)
type DropboxStatus struct {
	Running bool   `json:"running"`
	Known   bool   `json:"known"`  // pause state could be read (tray icon tooltip)
	Paused  bool   `json:"paused"` // only meaningful when Known
	Tray    string `json:"tray,omitempty"`
}

// texts Dropbox shows in the tray tooltip while syncing is paused, in the languages of the Dropbox app:
// cs/sk pozastaven(á), en/de/fr/es/it/pt/sv/da/nb paus…, de angehalten, nl gepauzeerd, pl wstrzymana,
// uk призупинено, fi keskeytetty, tr duraklatıldı, id/ms dijeda, ja 一時停止, zh 暂停/暫停, ko 일시 중지, it sospesa, th หยุดชั่วคราว
// If the language is not covered, the dialog still lets the user continue (the check is only a reminder).
var dropboxPausedRe = regexp.MustCompile(`(?i)pozastav|paus|pauz|angehalten|wstrzyma|призупин|keskeyt|duraklat|dijeda|一時停止|暂停|暫停|일시\s?중지|sospes|หยุดชั่วคราว`)
