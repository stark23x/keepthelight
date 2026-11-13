# Keep the Light: feature backlog

The working list of feature ideas. Claude reads it before every build and builds what is under "Requested for the
next build". Shipped features move to "Done" with their version number. Nothing is built until the owner says
"build".

## Requested for the next build

TVmaze, made useful (2026-11-13). In 1.38.0 it is only asked after TMDB has matched the show, only on the Plex names
tab, only for seasons with 3+ files, and it shows nothing when the two agree, so the owner saw it do nothing. The
case that prompted this: **Red vs. Blue: Restoration** (released in 2024 as a film, also as episodes). TMDB has
Restoration as a film, and on the show (TMDB 39373) only a season 0 special "Red vs. Blue: Restoration - The Final
Season (Prologue)"; TVmaze's Red vs. Blue (show 6090) ends at season 19, so Restoration is probably among its
specials (season 0), which 1.38.0 leaves out.

- **TVmaze as a fallback when TMDB finds nothing** (or nothing that fits: no such season or episode), on the Convert,
  Queue and Plex names tabs. TMDB is still tried first. From TVmaze: the show's name and year, the episode's title,
  date and description, and its poster (TVmaze's show/season image). The note says where it came from ("TVmaze: …").
- **A visible result** of the second opinion: "TVmaze: agrees with TMDB", "TVmaze: numbers it differently" (the
  existing note), "TVmaze: show not found", "TVmaze: not reachable", per season on the Plex names tab and in the
  file's lookup note, so it is clear it ran. Also for seasons with fewer than 3 files.
- **Search TVmaze in the Edit / "Which one?" search,** beside TMDB's results (marked as TVmaze), for when the
  automatic match picks the wrong show or none.
- **Specials (season 0):** include TVmaze's specials when the file says S00Exx, and let a show's specials be
  searched by title, so episodes released outside the normal seasons (like Restoration) can be found. Check against
  the real Red vs. Blue: Restoration files (ask the owner how they are named) before shipping.

## Ideas raised, not yet requested

None at the moment.

## Done

- 1.38.0: TVmaze as a second opinion for TV episode numbers (compare, choose per show and season, titles from TVmaze, remembered; setting to turn off; credited in About).
- 1.37.1: red "Clear the queue" button; MP4Box `:dvp=` when GPAC knows it (else `:dv-profile=`); dovi_tool's "Reordering metadata" lines hidden.
- 1.37.0: Library scan results kept until the next scan (saved, shown with their date, gone/changed files marked, Clear results).
- 1.36.1: faster Dolby Vision profile 5 conversion: Vulkan device matched to the encoder; GPU decoding (on the graphics card, or copied back) used only after a 48-frame check against processor decoding on the file itself, with fallback during the job.
- 1.36.0: Dolby Vision profile 5 converted (colours to HDR10 with libplacebo on the graphics card; Dolby Vision 8.1 kept, or dropped); HDR10 information read from the Dolby Vision data; stale Dolby Vision labels removed when Dolby Vision is dropped.
- 1.35.1: Dolby Vision picture and sound stored together (no more black picture on streaming players); every new file checked and remuxed if needed; Library scan finds and fixes (remux) older files, plus `-fix-interleave`; a Dolby Vision label without Dolby Vision data no longer fails the job; no false "Interlaced video" warning after smooth-motion deinterlacing.
- 1.35.0: double episodes (read S05E09-E10; mark or spot doubles/splits on the Plex names tab; season renumbered without overwriting, undoable); target file size follows double episodes.
- 1.34.0: your own subtitle files (added as tracks, found by name, encoding fixed, timing offset); VobSub to SRT; the file's bitrate always shown with a target comparison.
- 1.33.2: pop-ups always kept inside the window; right-click menu placed correctly at any text size.
- 1.33.1: fixed the full-GPU path failing on full-range files (skips them, checks with the job's colour options, retries the normal way on failure).
- 1.33.0: uses the whole window (two-column Convert tab when wide, full-width lists), Text size setting with Ctrl +/−/0, window remembers its size and place.
- 1.32.0: Plex names tab (a file renamer for the queue and added folders: Rename now with subtitle files, Undo last rename, Rename log, Use for the converted files; folders never renamed).
- 1.31.0: track names in English instead of the language's own script, more language names (yue, cmn and others); track names follow through a series in the queue; new README "New since version 1.27" section.
- 1.30.0: Plex-style names in the dotted format with resolution, HEVC and HDR kind (Plex matching to be confirmed by the owner on real files).
- 1.29.0: already-converted tag; Plex refresh; schedule and watch folder; failure logs.
- 1.28.0: optional Plex-style names with Review names; fill in track details from names; TMDB logo; no work folders kept when nothing was finished; a second try for unreadable Dolby Vision data.
- 1.27.0: TMDB "Which one?" window; crop black bars (576p and 480p only).
- 1.26.0: Pause / Resume; Stop after this file; move a queued file to the top or bottom.
- 1.25.1: fixed the 30-second test of MP4 files with text subtitles.
- 1.25.0: TMDB: match one file by hand, the rest of that title follows (remembered; Settings can forget).
- 1.24.1: the queue hint no longer flickers (UI-7); Start no longer also adds (UI-4); UI-12 tighter spacing. To undo
  UI-4 or UI-7, revert both commits with that number.
- 1.24.0: deinterlace (automatic, from the picture); queue estimates for short files; Browse remembers the last
  folder; layout and UI changes UI-1 to UI-11, each its own commit so any one can be reverted alone (`git revert`
  the commit titled "UI-n: …", then a patch release).
- 1.23.0: Plex check when files are added (settings-aware, a summary on the Queue tab, and a check of each finished
  file); 576p and 480p resolutions.
- 1.22.0: empty subtitle/audio tracks left out automatically; Skip Plex extras in the library scan; right-click
  Cut/Copy/Paste/Select all; queued file logs scroll again, with a Copy log button.
- 1.21.0: batch and per-file progress on the Queue tab (time left for the batch and the running file, estimates for
  waiting files, time taken for finished ones).
- 1.20.1: releases built on GitHub with the current Go; `govulncheck` passes.
- 1.20.0:
  - **Queue:** Clear the queue; back to the Queue tab after Edit; "Same tracks for others".
  - **Track rules:** one audio track per language; subtitles kept or dropped by name.
  - **Track naming:** untitled tracks named from what they are.
  - **TMDB:** lookup for the queue and the library scan; a language setting; the key in Credential Manager.
  - **Profiles:** `-profile` on the command line; MKV/MP4 saved in profiles.
  - **Other:** Tesseract language hint; SHA-256 checksums; govulncheck on GitHub.
- 1.19.x: ASS and PGS subtitles to SRT (PGS by Tesseract OCR); clearer Tesseract warnings; retry when another
  program has a file open.
- 1.18.x: new layout (tabs, Start bar always visible, ⚙ Settings).
- 1.17.0: metadata lookup on TMDB.
- 1.16.0: library scan, compatible audio track rule, work locally.
- 1.15.0: full-GPU path for NVENC and QuickSync, space saved.
- Earlier versions are in `CHANGELOG.md`.
