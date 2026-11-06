# Keep the Light: feature backlog

The working list of feature ideas. Claude reads it before every build and builds what is under "Requested for the
next build". Shipped features move to "Done" with their version number. Nothing is built until the owner says
"build".

## Requested for the next build

- **Track names in English, not the language's own script.** When a track's existing name is only a language
  written in its own script ("日本語", "한국어", "Русский", "Deutsch"), replace it with the English automatic name
  ("Japanese AC-3 5.1"). Names with anything more in them (for example "日本語 コメンタリー") are left alone. The
  new name shows in the track list before converting and can be typed over. Also add the language codes that are
  missing from the list (such as `yue` Cantonese and `cmn` Mandarin) so they come out as English words, not codes.

- **Track names follow through a series in the queue.** When you edit one episode in the queue and change its track
  names, Keep the Light offers, on saving, to give the same names to the other waiting episodes of that show
  ("Use these track names for the other 9 episodes of The Office?"). The track language, commentary/SDH marks and
  track choices go with them. Only episodes whose tracks are laid out the same way (same kinds, in the same order)
  are changed; the others are listed and left alone. Each episode's own title, description and poster are never
  copied. "Same show" means the TMDB match, or the show name in the file name when there is no match. The existing
  "Use these tracks for the others" button should copy the track names too.

## Ideas raised, not yet requested

None at the moment.

## Done

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
