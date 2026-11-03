# Keep the Light: feature backlog

The working list of feature ideas. Claude reads it before every build and builds what is under "Requested for the
next build". Shipped features move to "Done" with their version number. Nothing is built until the owner says
"build".

## Requested for the next build

- **Crop black bars, for the SD sizes only** (requested 2026-11-03). A **Crop black bars** tick box under
  Resolution, shown only when 576p or 480p is chosen; for every other resolution it is hidden and nothing is cropped.
  - **Default:** ticked when shown. It is part of the Video settings, so it is saved in profiles; `-crop` on the
    command line, which is accepted only with `-scale 576p` or `-scale 480p`.
  - **Finding the bars:** the picture is checked at several points (FFmpeg's cropdetect), so opening titles and
    credits with different bars do not fool it. It crops only when the bars are the same throughout, rounds inwards
    (never into the picture), and ignores tiny edges of a few pixels.
  - **Order:** the bars are cut first, and the picture is then shrunk to fit 1024×576 or 854×480, keeping its
    shape. For example a 4:3 show in a 1920×1080 frame becomes 640×480, with no bars.
  - **Works with:** deinterlacing (which comes first) and noise reduction.
  - **Dolby Vision:** files are not cropped, because their scene data would need adjusting too; the log says so.
  - **Shown in the window:** the Convert tab and the log say what was found ("black bars: 1440×1080 picture in a
    1920×1080 frame, cropped").
- **TMDB: a "Which one?" window for unclear matches** (requested 2026-11-03), so a show or film with several possible
  matches no longer needs Edit.
  - **When it opens:** automatically, once the queue's TMDB lookup has finished, when any title had several possible
    matches. It can also be opened with a **Choose…** button on any "needs a match" row.
  - **What it asks:** one question per title, not per file ("Which *The Office*? 22 files"). Several titles step
    through in turn ("1 of 2").
  - **What it shows:**
    - The top 3 results, each with a poster, name, year and one line of description.
    - An editable title and a **Year** box, with **Search again**.
    - **Show more**, for the rest of the results.
    - **Skip**: a skipped title is not asked about again in the same session (its rows keep **Choose…**).
  - **On choosing:** every waiting file with that title is filled in from the chosen show or film (each episode with
    its own season and episode), and the choice is remembered, as in 1.25.0.
  - **The queue keeps going:** clear matches are filled in already, and the queue can run while the window is open.

## Ideas raised, not yet requested

- Plex-style output names after a TMDB lookup, such as `Title (Year) {tmdb-id}`. The owner chose no renaming
  (2026-10-27).
- A TMDB logo in the About box. This is a TMDB attribution requirement, and needs the logo file from the owner.
- "Fill in from names": set a missing language and the commentary, SDH and forced flags from words in track
  names. The owner chose naming only (2026-10-28).

## Done

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
