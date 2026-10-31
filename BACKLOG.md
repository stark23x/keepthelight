# Keep the Light: feature backlog

The working list of feature ideas. Claude reads it before every build and builds what is under "Requested for the
next build". Shipped features move to "Done" with their version number. Nothing is built until the owner says
"build".

## Requested for the next build

- **Skip Plex extras in the library scan** (requested 2026-10-30).
  - **The option:** a "Skip Plex extras" check box next to "Include subfolders", on by default and remembered.
  - **What counts as an extra:** the same rules Plex uses
    ([movies](https://support.plex.tv/articles/local-files-for-trailers-and-extras/),
    [TV](https://support.plex.tv/articles/local-files-for-tv-show-trailers-and-extras/)):
    - files inside a folder named Behind The Scenes, Deleted Scenes, Featurettes, Interviews, Scenes, Shorts,
      Trailers or Other;
    - files whose name ends in `-behindthescenes`, `-deleted`, `-featurette`, `-interview`, `-scene`, `-short`,
      `-trailer` or `-other` (the hyphen directly before the word).
  - **Also skipped:** "Plex Versions" folders (Plex's own optimised copies).
  - **Matching:** capitals don't matter. Only those exact folder names and endings are skipped, so titles like
    "The Other" or "Trailer Park Boys" stay in.
  - **Reporting:** the summary line says how many extras were skipped, for example "1,240 files · 312 extras
    skipped".

- **Right-click menu for copy and paste** (requested 2026-10-30).
  - **What it does:** right-clicking a text box (paths, name ending, metadata fields, TMDB search and so on) shows Cut, Copy, Paste and Select all. Right-clicking the log or other text shows Copy and Select all.
  - **How:** the window's built-in browser menu is switched off today (it is only on with `KTL_DEBUG=1`), because it also offers Back, Reload and Inspect. So this is KTL's own small menu, styled like the window.
  - **Paste** goes through KTL (the Windows clipboard read by the program), because the page alone can't always read the clipboard without asking.
- **Fix: a queued file's Log can't be scrolled** (reported 2026-10-30). The list is redrawn every half-second, and each redraw moves every row, which resets an open log to the top. Rows should only move when the order changes. Also make it easy to copy a whole log (a "Copy log" button).

- **Skip empty subtitle tracks automatically** (requested 2026-10-30). John Wick (2014) failed after 9 minutes
  with "hevc_nvenc … Terminating thread with error: Invalid argument". The cause was an empty subtitle track: with
  all subtitles dropped, the test ran fine.
  - **Detect them** when reading a file. MKV records each track's subtitle count and size (`NUMBER_OF_FRAMES` /
    `NUMBER_OF_BYTES` in the track statistics). Where those are missing, count the track's packets with ffprobe.
  - **In the window:** an empty track is marked "empty" in the Subtitles list and left out by default (it can be
    kept on purpose).
  - **Everywhere else:** left out for queued files and in the library scan too, with a note in the log ("subtitle
    track 3 is empty and was left out").
  - **Check before building:** whether empty audio or attachment tracks cause the same failure, and handle those
    the same way.

## Ideas raised, not yet requested

- Plex-style output names after a TMDB lookup, such as `Title (Year) {tmdb-id}`. The owner chose no renaming
  (2026-10-27).
- A TMDB logo in the About box. This is a TMDB attribution requirement, and needs the logo file from the owner.
- "Fill in from names": set a missing language and the commentary, SDH and forced flags from words in track
  names. The owner chose naming only (2026-10-28).

## Done

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
