# Keep the Light: notes for Claude Code

Keep the Light ("KTL" in conversation; the program's name is always written out as "Keep the Light") is a Windows
transcoder written in Go. It converts HDR10, HLG, HDR10+ and Dolby Vision video to HEVC while keeping the HDR
information intact. It has its own window (WebView2) and a command-line version, and is mostly used to prepare
files for a Plex server. Personal setup details (PCs, drives, where releases are copied) are in `CLAUDE.local.md`,
which is not in the repository.

## How to work with the owner

- **"Question only", "answer only", "don't build", "no changes":** answer in words. Do not edit files, build or
  commit.
- **Build only when told to** ("build it", "do it", "make a build"). Otherwise discuss, and add requests to the
  backlog.
- **The backlog is `BACKLOG.md`.**
  - When the owner suggests a feature, add it under "Requested for the next build" and say so.
  - Before every build, read it and include everything requested (ask first if anything is unclear or would be
    expensive to get wrong).
  - After a release, move what shipped to "Done" with its version number.
- **Questions:** ask before building only when a choice changes what gets built. Otherwise make the sensible
  choice and say which one in one line.
- **Writing:** UI text, README and CHANGELOG are in plain, short English for a non-programmer. Say what something
  does and why it matters; no jargon without a gloss. Follow the existing wording style (British spellings such
  as "licence" and "recognisable" are used throughout).
- **Never** publish a GitHub release, delete the owner's files, or change files on PlexRaid without being asked.

## Decisions to keep (do not undo without asking)

- **File names:** converted files end in `_KTL` by default (was `_hdr`). The ending is the owner's choice and is
  remembered. A profile can name files after itself; typing over its ending is a one-off override (↺ restores it).
- **Profiles:**
  - What they hold: the Video settings, the format (MKV/MP4), the Audio/Subtitles rules, track naming and the
    Advanced HDR overrides. Export and import are supported.
  - What they never hold: files, folders, PC settings (⚙ Settings), one file's own tracks and metadata, or
    "Replace the original".
  - No profile is applied at start-up, and there are no ready-made profiles.
- **Replace the original** is optional and only for "Metadata only". The original goes to the Recycle Bin, or is
  renamed "(original)" on drives without one, and only after the new file is checked.
- **Space saved:** shown per file and per run, plus one running total (no history).
- **Layout:** Convert / Queue / Library scan tabs, Start bar always at the bottom (at every window width), PC
  settings in the ⚙ Settings dialog.
- **TMDB:** English is the default language (a setting). The owner chose no Plex-style renaming. The key lives in
  Windows Credential Manager.
- **Subtitles to SRT:** ASS via FFmpeg; PGS read with Tesseract OCR (installed separately, never bundled).
- **The GPU path is automatic,** with fallback. The Dell (Intel QuickSync) must keep every option the laptop
  (NVIDIA NVENC) has.

## Code map

- `main.go` – command line: flags, `transcode()` (the whole job, step by step).
- `build.go` – the `Config`/`Plan` types and the video arguments.
- `gui.go` – the window's back end: `settings` (what the page sends), `toConfig()` (settings to `Config`),
  `cliArgs()` (`Config` to command-line options), and the `call()` request dispatcher.
- `queue.go` – the queue and its runner; checking items; `queueApply`, `queueCopyTracks` and edit-in-place.
- `index.html` – the whole page (HTML, CSS, JavaScript in one file); `about.html` is the About box text.
- Feature files:
  - `tracks.go`, `trackrules.go` – track choice, rules and naming.
  - `subs.go`, `ocr.go` – subtitles; the PGS reader and Tesseract.
  - `meta.go` – metadata and replacing the original.
  - `tmdb.go` – TMDB lookups.
  - `scan.go` – the library scan.
  - `denoise.go` – noise reduction and the 30-second test.
  - `quality.go` – the quality guide and the test's score.
  - `gpupath.go` – the full-GPU path and the encoder check.
  - `scale.go` – resizing (including Dolby Vision active area).
  - `worklocal.go`, `fsretry.go` – working locally, and retries when another program has a file open.
  - `profilecli.go` – `-profile`.
  - `secret_*.go` – Credential Manager.
- Platform code sits in `*_windows.go` / `*_other.go` pairs.

The window runs `keepthelight_cli`'s code as a child process per job: the page sends `settings`, `toConfig()` makes
a `Config`, and `cliArgs()` turns it into the job's command line. **A new option needs all four:** the
`settings` field, `toConfig`, `cliArgs`, and the flag in `parseArgs`. Window-side it also needs `settings()`,
`applyFields`, `PROFILE_KEYS` (if it belongs in profiles), the `trackRules` pref (if it is a rule) and a
summary.

## Build, test, run

- `go test ./...` must pass.
  - Tests that need ffmpeg, ffprobe or tesseract skip themselves when the tools are missing.
  - Queue tests use stand-in tools (`fakeTools`) on non-Windows only.
  - `version_test.go` requires a `CHANGELOG.md` entry for the version in `VERSION`.
- Build both programs with `build.cmd`. After changing `VERSION` or the icon, run `go generate` first: it
  rewrites `rsrc_windows_*.syso`, which carry the version and icon.
- `keepthelight_cli -server` opens the window in a browser instead of WebView2, handy for checking the page.
  `KTL_DEBUG=1` enables DevTools in the real window.
- Check a page change by running the window and looking at it, not only by reading the code.

## Releases

1. Bump `VERSION` (semantic versioning: patch = fixes, minor = features), add the `CHANGELOG.md` entry, write
   `.github/release-notes/<version>.md` (short and plain), update `README.md` for anything user-visible, and run
   `go generate`.
2. Commit and push to `main`. `.github/workflows/release.yml` then:
   - runs the tests and `govulncheck`;
   - builds with the current Go;
   - adds dovi_tool and hdr10plus_tool (checksum-verified);
   - creates a **draft** GitHub release (and its tag) with the program zip, the source zip and
     `SHA256SUMS-<version>.txt`.
3. When it succeeds, download those files, copy them where `CLAUDE.local.md` says, and tell the owner the draft is
   ready to publish. The owner publishes releases themselves.
4. Move the shipped items to "Done" in `BACKLOG.md`.

The owner sometimes edits `README.md` on GitHub: always pull before committing.
