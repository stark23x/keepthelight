package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ---------- -profile for the command line ----------
//
// "keepthelight_cli -profile "Plex 4K" film.mkv" uses a profile saved in the
// window. The profile becomes ordinary options placed first, so any option
// given on the command line after it wins.

type savedProfile struct {
	Name       string          `json:"name"`
	UseNameTag bool            `json:"useNameTag"`
	Settings   json.RawMessage `json:"settings"`
}

// takeProfileArg removes "-profile <name>" (or -profile=name) from args.
func takeProfileArg(args []string) (name string, rest []string, found bool) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-profile" || a == "--profile":
			if i+1 < len(args) {
				name, found = args[i+1], true
				i++
				continue
			}
			found = true
			continue
		case strings.HasPrefix(a, "-profile=") || strings.HasPrefix(a, "--profile="):
			name, found = a[strings.Index(a, "=")+1:], true
			continue
		}
		rest = append(rest, a)
	}
	return name, rest, found
}

func loadProfiles(prefsFile string) ([]savedProfile, error) {
	b, err := os.ReadFile(prefsFile)
	if err != nil {
		return nil, fmt.Errorf("no profiles saved yet (%v)", err)
	}
	var prefs map[string]json.RawMessage
	if err := json.Unmarshal(b, &prefs); err != nil {
		return nil, fmt.Errorf("the settings file could not be read: %v", err)
	}
	var ps []savedProfile
	_ = json.Unmarshal(prefs["profiles"], &ps)
	return ps, nil
}

// profileArgs turns a saved profile into command-line options, and says which
// format (".mkv"/".mp4") it uses, if any.
func profileArgs(prefsFile, name string) ([]string, string, error) {
	ps, err := loadProfiles(prefsFile)
	if err != nil {
		return nil, "", err
	}
	var p *savedProfile
	var names []string
	for i := range ps {
		names = append(names, ps[i].Name)
		if strings.EqualFold(strings.TrimSpace(ps[i].Name), strings.TrimSpace(name)) {
			p = &ps[i]
		}
	}
	if p == nil {
		if len(names) == 0 {
			return nil, "", fmt.Errorf("no profile named %q (no profiles are saved; make them in the window)", name)
		}
		return nil, "", fmt.Errorf("no profile named %q; saved profiles: %s", name, strings.Join(names, ", "))
	}
	var s settings
	if err := json.Unmarshal(p.Settings, &s); err != nil {
		return nil, "", fmt.Errorf("profile %q could not be read: %v", p.Name, err)
	}
	var extra struct {
		Format string `json:"format"`
	}
	_ = json.Unmarshal(p.Settings, &extra)
	format := ""
	if extra.Format == ".mkv" || extra.Format == ".mp4" {
		format = extra.Format
	}
	// toConfig needs files; these stand in for them and are dropped again
	s.Input, s.Output = "in.mkv", "out"+map[bool]string{true: format, false: ".mkv"}[format != ""]
	cfg, err := s.toConfig()
	if err != nil {
		return nil, "", fmt.Errorf("profile %q: %v", p.Name, err)
	}
	a := cfg.cliArgs()
	a = a[:len(a)-2] // the stand-in files
	if p.UseNameTag {
		a = append(a, "-tag", "_"+cleanTag(strings.Join(strings.Fields(p.Name), " ")))
	}
	return a, format, nil
}

// defaultPrefsFile is where the window keeps its settings.
func defaultPrefsFile() string { return filepath.Join(appDataDir(), "settings.json") }
