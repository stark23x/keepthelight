// mkres writes the Windows resources (icon, manifest, version information)
// into rsrc_windows_*.syso, which go build links into both programs.
//
// Run it from the project folder after changing VERSION or the icon:
//
//	go generate
//
// (that runs "go -C tools/mkres run . ../..").
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"
)

const (
	appName     = "Keep the Light"
	description = "Keep the Light - HDR10 / HLG / HDR10+ / Dolby Vision transcoder"
)

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	if err := run(root); err != nil {
		fmt.Fprintln(os.Stderr, "mkres:", err)
		os.Exit(1)
	}
}

func run(root string) error {
	raw, err := os.ReadFile(filepath.Join(root, "VERSION"))
	if err != nil {
		return err
	}
	ver := strings.TrimSpace(string(raw))
	m := regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)$`).FindStringSubmatch(ver)
	if m == nil {
		return fmt.Errorf("VERSION must be MAJOR.MINOR.PATCH, got %q", ver)
	}
	var v4 [4]uint16
	for i := 0; i < 3; i++ {
		n, _ := strconv.Atoi(m[i+1])
		v4[i] = uint16(n)
	}

	rs := &winres.ResourceSet{}

	f, err := os.Open(filepath.Join(root, "res", "keepthelight.ico"))
	if err != nil {
		return err
	}
	defer f.Close()
	icon, err := winres.LoadICO(f)
	if err != nil {
		return err
	}
	// ID 1: Explorer shows the first icon; the window loads it by this ID (appIconID).
	if err := rs.SetIcon(winres.ID(1), icon); err != nil {
		return err
	}

	rs.SetManifest(winres.AppManifest{
		Identity:            winres.AssemblyIdentity{Name: "KeepTheLight", Version: v4},
		Description:         description,
		Compatibility:       winres.Win10AndAbove,
		ExecutionLevel:      winres.AsInvoker,
		DPIAwareness:        winres.DPIPerMonitorV2,
		UseCommonControlsV6: true,
	})

	vi := version.Info{}
	vi.SetFileVersion(ver)
	vi.SetProductVersion(ver)
	for k, v := range map[string]string{
		version.ProductName:     appName,
		version.FileDescription: appName,
		version.InternalName:    "keepthelight",
		version.Comments:        description,
		version.LegalCopyright:  fmt.Sprintf("© %d", time.Now().Year()),
		version.ProductVersion:  ver,
		version.FileVersion:     ver,
	} {
		if err := vi.Set(version.LangDefault, k, v); err != nil {
			return err
		}
	}
	rs.SetVersionInfo(vi)

	for _, arch := range []winres.Arch{winres.ArchAMD64, winres.ArchARM64} {
		out := filepath.Join(root, "rsrc_windows_"+string(arch)+".syso")
		w, err := os.Create(out)
		if err != nil {
			return err
		}
		if err := rs.WriteObject(w, arch); err != nil {
			w.Close()
			return err
		}
		if err := w.Close(); err != nil {
			return err
		}
		fmt.Println("wrote", out, "for version", ver)
	}
	return nil
}
