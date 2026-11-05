# Keep the Light: feature backlog

The working list of feature ideas. Claude reads it before every build and builds what is under "Requested for the
next build". Shipped features move to "Done" with their version number. Nothing is built until the owner says
"build".

## Requested for the next build

- **Fix: don't keep work folders when nothing was finished** (reported 2026-11-04). A Dolby Vision job that failed at
  step 1 (extracting the RPU) still kept "Intermediate files" and the local working copy (22.6 GB on D:). Keep them
  only when a step has actually finished something worth reusing; otherwise delete them and say so.
- **Fix: a second try when extracting the Dolby Vision RPU fails** (reported 2026-11-04). A DV 8.1 film failed with
  FFmpeg "non monotonically increasing dts … 22 >= 22" and dovi_tool "Invalid PPS index": a glitch near the start of
  that file.
  - **The retry:** extract again a more careful way: write the video to a temp file with rebuilt timestamps
    (`-fflags +genpts`) and start at the first clean keyframe, then run dovi_tool on that file.
  - **If that also fails:** stop with a plain message ("this file's video has a glitch near the start, so its Dolby
    Vision cannot be read; set Dolby Vision to Drop (plain HDR10) to convert it, or re-mux it with MKVToolNix
    first").
  - **To test:** build and test it without the owner's sample. Make a Dolby Vision test file with a duplicated
    timestamp near the start, and check that the retry and the plain message work. The owner may send a sample later.

- **Plex-style file names, optional, with a preview** (requested 2026-11-05; the owner had earlier chosen no
  renaming, and now wants it as an option).
  - **Off by default:** a setting "Name converted files the Plex way after a TMDB match".
  - **The names:** films `Title (Year) {tmdb-ID}.mkv`; episodes `Show (Year) - S01E02 - Episode title.mkv`. Characters
    Windows does not allow are left out. The "_KTL" ending is left off, because extra words can confuse Plex's
    matching.
  - **Only the new file:** the original keeps its name, and no folders are created or moved.
  - **Preview first:** nothing is renamed until it has been shown. Each queue row shows its proposed name
    ("→ The Office (2005) - S01E02 - Diversity Day.mkv"), and a **Review names** window lists every change (old →
    new) with a tick box each and **Apply**. The Convert tab shows the proposed name in the Output box, still
    editable.
  - **Clashes:** if two files would get the same name, or the name already exists, it says so and that file keeps
    its normal name.
- **TMDB logo in the About box** (requested 2026-11-05). The owner's SVG is in `assets/tmdb-logo.svg`. Show it small
  (about 80 px wide) next to the TMDB credit line in About, and also in ⚙ Settings → TMDB. Embedded in the program,
  not a separate file.
- **Fill in track details from their names** (requested 2026-11-05).
  - **What it reads:** when a track's language or flags are missing, they are filled in from words in its name:
    a language ("English", "Eng", "Français", "Japanese"…), "Commentary" → commentary flag, "SDH" or "hearing
    impaired" → SDH flag, "Forced" → forced flag.
  - **Only what is missing:** a language or flag the file already has is never changed.
  - **A rule:** under track naming, on by default, saved in profiles.
  - **Visible and editable:** the filled-in values show in Metadata, where they can be changed, and the log lists
    them.

## Ideas raised, not yet requested

None at the moment.

## Done

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
