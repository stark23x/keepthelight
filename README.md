# Keep the Light

Windows only transcoder written in Go, that keeps HDR10, HLG, HDR10+ and Dolby Vision intact, with its own window and a command-line version.
It orchestrates FFmpeg, dovi_tool, MP4Box and (optionally) hdr10plus_tool. It has evolved into those functions, plus many functions that will help you manage your Plex (or Jellyfin or Emby) media server files.

## Download
Get the latest version from the [Releases page](https://github.com/stark23x/keepthelight/releases/latest):
download `KeepTheLight-<version>-win64.zip`, unzip it anywhere, and follow **Setup** below.

## Setup
Unzip the download anywhere and keep the folder together:

| File | What it is |
|---|---|
| Keep the Light.exe | the window (no console) |
| keepthelight_cli.exe | the command-line version, for terminals and scripts |
| tools\ | the helper programs Keep the Light runs |

The `tools` folder already contains **dovi_tool** (Dolby Vision) and **hdr10plus_tool** (HDR10+).
Two free programs have to be added once:

| Needed | For | How |
|---|---|---|
| FFmpeg | everything | Download `ffmpeg-release-essentials.zip` from [gyan.dev](https://www.gyan.dev/ffmpeg/builds/) (any build with libx265 works) and copy `ffmpeg.exe` and `ffprobe.exe` from its `bin` folder into `tools`. |
| GPAC (MP4Box) | Dolby Vision only | Install it from [gpac.io](https://gpac.io/downloads/) with its normal installer. Keep the Light finds it in `Program Files\GPAC` or on PATH. |
| Tesseract OCR | converting PGS subtitles to SRT only | Install it from [UB Mannheim's page](https://github.com/UB-Mannheim/tesseract/wiki) with its normal installer (English is included; tick other languages in the installer if you need them). Keep the Light finds it in `Program Files\Tesseract-OCR` or on PATH. |

If something is missing, the window says what and has buttons for the download pages and the tools folder. dovi_tool
and hdr10plus_tool need the Microsoft Visual C++ Runtime, which most PCs already have; if it is missing, the window
links to Microsoft's installer.

Why FFmpeg and GPAC are not included: their Windows builds contain GPL-licensed code, and passing them on means also
providing the complete source code of everything in those builds. The included tools are MIT-licensed; their licence
texts, exact versions and source links are in `tools\licenses`.

## Use
**Window:** double-click `Keep the Light.exe` (or drag a video onto the exe). Pick the input, check the detected
HDR / Dolby Vision info, choose quality and audio, press Start. It uses the normal Windows file dialogs; progress
and a log are shown live (the percentage also shows in the title bar and taskbar), and Cancel stops everything.
Closing the window while a transcode runs asks first; stopping deletes the unfinished output file.

- **Time left:** about 30 seconds into the encode, the title bar and progress line show an estimate of the
  time remaining. It is rough: speed varies through a film, and a hardware-encoder retry restarts it.
- **Queue progress:** while the queue runs, the Queue tab shows "File 2 of 5", how much of the whole batch is done
  (longer files count for more) and the time left for everything. Each file shows its own time: time left while it
  runs, an estimate such as "about 40 min" while it waits (once the first file has run for a few minutes), and how
  long it took once it is done.
- **Layout:** three tabs: **Convert** (the file and its settings), **Queue** and **Library scan**. Start, Add to queue,
  Test 30 s and the progress bar stay in view at the bottom of the window. The Queue tab shows how many files are waiting. Settings for this PC (temp folder, priority, GPU
  decoding, working locally, TMDB key, sounds) are under **⚙ Settings** at the top; profiles never include them.
  What the input file is (HDR10, Dolby Vision, resolution, and what will be kept) shows right under it. The Audio and
  Subtitles rules for new files and the queue are in a box of their own under each file's tracks. A notice about
  optional tools (GPAC for Dolby Vision, say) can be hidden; Dolby Vision files still say so if a tool is missing.
- **Sections:** click a section's heading to close or open it. Closed sections show a summary of their
  settings, and the layout is remembered (in `%LOCALAPPDATA%\Keep the Light\settings.json`).
- **Save converted files** (under Files): next to the original, or in a folder you choose. One remembered
  setting for single files and the queue. Choosing another place with **Save as…** (or typing one) changes it
  too; saving next to the original sets it back.
- **Queue (batch):** add several files with **Add files…**, **Add folder…**, by choosing several in **Browse…**,
  by dropping several videos on `Keep the Light.exe`, or with **Add to queue**. Each file keeps the settings it
  was added with (**Use current settings for waiting files** updates them). Results go next to each input as
  `name_KTL.mkv` (the ending is set under **Add to the name**), or into the folder chosen under **Save converted files**; files whose
  result already exists are skipped. Files run one after another; one that fails
  does not stop the rest. Unfinished queues are remembered and come back when you reopen Keep the Light.
  **When finished** can put the PC to sleep or shut it down (with 60 seconds to cancel).
- **Low priority** (⚙ Settings): other programs come first, so the PC stays responsive while encoding; it
  still runs at full speed when the PC is otherwise idle. Remembered, and takes effect at once.
- **Resolution** (under Video): shrink the video to fit 4K, 1440p, 1080p or 720p, keeping its shape (a
  3840x1600 film becomes 1920x800 at 1080p) and its HDR10, HDR10+ and Dolby Vision data. A smaller picture
  encodes much faster (1080p is about a quarter of the work of 4K). Videos are never enlarged.
- **Noise reduction** (under Video): Off / Light / Medium / Strong (fast, on the CPU) or High quality (on the GPU
  when the FFmpeg build and graphics driver allow it, through Vulkan or OpenCL; otherwise a slower CPU filter).
  Only filters that work in 10-bit are used, so HDR keeps its gradations; Dolby Vision is unaffected. Besides a
  cleaner picture, it usually makes files noticeably smaller. Light keeps film grain recognisable; Strong can look
  smoothed. The log says where "High quality" ran.
- **Quality guide** (under Video): for Constant quality and Target bitrate, a line under the setting judges the
  choice: bits per pixel for the resulting picture size, adjusted for the encoder and preset (GPU encoders need about
  25% more), noise reduction (less needed) and HDR (a little more needed), compared with the source (in HEVC
  terms: H.264 needs about 1.7 times the bitrate of HEVC). Very low settings get a warning, and so does a bitrate
  higher than the source needs. Target file size shows the same guide in its plan line. It is a rule of thumb;
  the 30-second test measures the real result.
- **Test 30 s** (next to Start): encodes 30 seconds with the current settings, from a chosen time or 40% into
  the film, and saves it next to the result as "... (30 s test)". It runs on its own, even when files are waiting
  in the queue. A target file size becomes the bitrate it works out to for the whole film. The test starts on the
  keyframe before the chosen time, so it can be a few seconds longer. The same seconds of the original are saved
  next to it, unchanged, as "... (30 s original)", so the two short files can be compared directly. The test is then
  measured against that original: VMAF (0-100, a score that judges like a viewer) when the FFmpeg build includes
  it, otherwise SSIM. The score is shown in the queue list and the log; noise reduction lowers it, because removed
  grain counts as a difference.
- **Full-GPU path for NVENC and QuickSync** (automatic): with an NVIDIA card (NVENC) or Intel graphics (QuickSync),
  each file is decoded, resized and encoded on the graphics card, without frames going back to the CPU. KTL
  tries it on a few frames of each file first and uses the normal way if it does not work; the log says which.
  Noise reduction runs on the CPU (so it looks the same with every encoder), so files with noise reduction go the
  normal way. If the chosen GPU encoder cannot work on this PC at all (NVENC without an NVIDIA card, QuickSync
  without Intel graphics), the window says so before you start.
- **Space saved:** each finished file shows its size before and after ("18.7 GB → 9.2 GB · saved 9.5 GB (51%)"),
  the Queue heading adds up the run, and a single running total ("4.2 TB saved in total") is kept for this PC.
  No history is stored.
- **Library scan** (its own tab): choose a folder (for example `\\PlexRaid01\Movies`), with or
  without its subfolders, and Keep the Light reads every video in it (only the file information; nothing is changed).
  Each file shows its video (codec, resolution, HDR10 / HLG / Dolby Vision profile), bitrate, audio and size, an
  estimate of its size as HEVC and the space that would save, and the things likely to make Plex convert it while you
  watch: no AC-3, E-AC-3 or AAC audio (TrueHD, DTS or FLAC only), Dolby Vision profile 7, forced picture subtitles
  (PGS/VobSub, which Plex burns in), H.264 10-bit, VC-1 / MPEG-2, and very high bitrates. Show all files, those worth
  converting (at least 300 MB and a quarter saved) or those with trouble spots; **Select worth converting**, then
  **Add selected to queue** with the current settings or a profile. The estimate assumes x265 at very good quality; a
  30-second test shows the real result. Files Keep the Light made (ending in the name tag) are left out.
- **Add a compatible audio track when needed** (Audio rules): if a file has no AC-3, E-AC-3 or AAC track (TrueHD,
  DTS or FLAC only), an E-AC-3 5.1 copy of the main track is added next to the original, so TVs and streamers that
  cannot play the original get sound without Plex converting it. Files that already have one are left as they are.
- **Work locally with network files** (⚙ Settings, remembered): a file on a network drive (`\\PlexRaid01\...`
  or a mapped drive letter) is first copied to this PC (the temp folder, or Windows' temp folder if that is on the
  network too), converted there, and the result copied back. Reading and writing over the network during a long
  encode is avoided, and a network hiccup cannot break the encode. Needs free space for the file and its result;
  without enough, the file is converted over the network as before, and the log says so. If a job fails, the local
  copy is kept and the log says where.
- **Decode on the GPU** (⚙ Settings): the graphics chip reads the source video, so the CPU only encodes.
  Speeds up x265, most with 4K sources. Before each file, Keep the Light test-decodes a few frames; if the GPU
  cannot decode it, that file is decoded on the CPU as before, and the log says which was used. Remembered.
- **Profiles** (above Video): save the settings below the Files section under a name of your own, and pick them again
  later from the dropdown. A profile holds the Video settings, the format (MKV or MP4), the Audio and Subtitles
  rules, track naming and the Advanced overrides, and can name converted files after itself (`Film_Plex 4K.mkv` instead of `Film_KTL.mkv`; type another
  ending in "Add to the name" to use it for now, ↺ brings the profile's back). "changed"
  next to the dropdown means the form no longer matches the profile; **Update** saves it. **Export…** / **Import…**
  save profiles to a .json file and read them back (a backup, or another PC). Not included: files and folders,
  one file's own tracks and metadata, PC settings (temp folder, priority, GPU decoding, sounds), and "Replace the
  original". The command-line program can use a profile too: `keepthelight_cli -profile "Plex 4K" film.mkv`
  (other options on the command line override it).
- **Changing one queued file:** **Edit** on a waiting file opens it in the window with its real tracks, chapters and
  metadata; **Save changes to the queue** puts it back with those changes (for this file only), **Cancel** leaves it
  as it was; either way the window goes back to the Queue tab, at that file. While it is being changed it waits; the
  queue goes on with the other files. **Apply a profile to waiting files** (Queue) and **Use current settings for
  waiting files** ask whether files with their own changes keep them.
- **Same tracks for others** (on a queued file with its own track choices): gives the other waiting files the same
  audio and subtitle choices, by track position (which tracks are kept, converted or dropped, the default and forced
  flags, SRT conversion). Only files whose tracks match (same number of tracks, languages and formats) are changed;
  the others are listed and left as they were. Handy for a season: set up one episode with Edit, then apply it to
  the rest.
- **Clear the queue** removes every file from the queue (after asking); a file being transcoded goes on. **Clear
  finished** removes only the finished ones.
- **Sound when finished** (⚙ Settings, remembered): when a transcode or the whole queue is done, Keep the Light
  plays a sound (Notify by default) and flashes its taskbar button; a different sound (Critical stop) means a file
  failed. Choose any Windows sound, a sound from Windows' Media folder (Alarm 1-10 and so on), your own .wav, or none.
- **One window at a time:** starting Keep the Light again, or dropping videos on the exe while it is open,
  brings the open window to the front and adds the files there.
- The PC is kept awake while a transcode runs. Results are written as `name_KTL.partial.mkv` and renamed when
  complete.
- **Temp folder** (⚙ Settings): Dolby Vision and two-pass jobs write large temporary files, next to the
  output file unless you choose a folder here. The choice is remembered.
  Backup programs such as Backblaze can be told to skip it (its files are large and gone minutes later). Files
  another program has open for a moment are waited for (up to 30 seconds), and a temporary file that is still in
  use is removed by the next job instead of failing this one.
- **About** (top right): version, contact information and licences. Its text is in `about.html` in the source.

The window uses the Microsoft Edge WebView2 Runtime, which is part of Windows 11 and installed on nearly all
Windows 10 PCs ([download](https://developer.microsoft.com/microsoft-edge/webview2/) if missing). Without it, or if it
fails to start, Keep the Light falls back to a browser window instead: the same page in an Edge/Chrome app window, served by a
private local web server. To choose that yourself, run `keepthelight_cli -server`.

- Dropping a file on the open window cannot tell Keep the Light where the file is: use Browse, paste the path
  (Ctrl+V), or drop the video onto `Keep the Light.exe` itself.
- The window keeps its browser cache in `%LOCALAPPDATA%\Keep the Light` (safe to delete). Start-up problems are
  logged to `webview2.log` there. Setting `KTL_DEBUG=1` enables right-click and DevTools in the window.

**Command line** (`keepthelight_cli.exe`):

    keepthelight_cli -crf 18 -preset slow input.mkv output.mkv
    keepthelight_cli -audio eac3 -audio-bitrate 640k movie.mkv movie_small.mp4
    keepthelight_cli -dry-run input.mkv        (prints the commands only)
    keepthelight_cli -low-priority in.mkv     (other programs come first)
    keepthelight_cli -scale 1080p in.mkv      (shrink to fit 1920x1080, keeping HDR / Dolby Vision)
    keepthelight_cli -hw-decode in.mkv        (decode on the GPU, encode on the CPU)
    keepthelight_cli -denoise light -sample 30 -sample-start 1:00:00 in.mkv test.mkv   (try noise reduction on 30 s)
    keepthelight_cli -audio-tracks 1,3:eac3 -sub-tracks 2:forced in.mkv   (choose tracks)
    keepthelight_cli -audio-lang eng -drop-commentary -subs forced in.mkv  (rules, for scripts)
    keepthelight_cli -audio-tracks 1,1:eac3:5.1 in.mkv   (keep the 7.1 track and add a 5.1 E-AC-3 copy)
    keepthelight_cli -add-compatible-audio in.mkv   (add an E-AC-3 5.1 copy if there is no AC-3/E-AC-3/AAC track)
    keepthelight_cli -work-local \\PlexRaid01\Movies\in.mkv   (copy to this PC, convert, copy back)
    keepthelight_cli -sub-tracks 1:srt,2:srt+ in.mkv   (track 1 to SRT; track 2 kept plus an SRT copy)
    keepthelight_cli -subs-to-srt replace in.mkv  (every ASS/SSA and PGS track to SRT)
    keepthelight_cli -profile "Plex 4K" in.mkv   (a profile saved in the window; other options override it)
    keepthelight_cli -audio-one-per-language best -sub-drop-names Signs -name-tracks in.mkv
    keepthelight_cli -tag _4k in.mkv          (name the result in_4k.mkv; default _KTL)
    keepthelight_cli -metadata-only -title "My Film" -replace-original in.mkv   (metadata only, in place)
    keepthelight_cli -gui [input]              (opens the window)

    keepthelight_cli -version                  (prints the name and version)

Dragging a video onto `keepthelight_cli.exe` transcodes it with the default settings. `Keep the Light.exe` also accepts
command-line options when run from a command prompt, but its output appears after the prompt, so use
`keepthelight_cli.exe` there.

## What it does
- **HDR10**: x265 10-bit with BT.2020/PQ tags, master-display and MaxCLL/FALL copied from the source.
- **HLG**: BT.2020/HLG tags plus the HLG alternative-transfer SEI.
- **HDR10+**: dynamic metadata extracted and re-injected (x265 only).
- **Dolby Vision 8.x**: RPU extracted, base layer re-encoded, RPU injected, packaged with the DV config record.
- **Dolby Vision 7**: converted to 8.1 (enhancement layer discarded).
- **Resizing**: the picture is scaled in 10-bit with its colour space unchanged, so HDR10 / HDR10+ / HLG metadata
  stays valid as is. Dolby Vision's only size-dependent data, the active area (black-bar sizes, level 5), is
  scaled with the picture scene by scene using dovi_tool; everything else in the RPU is kept unchanged.
- **Dolby Vision 5**: refused for re-encode (no HDR10 base layer; colours would be wrong). Use `-copy-video`.

## Target file size
Pick **Target file size** in the window (or `-target-size 8G` / `-target-size 4500M`). Keep the Light subtracts the audio,
subtitles and container from the target to get a video bitrate, then:
- **x265:** encodes in two passes; the result lands close to the target, usually 1-2% under it (about double the encode time).
- **NVENC / QuickSync:** these miss their bitrate, so the finished size is measured and the video is re-encoded with a
  corrected bitrate (up to `-size-retries`, default 2) until it lands at 97-100% of the target. The best attempt is kept.
- Sizes use GB = 1024^3 bytes, as Windows Explorer shows them. The window shows the resulting video bitrate and a rough
  quality guide (bits per pixel) before you start; very small targets are flagged.
- If the file does not record its audio size, Keep the Light reads the file once to measure it.
- Not available with "copy video", and not combinable with a fixed bitrate.
- Needs FFmpeg 5.1 or newer for frame-exact passes (older builds fall back to `-vsync`).

## Metadata
- The **Metadata** section shows the file's title, year or date and description, the name, language and SDH /
  commentary flags of each kept track, the chapter names, and the cover image (MKV). Change what you like; only the
  changes are written, everything else is kept. It works with every mode.
- **Look up on TMDB** (top of the Metadata section): searches The Movie Database for the film, or a TV show's
  episode, and fills in the title, date, description and cover (the poster, or the season's poster for an episode).
  The search starts from the file's name ("Dune.Part.Two.2024.2160p.REMUX.mkv" becomes "Dune Part Two", 2024;
  "The.Bear.S02E03…" becomes The Bear, season 2, episode 3; "movie.mkv" in a "Heat (1995)" folder uses the folder);
  correct it and search again if needed. Pick a result with **Use**; the fields stay editable, and nothing is written
  until the job runs. Needs your own free TMDB API key (or API read access token), entered once in ⚙ Settings:
  create a TMDB account, then Settings → API. The key is kept in Windows' Credential Manager (for your Windows
  account), not in the settings file. The language of titles and descriptions is chosen in ⚙ Settings.
- **TMDB for the queue:** **Look up metadata on TMDB** (Queue tab), or the check box next to **Add selected to
  queue** in the library scan, looks up each waiting file. Only a clear match is used (one result, or one whose title
  and year match the file's name); it fills in the title, date, description and cover, and the queue list shows what
  was found. Anything else is marked "needs a match": open it with Edit and pick the right result. Files whose title
  was already changed are skipped.
  This product uses the TMDB API but is not endorsed or certified by TMDB.
- **Metadata only** (under Video) copies every kept stream unchanged into a new file with the new metadata: no
  re-encoding, so the quality is identical, and it takes about as long as copying the file. Audio tracks can be kept
  or dropped but not converted. A new file is always written (FFmpeg cannot change a file in place), so Keep the
  Light says so before starting, with the size and the free space on the drive, and stops if there is not enough.
- **Replace the original when finished** (metadata only): the new file takes the original's name and place, and the
  original goes to the Recycle Bin. Drives without a Recycle Bin (network drives, USB sticks) would delete it for
  good, so there the original is renamed "... (original)" instead. The new file is checked (it must read as video,
  with the same length) before anything happens to the original.
- Existing MKV cover images are now kept (before, they were left out).
- Plex uses the track languages, names and default/forced flags; titles, years, descriptions and posters come from
  its own sources.

## Subtitles and audio
- **Choosing tracks:** the Audio and Subtitles sections list every track of the file. Each audio track can be
  copied, converted (E-AC-3, AC-3, AAC, Opus, FLAC) or dropped; each subtitle track kept or dropped, with its
  forced flag. One audio track and (optionally) one subtitle track are marked as the default. **Set all tracks
  to…** changes every track at once.
- **Name untitled tracks** (Metadata, remembered, also for the queue and in profiles): tracks without a name get
  one from what they are as written: "English AC-3 5.1", "English TrueHD Atmos 7.1", "English SRT Forced". Names
  already set are kept; the Metadata section shows the planned names.
- **Subtitles to SRT:** ASS/SSA (styled text) and PGS (Blu-ray picture subtitles) tracks can be converted to plain
  SRT, which every Plex player shows without Plex burning them into the picture. Per track: **as is**, **→ SRT**
  (replaces it) or **as is + SRT** (an SRT copy next to it, named "… (SRT)"); for the queue, the rule **Convert
  ASS and PGS to SRT**. Timings, language, default and forced flags are kept.
  - ASS/SSA: converted by FFmpeg, instantly. Fonts, colours, positions and effects are lost; plain dialogue
    converts cleanly, heavily styled tracks (karaoke, signs) may show doubled lines.
  - PGS: the pictures are read with **Tesseract OCR** (free, installed separately; see Setup), in the track's
    language when Tesseract has it, otherwise English. A film takes from seconds to a couple of minutes. Clean
    Blu-ray subtitles come out very accurately, but skim the result once: OCR can mistake l / I / 1, and
    italics are not marked.
  - In MP4, which cannot hold PGS, a PGS track can now be kept as text; ASS becomes plain text there anyway.
- **Rules** (remembered) give the starting choice for each file and decide the tracks for the queue, where every
  file is different: languages to keep (for example `eng, jpn`), leave out commentary tracks, **one track per
  language** (the best quality, or the most compatible: AC-3, E-AC-3 or AAC; commentary tracks are not counted), and
  all / forced only / no subtitles, and subtitles **by name**: keep only, or drop, tracks whose name contains given
  words ("SDH, Signs"; "SDH", "forced" and "commentary" also match the tracks' flags). Tracks with no language set are kept; if no audio track matches, the file's default
  track is kept, so nothing ends up silent. A file added from the form with **Add to queue** keeps exactly the
  tracks ticked for it; **Use current settings for waiting files** applies the rules.
- Converted tracks get a bitrate that suits the format and channels unless one is set. AC-3 and E-AC-3 hold at
  most 5.1, so a 7.1 track is mixed down; a title naming the old format ("TrueHD 7.1") is updated.
- MP4 cannot hold TrueHD or DTS: such a track is converted to E-AC-3 (the window offers no "Copy" for it).
- **Channels:** a converted track can keep its channels or be mixed down to 5.1 (from 7.1) or stereo, per track or
  for all converted tracks under the rules. The downmix is the conventional one: 7.1 to 5.1 combines each side and
  rear surround at -3 dB; stereo adds the centre and surrounds at -3 dB (LFE is left out). It is mixed in floating
  point with a transparent limiter, so loud passages cannot clip. Atmos height information cannot be kept when
  converting; copy the track to keep it.
- **MKV seek index at the front:** like MP4's "fast start", so Plex and other players can seek and resume
  without first reading the end of the file (needs FFmpeg 5.1 or newer; older builds write it at the end as before).
- MKV output: text subtitles are copied; MP4-style `mov_text` is converted to SubRip (MKV cannot store it); image subtitles (PGS, VobSub) are copied.
- MP4 output: text subtitles become `mov_text` (ASS styling is lost); image subtitles cannot be stored in MP4 and are dropped with a warning.
- If a run fails after the long encode (Dolby Vision / HDR10+ modes), the temporary folder is kept and its path is printed, so finished work is not deleted.

## Limits
- No cropping. Resizing keeps the frame count, so it never changes the frame rate.
- GPU decoding uses the GPU that runs the main display (Direct3D 11). The decoded frames are copied back to the
  CPU, which costs a little but keeps everything else exactly as without it.
- Dolby Vision mode assumes constant frame rate.
- QuickSync (`-codec qsv`) and NVENC (`-codec nvenc`) are fast but cannot keep HDR10+, and their quality number is not the same scale as x265 CRF.
- Re-encoding is lossy; the Dolby Vision tone-mapping metadata is preserved, not recomputed.
- The DV packaging path was verified only as a command sequence, not run against a real DV file.
  Keep the Light re-probes the output and warns if the Dolby Vision record is missing.

## Version
The current version is in `VERSION`. Version numbers follow semantic versioning; CHANGELOG.md explains
what each part means and lists the release steps. Keep the Light was called hdrtranscode up to version 0.9.

`THIRD_PARTY_NOTICES.txt` holds the licences of the open-source code built into the program (also shown in
About). Keep it with the program when sharing it.

## Build from source
Go 1.24 or newer. Both programs come from the same source; the `cli` build tag makes the command-line version.
`build.cmd` builds both:

    go build -trimpath -ldflags "-s -w -H windowsgui" -o "Keep the Light.exe" .
    go build -trimpath -tags cli -ldflags "-s -w" -o keepthelight_cli.exe .

(from a non-Windows machine, set `GOOS=windows GOARCH=amd64` first).

**Releases** are built on GitHub (`.github/workflows/release.yml`) with the current Go version, so they include Go's
latest security fixes. A push to `main` that changes `VERSION` (which needs a matching `CHANGELOG.md` entry) runs
the tests and Go's vulnerability check, builds both programs, adds dovi_tool and hdr10plus_tool (downloaded
from their author's releases and checked against their SHA-256), and makes a draft release (and its tag) with the program zip,
the source zip and `SHA256SUMS-<version>.txt`. `.github/workflows/govulncheck.yml` also checks every push and runs
weekly.

The icon, manifest (DPI awareness, modern dialogs) and version details are in `rsrc_windows_*.syso`, which
`go build` links in automatically. After changing `VERSION` or the icon, run `go generate` to rewrite them
(it runs the small generator in `tools/mkres`).

The icon is made from `res/keepthelight-original.png`: `res/mkicon.py` crops it to the artwork, fades the glow at
the edges and writes `res/keepthelight.ico` with sizes 16-256 (slightly sharpened at 32 and below). To use a new
image, replace the PNG, adjust the crop box in the script if needed, run it, then `go generate`.

## Licence
Keep the Light is free to all: its own code is dedicated to the public domain under
[CC0 1.0](LICENSE). The open-source components built into the program keep their own licences
(see `THIRD_PARTY_NOTICES.txt`), as do the helper tools in the release zip (see `tools\licenses`).
