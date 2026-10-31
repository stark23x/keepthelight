# Keep the Light: feature backlog

The working list of feature ideas. Claude reads it before every build and builds what is under "Requested for the
next build". Shipped features move to "Done" with their version number. Nothing is built until the owner says
"build".

## Requested for the next build

- **Queue time estimates for short files** (requested 2026-10-31). Today a file's speed is only trusted after 30
  seconds of encoding, so files that finish faster never give one, and waiting files stay on "estimate after the
  first minutes".
  - **Fix:** once a file finishes, use how long it really took (start to finish, including the steps around the
    encode) for its length. Scale that for files of other sizes, as now.
  - **Better with more files:** each finished file updates the guess (an average of the recent ones), so the
    estimate settles after the first one or two.
  - **Long files:** they keep the live speed measured during the encode, as now.
- **Browse remembers the last folder** (requested 2026-10-31). Today the file picker opens where the current input
  file is; with an empty box (a new job, or after restarting) it opens wherever Windows decides, and the folder
  pickers (output folder, library scan) always start from the top.
  - Each kind of picker remembers its own last folder, kept after restarting: videos (Browse, Add files, Add
    folder), output folder, library scan folder, cover images, and profile import/export.
  - A path already typed in the box still wins.
  - If the remembered folder is gone (an unplugged drive, PlexRaid offline), the picker falls back to the normal
    start.
  - The folder pickers start in the remembered folder too, which needs the newer Windows folder dialog.
- **Deinterlace** (requested 2026-10-31), for old TV material such as The Carol Burnett Show. That file is labelled
  progressive but about 99.5% of its frames show combing, bottom field first.
  - **Setting under Video:** Auto (the default), Smooth (60 fps), Standard (30 fps) or Off.
  - **Detection:** Auto looks at the picture itself (FFmpeg's idet on a sample), not the file's label, which can be
    wrong. The field order is worked out the same way, because the wrong order makes motion jitter.
  - **Method:** bwdif, which works in 10-bit, so it is safe for HDR. Smooth keeps every half-picture as its own
    frame, giving the live-TV look of videotaped shows.
  - **Where it shows:** the Convert tab and queue rows say when a file is interlaced; the log says what was done.
  - **Scan and check:** the Plex check and the Library scan flag interlaced files.
  - **Test file:** `OneDrive\Claude Dev\The Carol Burnett Show.S11E01 - Jim Nabors.mkv` (combing at 2:00, Tim
    Conway's hand), with the clips in `Claude Dev\Carol Burnett test`.

- **Layout and UI changes UI-1 to UI-11** (requested 2026-10-31). The numbers are the owner's: refer to them by
  number.
  - **How to build them so each can be undone on its own:**
    - Each item is its own commit, titled with its number ("UI-4: …"), with no other change mixed in.
    - Items do not depend on each other. Where two touch the same code, the later one is written so the earlier can
      be reverted without breaking it.
    - Before release, check that reverting each one alone still builds and passes the tests.
    - The CHANGELOG and release notes list them by number.
    - **Undoing one:** `git revert` its commit, then make a patch release.
  - **UI-1 One-click fixes:** a fixable warning (especially the Plex check) has a **Fix** button that changes the
    setting and turns the warning green.
  - **UI-2 Calmer notes:** the notes under the file become one compact **Checks** panel: a line per item with ✓ / ⚠
    / ✕, problems first, "all good" items folded into one line. Plain wording for the mastering-display warning,
    with no command-line option in the window.
  - **UI-3 Smaller tools banner:** the GPAC/MP4Box notice becomes a slim one-line bar, or a ⚠ on ⚙ Settings, and is
    shown in full only when a Dolby Vision file is loaded.
  - **UI-4 Clear Start button:** the main button says exactly what happens ("Convert this file", "Add to queue and
    start", "Start queue (3 files)"). "When finished" moves to the Queue tab or next to the queue button.
  - **UI-5 Plain file details:** "HEVC 10-bit" instead of "hevc yuv420p10le". The file name is shown large, with the
    folder smaller below it.
  - **UI-6 Fewer hints on screen:** keep the hints that prevent mistakes; move the rest into a "?" next to the
    label.
  - **UI-7 Helpful empty screens:** Convert with no file shows a large "Drop a video here or Browse" area. An empty
    queue explains how to add files.
  - **UI-8 Tidier queue rows:** two lines per row (name + status; a short summary with chips such as "⚠ 2 Plex",
    "SRT", "480p"). Clicking a row opens its full details below it.
  - **UI-9 Messages that don't interrupt:** results such as "3 files now use these tracks" appear as a small message
    that fades by itself. Pop-ups are kept for real questions.
  - **UI-10 Language inline:** "Unknown language" tracks get a language picker in their row.
  - **UI-11 Keyboard and contrast:** a visible outline on whatever has keyboard focus, and slightly brighter grey
    text.

## Ideas raised, not yet requested

- Plex-style output names after a TMDB lookup, such as `Title (Year) {tmdb-id}`. The owner chose no renaming
  (2026-10-27).
- A TMDB logo in the About box. This is a TMDB attribution requirement, and needs the logo file from the owner.
- "Fill in from names": set a missing language and the commentary, SDH and forced flags from words in track
  names. The owner chose naming only (2026-10-28).

## Done

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
