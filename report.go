package main

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ---------- the report of a queue run ----------
//
// When a queue run ends, what happened to each file is written to one page:
// its size before and after, the space saved, how long it took, the settings,
// its warnings and anything that had to be done another way than planned (a
// fallback). The page is kept in the logs folder (the newest 20) and can be
// opened from the Queue tab.

const keepReports = 20

func reportsDir() string { return filepath.Join(logsDir(), "reports") }

// reportRow is one file of the run.
type reportRow struct {
	Name, Input, Output string
	Status, Note        string
	Settings            string
	InSize, OutSize     int64
	Took                float64
	Warnings, Fallbacks []string
	Check               string // the thorough check's line
}

// fallbackMarks are the log lines that mean "done another way than planned".
var fallbackMarks = []string{
	"Trying again the normal way",
	"Trying again with the processor decoding",
	"decoded on the processor (the faster ways did not pass",
	"GPU decoding: not ",
	"Remuxing it with them in order",
	"so CRF ", // quality aim: a setting that would make the file bigger
	"leaving the choice of device to FFmpeg",
}

// jobFindings picks a job's warnings, fallbacks and thorough-check line out of its log.
func jobFindings(lines []string) (warnings, fallbacks []string, check string) {
	seen := map[string]bool{}
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || seen[l] {
			continue
		}
		fb := false
		for _, m := range fallbackMarks {
			if strings.Contains(l, m) {
				fb = true
				break
			}
		}
		switch {
		case strings.HasPrefix(l, "Thorough check: passed"), strings.HasPrefix(l, "WARNING: Thorough check:"):
			check = strings.TrimPrefix(l, "WARNING: ")
			if strings.HasPrefix(l, "WARNING:") {
				seen[l] = true
				warnings = append(warnings, check)
			}
		case fb:
			seen[l] = true
			fallbacks = append(fallbacks, strings.TrimPrefix(l, "WARNING: "))
		case strings.HasPrefix(l, "WARNING:"):
			seen[l] = true
			warnings = append(warnings, strings.TrimSpace(strings.TrimPrefix(l, "WARNING:")))
		}
	}
	return warnings, fallbacks, check
}

// settingsWords describes a job's settings in a few words ("x265 · CRF 18 · medium · 1080p").
func settingsWords(cfg *Config) string {
	if cfg == nil {
		return ""
	}
	switch {
	case cfg.MetaOnly:
		return "metadata only"
	case cfg.CopyVideo:
		return "copy video"
	}
	parts := []string{map[string]string{"x265": "x265", "nvenc": "NVENC", "qsv": "QuickSync"}[cfg.Codec]}
	switch {
	case cfg.QualityTarget > 0:
		parts = append(parts, fmt.Sprintf("aim for VMAF %g", cfg.QualityTarget))
	case cfg.TargetBytes > 0:
		parts = append(parts, "target "+fmtSize(cfg.TargetBytes))
	case cfg.Bitrate != "":
		parts = append(parts, cfg.Bitrate)
	default:
		parts = append(parts, fmt.Sprintf("CRF %d", cfg.CRF))
	}
	parts = append(parts, cfg.Preset)
	if cfg.Scale != "" {
		parts = append(parts, cfg.Scale)
	}
	if cfg.Denoise != "" {
		parts = append(parts, "noise reduction "+denoiseNames[cfg.Denoise])
	}
	return strings.Join(parts, " · ")
}

func tookWords(secs float64) string {
	d := time.Duration(secs) * time.Second
	switch {
	case secs <= 0:
		return ""
	case d < time.Minute:
		return fmt.Sprintf("%d s", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()+0.5))
	}
	return fmt.Sprintf("%d h %02d min", int(d.Hours()), int(d.Minutes())%60)
}

// writeReport writes the page and returns its path ("" when it could not be written).
func writeReport(rows []reportRow, started, ended time.Time) string {
	if len(rows) == 0 {
		return ""
	}
	dir := reportsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	p := filepath.Join(dir, ended.Format("2006-01-02 15-04-05")+" queue report.html")
	if err := os.WriteFile(p, []byte(reportHTML(rows, started, ended)), 0o644); err != nil {
		return ""
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "*.html")); len(m) > keepReports {
		sort.Strings(m) // the names start with the date and time
		for _, old := range m[:len(m)-keepReports] {
			_ = os.Remove(old)
		}
	}
	return p
}

func reportHTML(rows []reportRow, started, ended time.Time) string {
	esc := html.EscapeString
	var done, failed, other int
	var in, out int64
	var took float64
	for _, r := range rows {
		switch r.Status {
		case stDone:
			done++
			if r.InSize > 0 && r.OutSize > 0 {
				in += r.InSize
				out += r.OutSize
			}
		case stFailed:
			failed++
		default:
			other++
		}
		took += r.Took
	}
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Keep the Light: queue report</title><style>
:root{color-scheme:light dark;--bg:#f3f4f6;--panel:#fff;--line:#d3d8e0;--text:#1b1f27;--muted:#566070;--ok:#1e8150;--warn:#8f5c00;--bad:#c42b2b}
@media (prefers-color-scheme:dark){:root{--bg:#14161a;--panel:#1d2026;--line:#333844;--text:#e6e8ec;--muted:#b9c0cc;--ok:#4cc38a;--warn:#f0b74a;--bad:#ff6b6b}}
body{font:14px/1.5 "Segoe UI",system-ui,sans-serif;background:var(--bg);color:var(--text);margin:0;padding:24px}
main{max-width:1100px;margin:0 auto}h1{font-size:20px;margin:0 0 4px}.sub{color:var(--muted);margin-bottom:16px}
.tot{display:flex;gap:12px;flex-wrap:wrap;margin-bottom:18px}.tot div{background:var(--panel);border:1px solid var(--line);border-radius:8px;padding:10px 14px;min-width:130px}
.tot b{display:block;font-size:18px}.tot span{color:var(--muted);font-size:12.5px}
.file{background:var(--panel);border:1px solid var(--line);border-radius:8px;padding:12px 14px;margin-bottom:10px}
.file h2{font-size:15px;margin:0 0 2px;overflow-wrap:anywhere}.path{color:var(--muted);font-size:12.5px;overflow-wrap:anywhere}
.facts{margin:6px 0 0;display:flex;gap:6px 18px;flex-wrap:wrap}.ok{color:var(--ok)}.warn{color:var(--warn)}.bad{color:var(--bad)}
ul{margin:6px 0 0;padding-left:20px}li{overflow-wrap:anywhere}.st{font-weight:600}
</style></head><body><main>`)
	fmt.Fprintf(&b, "<h1>Keep the Light: queue report</h1><div class=\"sub\">Keep the Light %s · %s to %s</div>", esc(version),
		esc(started.Format("2 January 2006, 15:04")), esc(ended.Format("15:04")))
	b.WriteString(`<div class="tot">`)
	fmt.Fprintf(&b, "<div><b>%d</b><span>%s converted</span></div>", done, plural(done, "file", "files"))
	if failed > 0 {
		fmt.Fprintf(&b, "<div><b class=\"bad\">%d</b><span>failed</span></div>", failed)
	}
	if other > 0 {
		fmt.Fprintf(&b, "<div><b>%d</b><span>skipped or canceled</span></div>", other)
	}
	if in > 0 {
		fmt.Fprintf(&b, "<div><b>%s → %s</b><span>size before and after</span></div>", esc(formatBytes(in)), esc(formatBytes(out)))
		switch d := in - out; {
		case d > 0:
			fmt.Fprintf(&b, "<div><b class=\"ok\">%s</b><span>saved (%d%%)</span></div>", esc(formatBytes(d)), int(float64(d)*100/float64(in)+0.5))
		case d < 0:
			fmt.Fprintf(&b, "<div><b class=\"warn\">%s</b><span>bigger than before</span></div>", esc(formatBytes(-d)))
		}
	}
	if t := tookWords(took); t != "" {
		fmt.Fprintf(&b, "<div><b>%s</b><span>time taken</span></div>", esc(t))
	}
	b.WriteString(`</div>`)
	for _, r := range rows {
		cls, word := "ok", "Done"
		switch r.Status {
		case stFailed:
			cls, word = "bad", "Failed"
		case stSkipped:
			cls, word = "warn", "Skipped"
		case stCanceled:
			cls, word = "warn", "Canceled"
		case stDone:
			if len(r.Warnings) > 0 {
				cls, word = "warn", "Done, with warnings"
			}
		default:
			cls, word = "warn", capitalize(r.Status)
		}
		fmt.Fprintf(&b, "<div class=\"file\"><h2>%s</h2><div class=\"path\">%s<br>→ %s</div><div class=\"facts\"><span class=\"st %s\">%s</span>",
			esc(r.Name), esc(r.Input), esc(r.Output), cls, word)
		if r.Status == stDone && r.InSize > 0 && r.OutSize > 0 {
			fmt.Fprintf(&b, "<span>%s</span>", esc(savedText(r.InSize, r.OutSize)))
		}
		if t := tookWords(r.Took); t != "" {
			fmt.Fprintf(&b, "<span>took %s</span>", esc(t))
		}
		if r.Settings != "" {
			fmt.Fprintf(&b, "<span>%s</span>", esc(r.Settings))
		}
		b.WriteString("</div>")
		if r.Status != stDone && r.Note != "" {
			fmt.Fprintf(&b, "<ul><li class=\"%s\">%s</li></ul>", cls, esc(r.Note))
		}
		if r.Check != "" && strings.Contains(r.Check, "passed") {
			fmt.Fprintf(&b, "<ul><li class=\"ok\">%s</li></ul>", esc(r.Check))
		}
		if len(r.Fallbacks) > 0 {
			b.WriteString("<ul>")
			for _, f := range r.Fallbacks {
				fmt.Fprintf(&b, "<li>Done another way: %s</li>", esc(f))
			}
			b.WriteString("</ul>")
		}
		if len(r.Warnings) > 0 {
			b.WriteString("<ul>")
			for _, w := range r.Warnings {
				fmt.Fprintf(&b, "<li class=\"warn\">⚠ %s</li>", esc(w))
			}
			b.WriteString("</ul>")
		}
		b.WriteString("</div>")
	}
	b.WriteString("</main></body></html>")
	return b.String()
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
