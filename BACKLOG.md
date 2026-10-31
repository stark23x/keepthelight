# Keep the Light: feature backlog

The working list of feature ideas. Claude reads it before every build and builds what is under "Requested for the
next build". Shipped features move to "Done" with their version number. Nothing is built until the owner says
"build".

## Requested for the next build

- **Two smaller resolutions: 576p and 480p** (requested 2026-10-31), added to the Resolution list under Video and to
  `-scale`.
  - **Sizes:** the picture fits within 1024×576 (576p) or 854×480 (480p), keeping its shape. A 2.40:1 film becomes
    854×356 at 480p. Sizes are rounded to even numbers, which the encoders need.
  - **HDR:** HDR10, HLG, HDR10+ and Dolby Vision are kept as for the other sizes, including the Dolby Vision
    active area scaled per scene.
  - **Checks:** the quality guide, size plan and queue list need to know the new sizes. Check that NVENC and
    QuickSync accept HEVC Main10 at these small sizes, and use the CPU (x265) with a note if not.

## Ideas raised, not yet requested

- Plex-style output names after a TMDB lookup, such as `Title (Year) {tmdb-id}`. The owner chose no renaming
  (2026-10-27).
- A TMDB logo in the About box. This is a TMDB attribution requirement, and needs the logo file from the owner.
- "Fill in from names": set a missing language and the commentary, SDH and forced flags from words in track
  names. The owner chose naming only (2026-10-28).

## Done

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
