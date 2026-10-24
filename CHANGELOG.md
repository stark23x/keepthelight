# Changelog

All notable changes to Keep the Light. Newest first.

## Versioning

Keep the Light uses [semantic versioning](https://semver.org): **MAJOR.MINOR.PATCH**.

- **PATCH** (1.0.0 → 1.0.1): bug fixes only; same options, same behaviour otherwise.
- **MINOR** (1.0.1 → 1.1.0): new features or options; existing command lines and settings keep working.
- **MAJOR** (1.1.0 → 2.0.0): changes that break existing use (removed or renamed options, different defaults
  that change the output).

The version lives in one place, the `VERSION` file. To release:

1. Change `VERSION`.
2. Add a `## X.Y.Z - date` entry at the top of this list (the tests fail if it is missing).
3. Run `go generate` (stamps the version into the exe's file properties).
4. Run `build.cmd`.

## 1.4.1 - 2026-10-24

- **Fix: Dolby Vision files failing with "MP4Box.exe failed; exit status 0xc0000135".** That code means Windows
  could not start MP4Box because a DLL it needs is missing, typically when `mp4box.exe` was copied somewhere on
  its own without GPAC's other files, or when the Microsoft Visual C++ Runtime is missing.
  - Keep the Light now checks that MP4Box, dovi_tool and the other tools actually start. A copy that cannot
    start is skipped in favour of the next one (for example GPAC's own install folder).
  - If none works, the window says which file is broken and why, instead of failing in the middle of a batch.
  - Dolby Vision files are checked for this when they are added, so the problem shows before a batch starts
    (setting Dolby Vision to "Drop (plain HDR10)" still lets them be transcoded).
  - **Check again** re-tests the tools, so installing GPAC or the runtime is picked up without restarting.
  - The log shows which dovi_tool and MP4Box are used.

## 1.4.0 - 2026-10-23

- **Low priority** (under Advanced): transcodes run at Windows' "below normal" priority, so other programs come
  first and the PC stays responsive. When nothing else needs the CPU the encode still runs at full speed. The
  setting is remembered, applies to every file in a batch, and takes effect immediately, also on a transcode
  that is already running.
- `keepthelight_cli.exe` has the same option: `-low-priority`.

## 1.3.1 - 2026-10-23

- **Output folder for batches:** the Queue section has "Save converted files: next to each original / in this
  folder", and the choice is remembered. If the saved folder is not available (deleted, or its drive is not
  connected), the window says so and files are saved next to the originals. **Use current settings for waiting
  files** also moves waiting files to the chosen location.
- Files are now refused when they are added if their output folder does not exist, instead of failing later.

## 1.3.0 - 2026-10-23

- **Batch transcoding.** A new **Queue** section holds files to transcode one after another.
  - Add files with **Add files…** / **Add folder…**, by choosing several files in **Browse…**, by dragging several
    videos onto `Keep the Light.exe`, or with **Add to queue** for the file in the form.
  - Each file keeps the settings that were set when it was added; **Use current settings for waiting files**
    changes them all at once.
  - Every file is checked when it is added, so problems (for example a Dolby Vision profile that cannot be
    re-encoded) show before the queue starts.
  - Outputs go next to each input as `name_hdr.mkv` (or `.mp4`). A file whose output already exists is skipped.
  - Files run one at a time. A failed file does not stop the rest; its reason and log stay in the list.
    Files can be added, removed and reordered while the queue runs. **Stop queue** cancels the current file and
    starts no more.
  - The title bar shows "File 2 of 5", and the time left covers the whole queue.
  - Files that were still waiting are remembered when Keep the Light is closed, and come back (not started)
    the next time.
- **When finished:** do nothing, sleep or shut down, with 60 seconds to cancel. It applies to one run only.
- **The PC no longer goes to sleep during a transcode** (the screen can still turn off). This also applies to
  `keepthelight_cli.exe`.
- A transcode is written to `name_hdr.partial.mkv` and renamed when it is complete, so an interrupted run never
  leaves a half-written file under the real name.
- **Start transcode** now asks before replacing an output file that already exists.

## 1.2.1 - 2026-10-23

- The **temp folder** (under Advanced) is remembered between runs. If the saved folder is no longer available
  (deleted, or on a drive that is not connected), the window says so and temporary files go next to the output
  file as usual. Clearing the field goes back to that default.

## 1.2.0 - 2026-10-23

- **The command-line program is now `keepthelight_cli.exe`** (was `keepthelight.exe`). Scripts that call it need
  the new name. (Strictly a breaking change; kept as a minor release because 1.0 and 1.1 came out the same day.)
- **Helper tools included:** dovi_tool 2.3.4 and hdr10plus_tool 1.7.2 now come in the `tools` folder, with their
  licences and source references in `tools\licenses`.
- **Setup help:** FFmpeg and GPAC (MP4Box) are not included because of their licence terms (see README). When one is
  missing, the window explains where to get it, with **Get FFmpeg** / **Get GPAC**, **Open tools folder** and
  **Check again** buttons. It also warns if the Visual C++ Runtime the bundled tools need is missing.
- MP4Box is now also found in GPAC's normal install folder (`Program Files\GPAC`), so installing GPAC is enough.

## 1.1.0 - 2026-10-23

- **Time remaining:** while a job runs, the title bar and the progress line show an estimate of the time left
  (for example "42% · about 55 min left"). It appears once the encode has run for about 30 seconds and
  updates as it goes; a hardware-encoder retry to hit a target size restarts it.
- **Whole-job percentage:** the percentage now covers the entire job instead of starting again at 0 for each
  step (two-pass encodes, Dolby Vision packaging).
- **Collapsible sections:** Files, Source, Video, Audio and Advanced can each be closed. A closed section shows a
  one-line summary of its settings, and the open/closed layout is remembered. If the file has a problem that
  stops the job, the Source section opens by itself; warnings show as a marker in its summary.
- **About:** an About button at the top right shows information about Keep the Light, the version, a note that
  it was written with the help of Claude, and the open-source licences of the components included in the
  program. Links open in the normal browser or mail program.
- Settings are stored in `%LOCALAPPDATA%\Keep the Light\settings.json`.

## 1.0.0 - 2026-10-23

First release under the name **Keep the Light** (previously "hdrtranscode", versions 0.1-0.9 below).

- New name and icon. The programs are now `Keep the Light.exe` (window) and `keepthelight.exe` (command line).
- Version numbers follow semantic versioning from here on (see above).
- The exe's file properties (Explorer > Properties > Details) show the product name and version.
- Temporary folders are now named `.keepthelight-...`. Window settings and cache moved to
  `%LOCALAPPDATA%\Keep the Light` (the old `%LOCALAPPDATA%\hdrtranscode` folder can be deleted).
- The debug switch is now `KTL_DEBUG=1` (was `HDRT_DEBUG`).

Note: hdrtranscode builds 0.1-0.7 reported "1.0.0" internally. They are unrelated to Keep the Light 1.0.0;
Keep the Light shows its name next to the version (`keepthelight -version` prints "Keep the Light 1.0.0").

### Earlier versions (as hdrtranscode)

| Version | Changes |
|---|---|
| 0.9 | Own window (WebView2) instead of a browser app window, with the old web-server mode kept as a fallback (`-server`); separate `hdrtranscode.exe` (window) and `hdrtranscode-cli.exe` (console); confirm before closing during a transcode; progress in the title bar; app icon; sharp on high-DPI screens; no console windows flashing for ffmpeg/ffprobe |
| 0.8 | Version numbers (`-version`, shown in the window and file name) |
| 0.7 | Target file size |
| 0.6 | Subtitle fix; temp files kept on failure |
| 0.5 | Intel QuickSync encoder |
| 0.4 | Native Windows Browse / Save As dialogs |
| 0.3 | Console stays open and prints the address |
| 0.2 | Added the window (browser-based GUI) |
| 0.1 | Command-line transcoder: HDR10, HLG, HDR10+, Dolby Vision |

Builds 0.1-0.7 were delivered before numbers existed and report "1.0.0" internally.
