package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func callJSON(t *testing.T, g *guiServer, path string, body any) map[string]any {
	t.Helper()
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	} else {
		raw = []byte("null") // what the window bridge sends for a GET
	}
	u, err := url.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(g.call(context.Background(), u.Path, u.Query(), raw))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCallDispatch(t *testing.T) {
	g := newGUIServer("")
	if m := callJSON(t, g, "nope", nil); m["ok"] != false || !strings.Contains(m["error"].(string), "unknown") {
		t.Errorf("unknown request: %v", m)
	}
	if m := callJSON(t, g, "status?since=0", nil); m["active"] != false {
		t.Errorf("status without a job: %v", m)
	}
	if m := callJSON(t, g, "probe", map[string]any{"input": ""}); m["ok"] != false || m["error"] != "" {
		t.Errorf("probe without input: %v", m)
	}
	missing := filepath.Join(t.TempDir(), "missing.mkv")
	if m := callJSON(t, g, "probe", map[string]any{"input": missing}); m["ok"] != false || !strings.Contains(m["error"].(string), "not found") {
		t.Errorf("probe of a missing file: %v", m)
	}
	if m := callJSON(t, g, "start", map[string]any{"input": missing, "output": "x.mkv"}); m["ok"] != false {
		t.Errorf("start with a missing input: %v", m)
	}
	if m := callJSON(t, g, "start", map[string]any{"input": missing, "codec": "h264"}); m["error"] != "Unknown encoder" {
		t.Errorf("start with a bad encoder: %v", m)
	}
	if m := callJSON(t, g, "tools", nil); len(m) != 7 || m["vcruntime"] == nil || m["problems"] == nil {
		t.Errorf("tools: %v", m)
	}
	var got string
	g.notify = func(s string) { got = s }
	callJSON(t, g, "message", map[string]any{"text": "hello"})
	if got != "hello" {
		t.Errorf("message not delivered: %q", got)
	}
}

func TestCallBadBody(t *testing.T) {
	g := newGUIServer("")
	for _, name := range []string{"probe", "start"} {
		m := g.call(context.Background(), name, nil, []byte("{bad")).(map[string]any)
		if m["ok"] != false || m["error"] != "bad request" {
			t.Errorf("%s with a bad body: %v", name, m)
		}
	}
}

func TestPageSubstitution(t *testing.T) {
	g := newGUIServer(`C:\Videos\a "b" </script>.mkv`)
	p := string(g.page())
	if !strings.Contains(p, "v"+version) || strings.Contains(p, "__VERSION__") || strings.Contains(p, "__INITIAL_FILE__") {
		t.Fatal("placeholders not replaced")
	}
	if strings.Contains(p, `"b" </script>`) {
		t.Fatal("initial file not escaped for a script block")
	}
}

func TestServerAuth(t *testing.T) {
	g := newGUIServer("")
	g.addr = "127.0.0.1:1234"
	h := g.auth(g.handleAPI)
	for _, c := range []struct {
		host, url string
		hdr       string
		want      int
	}{
		{"127.0.0.1:1234", "/api/status", "", http.StatusForbidden},
		{"127.0.0.1:1234", "/api/status", g.token, http.StatusOK},
		{"127.0.0.1:1234", "/api/status?t=" + g.token, "", http.StatusOK},
		{"evil.example:1234", "/api/status", g.token, http.StatusForbidden}, // DNS rebinding
	} {
		r := httptest.NewRequest("GET", c.url, nil)
		r.Host = c.host
		if c.hdr != "" {
			r.Header.Set("X-Token", c.hdr)
		}
		w := httptest.NewRecorder()
		h(w, r)
		if w.Code != c.want {
			t.Errorf("%s %s token=%q: got %d want %d", c.host, c.url, c.hdr, w.Code, c.want)
		}
	}
}

func TestPrefsRoundTrip(t *testing.T) {
	g := newGUIServer("")
	g.prefsPath = filepath.Join(t.TempDir(), "sub", "settings.json")
	if m := callJSON(t, g, "prefs", nil); len(m["prefs"].(map[string]any)) != 0 {
		t.Fatalf("fresh prefs not empty: %v", m)
	}
	state := map[string]any{"files": true, "video": false}
	if m := callJSON(t, g, "setPref", map[string]any{"name": "sections", "value": state}); m["ok"] != true {
		t.Fatalf("setPref: %v", m)
	}
	g2 := newGUIServer("") // a later run reads the same file
	g2.prefsPath = g.prefsPath
	got := callJSON(t, g2, "prefs", nil)["prefs"].(map[string]any)["sections"].(map[string]any)
	if got["files"] != true || got["video"] != false {
		t.Fatalf("prefs not kept: %v", got)
	}
	if m := callJSON(t, g, "setPref", map[string]any{"name": "", "value": 1}); m["ok"] != false {
		t.Fatalf("empty name accepted: %v", m)
	}
}

func TestOpenOnlyWebAndMailLinks(t *testing.T) {
	g := newGUIServer("")
	for _, u := range []string{"javascript:alert(1)", "file:///C:/Windows/System32/calc.exe", `C:\Windows\notepad.exe`, ""} {
		if m := callJSON(t, g, "open", map[string]any{"url": u}); m["ok"] != false {
			t.Errorf("%q accepted: %v", u, m)
		}
	}
}

func TestAboutInPage(t *testing.T) {
	p := string(newGUIServer("").page())
	for _, want := range []string{
		"Keep the Light is free to all.", `href="mailto:stark23x@gmail.com"`, // the author's text
		"Version " + version, "written with the help of Claude",
		"data:image/png;base64,", "John Chadwick &lt;john@jchw.io&gt;", // icon; licence text escaped
	} {
		if !strings.Contains(p, want) {
			t.Errorf("page is missing %q", want)
		}
	}
	for _, ph := range []string{"__ABOUT__", "__NOTICES__", "__ICON__", "__VERSION__"} {
		if strings.Contains(p, ph) {
			t.Errorf("placeholder %s left in the page", ph)
		}
	}
	if strings.Count(p, `class="logo" src="data:image/png;base64,`) != 1 {
		t.Error("header logo missing")
	}
	if len(p) > 1_500_000 { // WebView2 shows pages of up to 2 MB this way
		t.Errorf("page is %d bytes", len(p))
	}
}

func TestSavedTmpDirMissing(t *testing.T) {
	g := newGUIServer("")
	g.prefsPath = filepath.Join(t.TempDir(), "settings.json")
	existing := t.TempDir()
	callJSON(t, g, "setPref", map[string]any{"name": "tmpDir", "value": existing})
	if m := callJSON(t, g, "prefs", nil); m["tmpDirMissing"] != nil || m["prefs"].(map[string]any)["tmpDir"] != existing {
		t.Fatalf("existing temp folder: %v", m)
	}
	gone := filepath.Join(existing, "deleted")
	callJSON(t, g, "setPref", map[string]any{"name": "tmpDir", "value": gone})
	if m := callJSON(t, g, "prefs", nil); m["tmpDirMissing"] != gone {
		t.Fatalf("missing temp folder not reported: %v", m)
	}
	callJSON(t, g, "setPref", map[string]any{"name": "tmpDir", "value": ""})
	if m := callJSON(t, g, "prefs", nil); m["tmpDirMissing"] != nil {
		t.Fatalf("cleared temp folder reported as missing: %v", m)
	}
}
