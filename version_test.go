package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Releases follow semantic versioning, and every version gets a CHANGELOG
// entry: these checks keep VERSION and CHANGELOG.md from drifting apart.
func TestVersionAndChangelog(t *testing.T) {
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(version) {
		t.Fatalf("VERSION %q is not MAJOR.MINOR.PATCH", version)
	}
	log, err := os.ReadFile("CHANGELOG.md")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^## \[?(\d+\.\d+\.\d+)\]?`).FindStringSubmatch(string(log))
	if m == nil {
		t.Fatal("CHANGELOG.md has no '## X.Y.Z' entry")
	}
	if m[1] != version {
		t.Fatalf("newest CHANGELOG entry is %s but VERSION is %s: add a CHANGELOG entry for the release", m[1], version)
	}
	if !strings.Contains(string(indexHTML), "__VERSION__") {
		t.Fatal("index.html lost its __VERSION__ placeholder")
	}
}
