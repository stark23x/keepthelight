# Keep the Light

Windows only transcoder written in Go, that keeps HDR10, HLG, HDR10+ and Dolby Vision intact, with its own window and a command-line version.
It orchestrates FFmpeg, dovi_tool, MP4Box and (optionally) hdr10plus_tool.

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
- **Sections:** click a section's heading to close or open it. Closed sections show a summary of their
  settings, and the layout is remembered (in `%LOCALAPPDATA%\Keep the Light\settings.json`).
- **Save converted files** (under Files): next to the original, or in a folder you choose. One remembered
  setting for single files and the queue. Choosing another place with **Save as…** (or typing one) changes it
  too; saving next to the original sets it back.
- **Queue (batch):** add several files with **Add files…**, **Add folder…**, by choosing several in **Browse…**,
  by dropping several videos on `Keep the Light.exe`, or with **Add to queue**. Each file keeps the settings it
  was added with (**Use current settings for waiting files** updates them). Results go next to each input as
  `name_hdr.mkv`, or into the folder chosen under **Save converted files**; files whose
  result already exists are skipped. Files run one after another; one that fails
  does not stop the rest. Unfinished queues are remembered and come back when you reopen Keep the Light.
  **When finished** can put the PC to sleep or shut it down (with 60 seconds to cancel).
- **Low priority** (under Advanced): other programs come first, so the PC stays responsive while encoding; it
  still runs at full speed when the PC is otherwise idle. Remembered, and takes effect at once.
- **Resolution** (under Video): shrink the video to fit 4K, 1440p, 1080p or 720p, keeping its shape (a
  3840x1600 film becomes 1920x800 at 1080p) and its HDR10, HDR10+ and Dolby Vision data. A smaller picture
  encodes much faster (1080p is about a quarter of the work of 4K). Videos are never enlarged.
- **Decode on the GPU** (under Advanced): the graphics chip reads the source video, so the CPU only encodes.
  Speeds up x265, most with 4K sources. Before each file, Keep the Light test-decodes a few frames; if the GPU
  cannot decode it, that file is decoded on the CPU as before, and the log says which was used. Remembered.
- **One window at a time:** starting Keep the Light again, or dropping videos on the exe while it is open,
  brings the open window to the front and adds the files there.
- The PC is kept awake while a transcode runs. Results are written as `name_hdr.partial.mkv` and renamed when
  complete.
- **Temp folder** (under Advanced): Dolby Vision and two-pass jobs write large temporary files, next to the
  output file unless you choose a folder here. The choice is remembered.
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
    keepthelight_cli -audio-tracks 1,3:eac3 -sub-tracks 2:forced in.mkv   (choose tracks)
    keepthelight_cli -audio-lang eng -drop-commentary -subs forced in.mkv  (rules, for scripts)
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

## Subtitles and audio
- **Choosing tracks:** the Audio and Subtitles sections list every track of the file. Each audio track can be
  copied, converted (E-AC-3, AC-3, AAC, Opus, FLAC) or dropped; each subtitle track kept or dropped, with its
  forced flag. One audio track and (optionally) one subtitle track are marked as the default.
- **Rules** (remembered) give the starting choice for each file and decide the tracks for the queue, where every
  file is different: languages to keep (for example `eng, jpn`), leave out commentary tracks, and all / forced
  only / no subtitles. Tracks with no language set are kept; if no audio track matches, the file's default
  track is kept, so nothing ends up silent. A file added from the form with **Add to queue** keeps exactly the
  tracks ticked for it; **Use current settings for waiting files** applies the rules.
- Converted tracks get a bitrate that suits the format and channels unless one is set. AC-3 and E-AC-3 hold at
  most 5.1, so a 7.1 track is mixed down; a title naming the old format ("TrueHD 7.1") is updated.
- MP4 cannot hold TrueHD or DTS: such a track is converted to E-AC-3 (the window offers no "Copy" for it).
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
