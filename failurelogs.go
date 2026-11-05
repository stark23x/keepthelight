package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Failure logs: the full log of every failed job is saved automatically, so a
// problem can be looked into later, even after the window is closed. The 50
// newest are kept.

const keepLogs = 50

func logsDir() string { return filepath.Join(appDataDir(), "Logs") }

// saveFailureLog writes a failed job's log and returns where ("" if it could not).
func saveFailureLog(name, input, output, cmdLine string, lines []string, when time.Time) string {
	dir := logsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	base := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	base = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`\/:*?"<>|`, r) || r < 32 {
			return '_'
		}
		return r
	}, base)
	if len(base) > 80 {
		base = base[:80]
	}
	p := filepath.Join(dir, when.Format("2006-01-02 15-04-05")+" "+base+".txt")
	var b strings.Builder
	fmt.Fprintf(&b, "Keep the Light %s, %s\r\nInput:  %s\r\nOutput: %s\r\n", version, when.Format("2006-01-02 15:04:05"), input, output)
	if cmdLine != "" {
		fmt.Fprintf(&b, "Options: %s\r\n", cmdLine)
	}
	b.WriteString("\r\n")
	for _, l := range lines {
		b.WriteString(l + "\r\n")
	}
	if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
		return ""
	}
	pruneLogs(dir, keepLogs)
	return p
}

// pruneLogs keeps the newest n logs.
func pruneLogs(dir string, n int) {
	m, _ := filepath.Glob(filepath.Join(dir, "*.txt"))
	if len(m) <= n {
		return
	}
	sort.Strings(m) // the names start with the date and time
	for _, p := range m[:len(m)-n] {
		_ = os.Remove(p)
	}
}
