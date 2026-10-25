package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// A sound when a run (one file, or a whole queue) has finished: one for
// "all done", another when a file failed. A sound is "" (none),
// "alias:<Windows sound event>" (follows the Windows sound scheme),
// "media:<file.wav>" (from Windows' Media folder) or "file:<path to a .wav>".

type soundPrefs struct {
	Done  string `json:"done"`
	Fail  string `json:"fail"`
	Flash bool   `json:"flash"`
}

var defaultSounds = soundPrefs{Done: "media:notify.wav", Fail: "alias:SystemHand", Flash: true}

// the Windows sound events offered, in the order shown
var soundAliases = [][2]string{
	{"alias:Notification.Default", "Notification"}, {"alias:Notification.Reminder", "Reminder"},
	{"alias:.Default", "Default beep"}, {"alias:SystemAsterisk", "Information (asterisk)"},
	{"alias:SystemExclamation", "Warning (exclamation)"}, {"alias:SystemHand", "Critical stop"},
	{"alias:MailBeep", "New mail notification"}, {"alias:DeviceConnect", "Device connected"},
}

func (g *guiServer) soundPrefs() soundPrefs {
	sp := defaultSounds
	if raw, ok := g.loadPrefs()["sounds"]; ok {
		_ = json.Unmarshal(raw, &sp)
	}
	return sp
}

// soundList is what the window offers: the sound events and the .wav files
// that come with Windows.
func soundList() map[string]any {
	var aliases, media []map[string]string
	for _, a := range soundAliases {
		aliases = append(aliases, map[string]string{"id": a[0], "name": a[1]})
	}
	entries, _ := os.ReadDir(mediaDir())
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".wav") {
			names = append(names, e.Name())
		}
	}
	sort.Slice(names, func(i, j int) bool { return strings.ToLower(names[i]) < strings.ToLower(names[j]) })
	for _, n := range names {
		media = append(media, map[string]string{"id": "media:" + n, "name": capitalize(strings.TrimSuffix(n, filepath.Ext(n)))})
	}
	return map[string]any{"ok": true, "aliases": aliases, "media": media}
}

// playSoundFn plays a sound; tests replace it.
var playSoundFn = playSound

// playSound plays a sound by its id. A missing file falls back to the
// Windows notification sound, so a deleted .wav does not mean silence.
func playSound(id string, wait bool) bool {
	kind, name, _ := strings.Cut(id, ":")
	switch kind {
	case "alias":
		return playSoundRaw(name, true, wait)
	case "media":
		name = filepath.Join(mediaDir(), filepath.Base(name))
	case "file":
	default:
		return false
	}
	if !fileExists(name) {
		return playSoundRaw("Notification.Default", true, wait)
	}
	return playSoundRaw(name, false, wait)
}

// finishedAlert tells the user a run is over: a sound, and the taskbar button
// flashing until the window is used.
func (g *guiServer) finishedAlert(failed bool) {
	sp := g.soundPrefs()
	id := sp.Done
	if failed {
		id = sp.Fail
	}
	if id != "" {
		playSoundFn(id, false)
	}
	if sp.Flash {
		flashTaskbar()
	}
}
