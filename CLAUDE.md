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
- **Never** delete the owner's files or change files on PlexRaid without being asked. Releases are published
  automatically by the release workflow, but only when every step passes (see Releases).
- **Model advice:** the owner asked to be told when a switch to a higher-tier model (such as Fable) would be worth
  it. Say so in one line before starting a build that is large, spans many files, or is hard to reason about (new
  video pipelines, colour or HDR handling, matching logic), or after a fix has failed twice. Do not suggest it for
  routine builds, small fixes, backlog edits or questions.

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
- **Quality aim:** the search moves the constant-quality number (12–30) for all three encoders; a file is never made
  bigger than the original; a film that cannot reach the score gets the setting within a point of its best, not the
  lowest one. The thorough check is a PC setting, off by default. Tidying up never deletes outright.
- **Keeping awake:** the window holds one Windows power request for the whole queue run (released while paused);
  on by default, with "keep the screen on" by default only on Modern Standby PCs (the owner's laptop is one).
- **Layout:** Convert / Queue / Library scan / Plex names tabs, Start bar always at the bottom (at every window width), PC
  settings in the ⚙ Settings dialog. Wide windows: the Convert tab in two columns (owner chose this over one wide
  column); narrow windows: one column. Text size zooms the whole page. Appearance: System / Dark / Light and five
  accents (Blue, Teal, Purple, Pink, Red; the owner dropped Green and Orange); every colour is a CSS variable.
- **TMDB:** English is the default language (a setting). The key lives in Windows Credential Manager. TVmaze is only a
  second opinion on episode numbers (no key; CC BY-SA, credited in About); TVDB is not used (paid / per-user PIN).
- **Plex-style names** are optional and never applied without being shown first. The Plex names tab renames
  originals (with their subtitle files, undoable); it never renames or creates folders.
- **Subtitles to SRT:** ASS via FFmpeg; PGS and VobSub read with Tesseract OCR (installed separately, never bundled).
  Your own SRT/ASS files are added as extra tracks (`extsubs.go`).
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
  - `subs.go`, `ocr.go`, `vobsub.go`, `extsubs.go` – subtitles; the PGS and VobSub readers and Tesseract; added subtitle files.
  - `meta.go` – metadata and replacing the original.
  - `tmdb.go` – TMDB lookups; `tvmaze.go` – TVmaze, the second opinion on episode numbers.
  - `scan.go` – the library scan.
  - `plexnames.go`, `renamer.go`, `episodes.go` – Plex-style names, the Plex names tab (renaming, undo, the rename log),
    and double / split episodes (renumbering a season; the target size of a double).
  - `denoise.go` – noise reduction and the 30-second test.
  - `quality.go` – the quality guide and the test's score.
  - `qualityaim.go` – "Aim for quality" and "Find the right setting": the search for the setting that reaches a VMAF
    score (short stretches, shared tries, the knee when a film's score tops out).
  - `deepcheck.go` – the optional thorough check of a finished file.
  - `report.go` – the page written after each queue run; `tidy.go` – tidying up originals (Library scan tab).
  - `gpupath.go` – the full-GPU path and the encoder check.
  - `nfo.go` – the NFO file for Plex beside a new file (from the TMDB / TVmaze lookup; written by the window after the
    finished file is in place, only with the "nfoFiles" setting).
  - `gpustuck.go` – a graphics card that stops answering (retry, then pause the queue) or crashes (a plain message).
  - `awake.go`, `power_*.go` – keeping the PC awake for a queue run (and the screen on with Modern Standby); sleep /
    shut down when finished.
  - `scale.go` – resizing (including Dolby Vision active area).
  - `reshape.go` – Dolby Vision profile 5: colours to HDR10 with libplacebo, HDR10 information from the RPU.
  - `interleave.go` – picture and sound stored together: the check of each new file, the remux, the scan's Fix.
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
   - creates a draft GitHub release (and its tag) with the program zip, the source zip and
     `SHA256SUMS-<version>.txt`;
   - **publishes** it once all three files are attached (the owner asked for this, 17 November 2026). If any step
     fails, nothing is published; a draft may be left behind. A release already published is never changed by a
     later run.
3. When it succeeds, download those files, copy them where `CLAUDE.local.md` says, and tell the owner the release is
   published. If it failed, tell the owner what failed and that the release was not published; fix it and push again
   (a rerun replaces the draft's files and then publishes).
4. Move the shipped items to "Done" in `BACKLOG.md`.

The owner sometimes edits `README.md` on GitHub: always pull before committing.
