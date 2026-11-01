# Keep the Light: feature backlog

The working list of feature ideas. Claude reads it before every build and builds what is under "Requested for the
next build". Shipped features move to "Done" with their version number. Nothing is built until the owner says
"build".

## Requested for the next build

- **Fix: the queue hint flickers on an empty queue** (reported 2026-11-01). On the Queue tab with nothing queued, the
  line "Add several files…" flashes in and out. Cause: the queue list shows it, and UI-7's empty box hides it every
  0.4 s. Fix inside UI-7, so it stays undoable on its own: the hint is simply not shown while the empty box is.
- **Fix: Start must not also add** (reported 2026-11-01). UI-4's "Add to queue and start (N files)" mixed two
  actions. With files waiting, the main button is always "Start queue (N files)", and **Add to queue** stays its own
  button. The UI-4 labels "Convert this file" and "Start queue (N files)" stay. Change made inside UI-4.
- **UI-12: tighter spacing** (requested 2026-11-01), so more fits on screen.
  - Cut the empty space at the edges of each section by about 35–50%: card padding 16 → 10 px, the gaps between
    sections 14 → 8 px, and smaller gaps in the source box, queue rows, track rows and the bottom bar.
  - Font sizes stay the same.
  - It is its own commit, so it can be undone like the other UI changes.

## Ideas raised, not yet requested

- Plex-style output names after a TMDB lookup, such as `Title (Year) {tmdb-id}`. The owner chose no renaming
  (2026-10-27).
- A TMDB logo in the About box. This is a TMDB attribution requirement, and needs the logo file from the owner.
- "Fill in from names": set a missing language and the commentary, SDH and forced flags from words in track
  names. The owner chose naming only (2026-10-28).

## Done

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
