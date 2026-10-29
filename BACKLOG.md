# Keep the Light: feature backlog

The working list of feature ideas. Claude reads it before every build and builds what is under "Requested for the
next build". Shipped features move to "Done" with their version number. Nothing is built until the owner says
"build".

## Requested for the next build

- **Batch and per-file progress on the Queue tab** (requested 2026-10-29).
  - **The whole batch:** a progress bar at the top of the Queue tab, with "File 2 of 5", the percentage of the
    batch done, and the estimated time left for everything.
    - The percentage is weighted by each file's length, so a 2-hour film counts more than a 20-minute episode.
    - Skipped files, and files whose output already exists, are left out.
  - **Each file:**
    - The running file has its own progress bar, percentage and time left.
    - Each waiting file shows an estimate such as "about 40 min". It uses the encode speed measured for similar
      files in the run (same encoder and resolution), or the current file's speed scaled by picture size.
    - Before any speed is known, waiting files say "estimate after the first minutes".
    - Finished files show how long they took.
  - **Background:** `queue.go` already measures speed per kind of job (`speedFor`) and estimates the whole queue's
    remaining time (`remainingLocked`, shown in the window title and the Start bar). The Queue tab only shows the
    running file's percentage, so this mostly shows what is already calculated.

## Ideas raised, not yet requested

- Plex-style output names after a TMDB lookup, such as `Title (Year) {tmdb-id}`. The owner chose no renaming
  (2026-10-27).
- A TMDB logo in the About box. This is a TMDB attribution requirement, and needs the logo file from the owner.
- "Fill in from names": set a missing language and the commentary, SDH and forced flags from words in track
  names. The owner chose naming only (2026-10-28).

## Done

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
