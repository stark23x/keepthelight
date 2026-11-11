# Keep the Light: feature backlog

The working list of feature ideas. Claude reads it before every build and builds what is under "Requested for the
next build". Shipped features move to "Done" with their version number. Nothing is built until the owner says
"build".

## Requested for the next build

- **Dolby Vision profile 5: convert instead of refusing** (2026-11-11; option 2 of two discussed). Profile 5 (mostly
  streaming downloads) has no HDR10 picture underneath: its colours are in Dolby's own format (IPTPQc2) and only look
  right after a Dolby Vision player reshapes them, so today it is refused for re-encoding.
  - Apply the Dolby Vision reshaping first (FFmpeg's libplacebo filter, on the graphics card) to get a normal
    BT.2020/PQ HDR10 picture, then encode it like any HDR10 file. The result plays everywhere and Plex can convert it
    with correct colours.
  - Also keep Dolby Vision: convert the RPU to profile 8.1 with dovi_tool (mode 3) and inject it, so Dolby Vision
    players still get Dolby Vision (an option, on by default; off = plain HDR10).
  - Check first that the bundled FFmpeg has libplacebo and that it works on both PCs (NVIDIA laptop, Intel Dell); if
    it is missing or the graphics card cannot run it, refuse with a clear reason as today.
  - Test on a real profile 5 file and compare with a Dolby Vision player (colours, brightness); note in the README
    that the HDR10 picture is a close match, not an exact one.
  - **Tried 2026-11-11 on Eddington (2025)** (profile 5.06, full range, no colour tags, 3840×2080; 20-second samples
    in Claude Dev\ktl-p5): `libplacebo=colorspace=bt2020nc:color_primaries=bt2020:color_trc=smpte2084:range=tv:format=yuv420p10le`
    (with `-init_hw_device vulkan`) gives correct colours where a plain decode is green/purple; the HDR10 result,
    tone-mapped, matches libplacebo's own Dolby Vision rendering within about 1.5/255. `dovi_tool -m 3` turns the RPU
    into 8.1 (identity reshaping, limited range), as needed for the HDR10 base. HDR10 static metadata from the RPU's
    L6 (1000 nits, MaxCLL 933, MaxFALL 171 here). The owner's FFmpeg (gyan.dev full build) has libplacebo and Vulkan;
    the README's "essentials" advice may need to change if that build lacks it.

## Ideas raised, not yet requested

None at the moment.

## Done

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
