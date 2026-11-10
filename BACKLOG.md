# Keep the Light: feature backlog

The working list of feature ideas. Claude reads it before every build and builds what is under "Requested for the
next build". Shipped features move to "Done" with their version number. Nothing is built until the owner says
"build".

## Requested for the next build

- **Fix: false "Interlaced video" warning after deinterlacing with smooth motion** (Laverne & Shirley S08E14,
  2026-11-10). The file was deinterlaced correctly (frames compared: the source's combing is gone), but the check of
  the finished file ran FFmpeg's interlace detector again, and on 59.94 fps "smooth motion" output (each frame made
  from one field, so neighbouring frames are offset by half a line) its frame-to-frame comparison reports
  interlacing (TFF 0 / BFF 196 / progressive 105 in one stretch). Fix: the finished-file check trusts the job when it
  deinterlaced (and otherwise judges by idet's single-frame counts, which are not fooled by the field offset).

## Ideas raised, not yet requested

None at the moment.

## Done

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
