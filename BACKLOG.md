# Keep the Light: feature backlog

The working list of feature ideas. Claude reads it before every build and builds what is under "Requested for the
next build". Shipped features move to "Done" with their version number. Nothing is built until the owner says
"build".

## Requested for the next build

- **Every kind of HDR in Plex-style names** (requested 21 November 2026). Today the name gets one tag, with Dolby
  Vision winning (`plexHDR` in `plexnames.go` for new files, the scan in `renamer.go` for files renamed as they are):
  DV, HDR10+, HLG or HDR10. The owner wants all that apply, in this order and spelling:
  - Dolby Vision = `DV`, HDR10 = `HDR10`, HDR10+ = `HDR10Plus` (no "+"), HDR = `HDR`.
  - Examples: `Movie.year.2160p.HEVC.DV.HDR10.mkv`, `TV Show.Year.S01E01.Episode Name.1080p.HEVC.DV.HDR.mkv`.
  - So Dolby Vision with an HDR10 base gets `DV.HDR10`, with HDR10+ too `DV.HDR10Plus`; Dolby Vision profile 5 (no
    HDR base) only `DV`.
  - Plain `HDR` replaces `HLG` (owner's choice, 21 November 2026: easier to grasp), so HLG gets `HDR` and Dolby
    Vision with an HLG base `DV.HDR`.
  - Both places: the new file's name (Convert tab and queue) and the Plex names tab. Files already renamed with the
    old tags (`HDR10+`, single tag) should then show up as "would be renamed" there, which is expected.

## Ideas raised, not yet requested

None at the moment.

## Done

- 1.44.2: profiles no longer lost (preference limit 8 KB → 1 MB; a failed profile save is shown); when the graphics card stops responding ("no encode device", MFX session errors) the job waits 30 s and tries once more, then the queue pauses on that file with a plain message (encode, start-up encoder check, Aim for quality); CLI icon checked (both exes carry the new icon; Windows' icon cache) and `ie4uinit.exe -show` added to the README.
- 1.44.1: new logo ("B2 · Kept light, film strip"): icon, header and About box; `res/mkicon.py` draws every size from `res/keepthelight.svg`; GitHub header at the top of the README.
- 1.44.0: "This is animation" (under Video, kept in profiles): x265 animation tune and 8 B-frames; NVENC B-frames as references, longer look ahead and temporal AQ, QuickSync more B-frames (each tried first, fullest set the card takes); offer to tick it when TMDB or TVmaze lists a title as animation; `-animation`.
- 1.43.1: Tidy up originals pairs files by Keep the Light's mark and length when the names differ (Plex-style names); episode number decides in season folders; "not sure" pairs left alone; clearer "nothing found" message.
- 1.43.0: Keep this PC awake for the whole queue run (network copies, gaps, Plex), released while paused; "and keep the screen on", on to begin with on Modern Standby PCs such as the laptop.
- 1.42.0: Aim for quality (VMAF search per film) and "Find the right setting" advice (Convert tab and Library scan); Tidy up originals (Recycle Bin, or "(original)" with undo); optional thorough check of new files (a PC setting); a report after every queue run; Plex told after Rename now and Undo.
- 1.41.0: Appearance: System / Dark / Light and accent colours (Blue, Teal, Purple, Pink, Red); every page colour a variable; title bar follows (DWM).
- 1.40.1: Dolby Vision profile 8.6 (and other profile 8 labels on an HDR10 picture) converted to 8.1 with dovi_tool mode 2; a clear message for labels that cannot be handled.
- 1.40.0: Library scan "Sort by"; added subtitle files cut to a 30-second test's stretch; Dolby Vision box kept in MP4 (`-strict unofficial`, `dvh1`); copied video checked ("HDR kept" / what was lost).
- 1.39.0: TVmaze as a fallback when TMDB has nothing (show or episode), TVmaze results in the searches, a visible per-season TVmaze status; audio rule "Copy tracks that are already compatible" (on); best + the file's own compatible track kept together.
- 1.38.0: TVmaze as a second opinion for TV episode numbers (compare, choose per show and season, titles from TVmaze, remembered; setting to turn off; credited in About).
- 1.37.1: red "Clear the queue" button; MP4Box `:dvp=` when GPAC knows it (else `:dv-profile=`); dovi_tool's "Reordering metadata" lines hidden.
- 1.37.0: Library scan results kept until the next scan (saved, shown with their date, gone/changed files marked, Clear results).
- 1.36.1: faster Dolby Vision profile 5 conversion: Vulkan device matched to the encoder; GPU decoding (on the graphics card, or copied back) used only after a 48-frame check against processor decoding on the file itself, with fallback during the job.
- 1.36.0: Dolby Vision profile 5 converted (colours to HDR10 with libplacebo on the graphics card; Dolby Vision 8.1 kept, or dropped); HDR10 information read from the Dolby Vision data; stale Dolby Vision labels removed when Dolby Vision is dropped.
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
