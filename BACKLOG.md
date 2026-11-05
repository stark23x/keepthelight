# Keep the Light: feature backlog

The working list of feature ideas. Claude reads it before every build and builds what is under "Requested for the
next build". Shipped features move to "Done" with their version number. Nothing is built until the owner says
"build".

## Requested for the next build

- **Mark files that are already converted** (requested 2026-11-05), so a converted file is not converted again by
  accident (each time costs quality and saves little).
  - **The tag:** every new file gets a small tag such as "Keep the Light 1.29.0 · x265 CRF 18 · 1080p".
  - **Library scan:** shows tagged files as "already converted" and leaves them out of the savings estimate.
  - **Queue and Convert tab:** adding a tagged file gives a warning in the Checks (it can still be converted).
  - **Visible:** the tag shows in the file details on the Convert tab.
- **Tell Plex to refresh after a batch** (requested 2026-11-05).
  - **Setup in ⚙ Settings:** the Plex server address and token (the token kept in Windows Credential Manager, like the
    TMDB key), with a **Check** button.
  - **When:** after a queue finishes (and after a single file), Keep the Light asks Plex to rescan just the library
    folder(s) that contain the new files, so they appear straight away. Off unless set up.
  - **Failures:** a failed refresh is a note in the log, never an error for the conversion.
- **Run unattended: a start time and a watch folder** (requested 2026-11-05).
  - **Start at a set time:** on the Queue tab, "Start the queue at [01:00]". There is also an optional "Pause at
    [07:00] and carry on at [01:00]" for long batches, using Pause / Resume. The times are remembered.
  - **Watch folder (optional):** in ⚙ Settings, choose a folder and a profile. Any video placed there is added to the
    queue with that profile once it has finished copying (its size has stopped changing). It is picked up while Keep
    the Light is open, and is started at once or at the set start time.
  - **Visible:** the Queue tab says what is planned ("Starts at 01:00", "Watching D:\To convert").
- **Keep failure logs automatically** (requested 2026-11-05).
  - **What is kept:** the full log of every failed or cancelled-with-error job, saved to a Logs folder in Keep the
    Light's settings folder, named by date and file ("2026-11-05 14-20 Heat (1995).txt"). The 50 newest are kept.
  - **Where to find them:** **Open logs folder** in ⚙ Settings, and a link on the failed row in the queue.

## Ideas raised, not yet requested

None at the moment.

## Done

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
