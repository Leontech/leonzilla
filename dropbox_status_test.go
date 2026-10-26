package main

// pause texts of the Dropbox tray tooltip in several languages (Dropbox check before a build)
import "testing"

func TestPausedRe(t *testing.T) {
	yes := []string{"Dropbox 277.3.4952 | Syncing paused", "Synchronizace pozastavena", "Synchronisierung angehalten", "Synchronisierung pausiert", "Synchronisatie gepauzeerd", "Synchronizacja wstrzymana", "Синхронізацію призупинено", "Synchronisation en pause", "同期を一時停止しました", "同步已暂停", "동기화 일시 중지됨", "Eşitleme duraklatıldı"}
	no := []string{"Dropbox 277.3.4952 | Up to date", "Aktuální", "Syncing 3 files", "Auf dem neuesten Stand"}
	for _, s := range yes {
		if !dropboxPausedRe.MatchString(s) {
			t.Error("should match:", s)
		}
	}
	for _, s := range no {
		if dropboxPausedRe.MatchString(s) {
			t.Error("should not match:", s)
		}
	}
}
