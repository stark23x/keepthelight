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

## 1.23.0 - 2026-10-31

- **Plex check when files are added.** Files added to Convert or the Queue get the same Plex checks as the
  library scan (no AC-3, E-AC-3 or AAC audio; forced picture subtitles; styled subtitles shown by default; Dolby
  Vision profile 7 or 5; H.264 10-bit; VC-1 / MPEG-2; very high bitrates), so problems show up without scanning
  the library. The check knows the settings: each problem is marked as fixed by them, still there (with the setting
  that would fix it), or not fixable. The Queue tab adds a line such as "1 of 10 waiting files will still have Plex
  problems", and each row lists its own. Every finished file is checked again ("Plex check:" lines in the log, and a
  ✓ or ⚠ in its row). Advice only: nothing is stopped.
- **Two smaller resolutions: 576p and 480p.** The picture fits within 1024×576 or 854×480, keeping its shape (a
  2.40:1 film becomes 854×356 at 480p), with HDR10, HLG, HDR10+ and Dolby Vision kept as for the other sizes. Also
  for `-scale 576p` / `-scale 480p`. Tested with NVENC and QuickSync.

## 1.22.0 - 2026-10-31

- **Empty subtitle and audio tracks are left out automatically.** A track with nothing in it (for example a
  subtitle track holding no subtitles) can make FFmpeg fail partway through an encode, with an error that looks
  like the video encoder's ("hevc_nvenc … Invalid argument"). Keep the Light now spots such tracks from the file's
  own statistics, marks them "EMPTY", and leaves them out, also for queued files, with a note in the log. Ticking
  one keeps it anyway (with a warning).
- **Skip Plex extras in the library scan** (on by default): trailers, featurettes, deleted scenes and the other
  extras Plex recognises by folder or file name, and Plex Versions folders. The summary says how many were skipped.
- **Right-click menu:** Cut, Copy, Paste and Select all in text boxes; Copy and Select all elsewhere. Paste reads
  the Windows clipboard directly.
- **Fix: a queued file's log could not be scrolled** (it jumped back every half-second). It also has a **Copy
  log** button now.

## 1.21.0 - 2026-10-29

- **Progress for the whole batch and for each file** (Queue tab).
  - A bar at the top shows how far the batch has got, for example "File 2 of 5 · 38% of the batch done", and the
    time left for all of it. A long film counts for more than a short episode, so the percentage reflects the real
    work. Before the queue starts, it shows how many files are waiting and, once known, how long they will take.
  - The running file shows its percentage and time left; each waiting file an estimate ("about 40 min"), based on
    the speed measured for similar files (same encoder, preset and resolution) or scaled from the current one;
    finished files show how long they took. Until a speed has been measured, waiting files say "estimate after the
    first minutes".
- A test in the test suite that could fail when the PC was busy waits properly now (no change to the program).

## 1.20.1 - 2026-10-28

- **Built with the current Go version.** Releases are now built on GitHub with the latest Go, which includes fixes
  for security problems found in Go's own web and certificate code (`net/http`, `crypto/x509`) in the older Go
  version 1.20.0 was built with. The check of Go's vulnerability database passes. No change to how Keep the Light
  works.
- The tools folder's README mentions Tesseract OCR (for PGS subtitles to SRT).

## 1.20.0 - 2026-10-28

- **Same tracks for others.** A queued file with its own track choices has a **Same tracks for others** button: the
  other waiting files get the same audio and subtitle choices (by track position, including SRT conversion and the
  default and forced flags). Only files with the same track layout are changed; the rest are listed. Set up one
  episode with Edit, then apply it to the season.
- **Smarter rules:** **one audio track per language** (the best quality, or the most compatible), and subtitles
  **by name** (keep only, or drop, tracks whose name contains given words). Saved in profiles.
  `-audio-one-per-language best|compatible`, `-sub-keep-names`, `-sub-drop-names`.
- **Name untitled tracks** from what they are ("English AC-3 5.1", "English SRT Forced"); names already set are
  kept. In the Metadata section, remembered, used for the queue, saved in profiles. `-name-tracks`.
- **Clear the queue** button (asks first; a file being transcoded goes on).
- **Back to the Queue tab** after Save changes or Cancel when changing a queued file, at that file.
- **TMDB for the queue and the library scan:** **Look up metadata on TMDB** (Queue tab) and a check box next to
  **Add selected to queue** (library scan). A clear match fills in the title, date, description and cover; anything
  else is marked "needs a match", to be chosen with Edit.
- **TMDB language** for titles and descriptions (⚙ Settings): 21 languages.
- **The TMDB key is kept in Windows' Credential Manager**, not in the settings file (moved there automatically).
- **Profiles keep the format** (MKV or MP4), also when applied to waiting files or used for the library scan.
- **`-profile <name>`** for the command-line program: uses a profile saved in the window; other options override it.
- **Tesseract language hint:** when a PGS track's language is not installed in Tesseract, the window says so before
  starting, with how to add it.
- Security: every release now comes with SHA-256 checksums (`SHA256SUMS.txt`), and the source repository checks the
  Go code and libraries against Go's vulnerability database on every change (GitHub Actions).

## 1.19.2 - 2026-10-28

- **Files in use by another program no longer fail a job.** Backup programs (Backblaze, OneDrive), antivirus
  scanners and indexers briefly open new files, and while they do Windows refuses to delete, rename or move them
  ("The process cannot access the file because it is being used by another process").
  - Keep the Light now retries those operations for up to 30 seconds, which is usually more than enough.
  - Deleting a temporary file never fails a job any more: if a file is still in use, the job finishes normally,
    the log says so, and the next job removes it. Only Keep the Light's own temporary files are removed this way.
  - Fixed: with the temp folder on another drive than the output, a target-size job failed at the very end when
    the temporary attempt could not be deleted, although the result had already been copied into place.
  - Tip: exclude the temp folder from backup programs; its files are large and gone minutes later.
- Fixed a test in the test suite that could fail when the PC was busy (no change to the program).

## 1.19.1 - 2026-10-27

- **Clearer warnings when Tesseract OCR is missing.** Before, the only warning was under the input file, out of view
  while choosing subtitles. Now:
  - the tools notice at the top says it is needed for converting PGS subtitles, with a **Get Tesseract** button
    (everything else works without it);
  - the Subtitles section warns right under the tracks as soon as a PGS track is set to SRT, and under the rule
    when **Convert ASS and PGS to SRT** is on, each with **Get Tesseract** and **Check again**.

## 1.19.0 - 2026-10-27

- **Subtitles to SRT.** ASS/SSA and PGS subtitle tracks can be converted to plain SRT, which every Plex player can
  show without Plex burning them into the picture (and converting the video). Each such track has **as is**,
  **→ SRT** (replace) or **as is + SRT** (an SRT copy next to it); the Subtitles rules have **Convert ASS and PGS to
  SRT** for new files and the queue (saved in profiles); **Set all tracks to…** can convert them all. Timings,
  language, default and forced flags carry over.
  - ASS/SSA is converted by FFmpeg (styling is lost).
  - PGS (picture) subtitles are read with **Tesseract OCR**, installed separately (free). Keep the Light copies the
    track out, reads the pictures and their timings itself, and has Tesseract read the text, in the track's
    language when installed. The window says if Tesseract is missing, with a link to get it. The log reports how
    many lines were read.
  - In MP4, PGS tracks can now be kept, as text.
- Library scan: forced PGS subtitles and ASS subtitles shown by default point to the SRT conversion.
- `keepthelight_cli.exe`: `-sub-tracks 1:srt,2:srt+`, `-subs-to-srt replace|add`, `-tesseract <path>`.

## 1.18.1 - 2026-10-27

- Start, Test 30 s and the progress bar now stay at the bottom of the window at every width. On wide windows they
  were in a column on the right, which covered part of the "Save changes to the queue" banner while changing a queued
  file.

## 1.18.0 - 2026-10-27

- **New layout.** Tabs for **Convert**, **Queue** and **Library scan**, so the settings for a file start right below
  it. Start, Add to queue, **Test 30 s** and the progress bar stay in view at the bottom (on a window 1400 pixels or
  wider, in a column on the right, with the log open). The Queue tab's count pulses when files are added; "Queue: 2
  waiting →" next to Start goes there.
- **⚙ Settings** (top right) holds the settings for this PC: temp folder, low priority, GPU decoding, working
  locally, the TMDB key and the sounds. Advanced now has only the HDR10 overrides.
- **The source under the input:** the Source section is now a box under the input file, with a plain line on what
  will be kept ("Will keep: Dolby Vision (profile 8.1) and the HDR10 information").
- **Profile** is the heading of the settings, with Rename, Delete, Export and Import under **More ▾**.
- **Rules apart from tracks:** each Audio and Subtitles section shows the file's tracks, then a closed box "For new
  files and the queue" with the rules and a one-line summary of them.
- **Less text up front:** longer explanations are behind **More** or **?**.
- Subtitles have a "Set all tracks to…" menu (Keep all / Drop all) like Audio; the tools notice can be hidden when
  it is only about optional tools; Video lists the quality before noise reduction; lighter hint text.

## 1.17.0 - 2026-10-27

- **Look up metadata on TMDB.** The Metadata section can search The Movie Database for a film or a TV episode and
  fill in the title, date, description and cover (poster) with one click; the fields stay editable for corrections.
  The search is guessed from the file's name (or its folder's, for names like "movie.mkv"), including SxxEyy for
  episodes. English, single files for now. Needs a free TMDB API key, entered under Advanced (with a Check button).
  Posters are saved in `%LOCALAPPDATA%\Keep the Light\covers` and removed after 60 days.

## 1.16.0 - 2026-10-26

- **Library scan: what's worth converting?** A new section reads every video in a folder (and its subfolders, if
  you like), without changing anything, and lists each file's video, bitrate, audio and size, with an estimate of
  the space converting it to HEVC would save. It points out Plex trouble spots: no AC-3, E-AC-3 or AAC audio, Dolby
  Vision profile 7, forced picture subtitles, H.264 10-bit, VC-1 / MPEG-2 and very high bitrates. Pick files (or
  **Select worth converting**) and add them to the queue with the current settings or a profile. Reading runs a
  few files at a time and can be stopped; results appear as they come in.
- **"Add a compatible audio track when needed"** (Audio rules, saved in profiles; `-add-compatible-audio`): files
  with only TrueHD, DTS or FLAC audio get an E-AC-3 5.1 copy of the main track next to the original.
- **Work locally with network files** (Advanced; `-work-local`): network files are copied to this PC, converted
  there and the result copied back, with progress shown as "Copying". Falls back to working over the network when
  there is not enough free space.

## 1.15.0 - 2026-10-26

- **Full-GPU path for NVENC and QuickSync.** With NVENC (NVIDIA) or QuickSync (Intel), each file is now decoded,
  resized and encoded entirely on the graphics card, without frames going back and forth to the CPU: faster,
  especially for 4K, and the CPU stays free (on the Plex server, for Plex). It is automatic: KTL tries it on a few
  frames of each file and uses the normal way if it does not work (an old driver, an unusual source format). The
  log says which ("Full-GPU path: on, NVIDIA (CUDA)"). Files with noise reduction go the normal way, because
  noise reduction runs on the CPU so that it looks the same with every encoder. x265 is unchanged.
- **"Not available on this PC" warning.** If NVENC or QuickSync cannot work on this PC (NVENC without an NVIDIA card,
  QuickSync without Intel graphics, or a missing driver), the window says so as soon as it is chosen, with the
  reason, instead of the job failing when it starts. Useful when profiles move between PCs.
- **Space saved.** Each finished file shows its size before and after, for example "18.7 GB → 9.2 GB · saved 9.5 GB
  (51%)" (or how much bigger it got). The Queue heading adds up the run, and keeps a single running total for this
  PC ("4.2 TB saved in total"). No history is kept. The command-line program prints the same "Size:" line.

## 1.14.0 - 2026-10-26

- **Quality guide** under the quality setting, for Constant quality and Target bitrate (Target file size shows it in
  its plan line). It judges the bits per pixel of the resulting picture, adjusted for the encoder and preset, noise
  reduction and HDR, and compares with the source in HEVC terms: 10 Mbps is "good" for 4K HDR but "excellent" for
  1080p from an H.264 Blu-ray. Very low settings get a warning (also in the log), and so does a bitrate higher
  than the source needs. It is a rule of thumb, not a measurement.
- **The 30-second test is measured**: after a test, Keep the Light compares it with the same 30 seconds of the
  original and reports a score, for example "Test quality: VMAF 94.2 of 100 (very good: differences are hard to
  see)". VMAF judges like a viewer and is used when the FFmpeg build includes it (gyan.dev "full" and BtbN builds
  do); otherwise SSIM. The score appears in the queue list and the log. Noise reduction lowers the score, because
  removed grain counts as a difference; the message says so.

## 1.13.1 - 2026-10-26

- **Test 30 seconds also keeps the same 30 seconds of the original**, unchanged, as "... (30 s original)" next to
  "... (30 s test)", so the two short files can be played one after the other or side by side. A new test replaces
  both.
- The Video section explains what the test does: only 30 seconds are converted, which two files it saves, and that
  the whole film is converted only with Start.

## 1.13.0 - 2026-10-26

- **Noise reduction** (Video section): Off, Light, Medium, Strong, or High quality.
  - Light / Medium / Strong use FFmpeg's hqdn3d filter: fast, on the CPU.
  - High quality uses nlmeans on the GPU (through Vulkan or OpenCL, whichever the FFmpeg build and graphics driver
    support; it is tested on the file first). Without a usable GPU filter it uses fftdnoiz on the CPU, which is
    slower. The log says which was used.
  - Only filters that work in 10-bit are used (FFmpeg's CPU nlmeans converts to 8-bit and would cause banding in
    HDR, so it is not used). Every frame is kept, so Dolby Vision data still lines up.
  - Applied after resizing (fewer pixels to clean). Saved in profiles; profiles saved earlier mean Off.
- **Test 30 seconds** (Video section): encodes 30 seconds with the current settings, starting at a chosen time
  (or 40% into the film), and saves it next to the result as "... (30 s test)". It runs on its own: files
  waiting in the queue are not started. With a target file size, the test uses the bitrate the size works out to
  for the whole film. The test begins on the keyframe before the chosen time, so it can be a few seconds longer.
- `keepthelight_cli.exe`: `-denoise light|medium|strong|hq`, `-sample <seconds>`, `-sample-start <time>`.

## 1.12.1 - 2026-10-26

- **"Add to the name" can be typed in while a profile names the files.** The box shows the profile's ending; typing
  another one uses it for now (for the file in the form and files added to the queue), without changing the
  profile or your own remembered ending. **↺** next to the box, or choosing the profile again, brings the
  profile's ending back.

## 1.12.0 - 2026-10-26

- **Keep all / drop all subtitles** with one checkbox above the subtitle list. It is ticked when every track is kept,
  empty when none is, and shows a dash when only some are. Ticking keeps every track (each with the forced flag it
  has in the file); unticking drops them all and clears the default subtitle. Picture subtitles that MP4 cannot
  hold are left out of "all".
- **Set all audio tracks at once** with the **Set all tracks to…** menu above the audio list: Copy, Convert to
  E-AC-3 / AC-3 / AAC / Opus / FLAC, or Drop (which asks first, since the file would have no sound). In
  "Metadata only" mode it offers Copy and Drop.
- Both work when changing a queued file, too.

## 1.11.1 - 2026-10-25

- **Fix: the "Add to the name" box could not be typed in** (since 1.11.0). Each keystroke put the saved ending
  back. It now only shows a profile's ending while that profile names the files, and gives yours back afterwards.

## 1.11.0 - 2026-10-25

- **Profiles.** Save the settings below the Files section under a name of your own and pick them again later from
  the **Profile** dropdown (above Video), with **Save as new…**, **Update**, **Rename…** and **Delete**.
  - A profile holds the Video settings (mode, encoder, preset, resolution, Dolby Vision, quality), the Audio and
    Subtitles rules (languages, convert/copy, channels, bitrate, commentary, which subtitles) and the Advanced
    overrides. Not included: files and folders, one file's own tracks and metadata, PC settings (temp folder,
    priority, GPU decoding, sounds) and "Replace the original".
  - **Converted files can be named after the profile**: `Film_Plex 4K.mkv` instead of `Film_KTL.mkv` (a choice when
    saving; renaming the profile renames the ending too).
  - "changed" next to the dropdown shows when the form no longer matches the profile.
  - **Export…** / **Import…** save profiles to a .json file and read them back. Imported profiles with a name you
    already use can replace yours or be added with a number.
  - **Apply a profile to waiting files** in the Queue section.
- **Change one queued file.** **Edit** on a waiting file opens it in the window with its real tracks, chapters,
  cover and metadata. Change anything (tracks, track names, title, resolution...) and **Save changes to the
  queue**, or **Cancel**. The changes apply to that file only and are kept with the queue when Keep the Light is
  closed. While it is being changed the file waits; the queue goes on with the others.
  - Files with their own changes are marked in the list ("own choice", "own metadata").
  - **Use current settings for waiting files** and **Apply a profile** ask whether those files keep their own
    changes or have them replaced too.

## 1.10.0 - 2026-10-25

- **A sound when the work is done.** When a transcode, or the whole queue, has finished, Keep the Light plays a
  sound: **Notify** when every file worked, **Critical stop** when a file failed. For a queue it plays once,
  after the last file; stopping the queue or closing the window plays nothing.
  - Choose the sounds under Advanced (remembered), each with a **Test** button: any Windows sound event (these
    follow your Windows sound scheme), any sound in Windows' Media folder (Alarm 1-10, Ring 1-10, Tada, Chimes...),
    your own .wav file, or none.
  - The taskbar button also flashes until the window is used, so you can tell even with the sound muted (can be
    turned off).
  - The sound plays before a sleep or shut-down countdown starts.
- `keepthelight_cli.exe -sound` plays the same sounds at the end.

## 1.9.0 - 2026-10-25

- **Edit metadata.** A new **Metadata** section shows and changes the file's title, year or date and description;
  each kept track's name, language and SDH / commentary flags; chapter names (or leave all chapters out); and the
  cover image (MKV: choose a .jpg/.png, or leave the cover out). Only the changes are written. It works with
  every mode, including re-encoding.
- **Metadata only** (a third choice under Video, next to Re-encode and Copy video): every kept stream is copied
  unchanged into a new file with the new metadata. Nothing is re-encoded, so the quality is identical, and it
  takes about as long as copying the file. Tracks can be kept or dropped, not converted.
  - Before it starts, Keep the Light explains that **a new file has to be created** (FFmpeg cannot change a file in
    place), with the new file's size and the free space on its drive. If there is not enough space it says so
    and does not start. For the queue, one message covers all metadata-only files.
- **Replace the original when finished** (optional, metadata only): the new file takes the original's name and
  place and the original goes to the **Recycle Bin**. On drives without a Recycle Bin (network drives, USB
  sticks), which would delete it for good, the original is renamed "... (original)" instead. The new file is
  checked first (it must read as video, with the original's length); if anything is wrong, the original is not
  touched.
- **Fix: MKV cover images are kept.** FFmpeg reads an MKV's cover as a picture stream, which was left out; covers
  are now carried over (and can be replaced or removed).
- **Fix: Dolby Vision files keep the file's title and tags** (they came from an intermediate file before).
- `keepthelight_cli.exe`: `-metadata-only`, `-replace-original`, `-title`, `-no-chapters`, `-cover`, `-no-cover`, and
  `-meta-json` for everything at once.

## 1.8.0 - 2026-10-25

- **Mix 7.1 down to 5.1, or to stereo.** Converted audio tracks have a channels choice: as the source, mix down to
  5.1 (for 7.1 tracks) or mix down to stereo. It is set per track, or for all converted tracks under the rules
  (remembered, used by the queue).
  - The conventional downmix: 7.1 to 5.1 keeps the fronts, centre and LFE and combines each side and rear
    surround at -3 dB; stereo adds the centre and surrounds at -3 dB and leaves out the LFE. Mixed in floating
    point with a transparent limiter, so loud passages cannot clip.
  - 7.1 converted to AC-3 or E-AC-3 (which hold at most 5.1) now uses the same downmix (before: FFmpeg's automatic one).
  - Bitrates and track titles follow the new channel count ("AAC 5.1").
  - `keepthelight_cli.exe`: `-audio-channels 5.1|stereo`, or per track, e.g. `-audio-tracks 1,1:eac3:5.1`.
- **MKV files have their seek index at the front**, like MP4's "fast start", so Plex and other players can seek and
  resume without first reading the end of the file. Space is reserved for it, so no extra time is needed; if it is
  ever not enough, FFmpeg makes room. Needs FFmpeg 5.1 or newer (older builds work as before).
- **The ending added to file names is your choice** (Files section, **Add to the name**, remembered). The default is
  now **`_KTL`** (was `_hdr`), and it can be anything, or nothing. With nothing added, save in another folder or as
  MP4, since a file cannot replace its original. `keepthelight_cli.exe`: `-tag`.
- **Add folder** skips files that already end in the current ending, `_KTL` or `_hdr` (results of earlier runs).

## 1.7.0 - 2026-10-25

- **Choose audio and subtitle tracks.** The Audio and new Subtitles sections list every track of the file
  (language, format, channels, title, SDH/commentary).
  - Each audio track: **Copy**, **Convert** (E-AC-3, AC-3, AAC, Opus or FLAC) or **Drop**. For example, keep the
    TrueHD track as it is and add an E-AC-3 copy for devices that cannot play TrueHD.
  - Each subtitle track: keep or drop, and set or clear its **forced** flag.
  - Choose the **default** audio track and the default subtitle (or none).
  - MP4 limits are shown per track: TrueHD/DTS cannot be copied into MP4 (they are converted to E-AC-3), and
    picture subtitles (PGS, VobSub) cannot be kept.
  - A target file size counts only the kept tracks.
- **Rules for the queue** (remembered): languages to keep for audio and subtitles, leave out commentary tracks,
  and all / forced only / no subtitles. They give the starting choice for each file in the form and decide the
  tracks for files added to the queue with Add files, Add folder or by dropping several. Tracks with no
  language set are kept, and if no audio track matches, the file's default track is kept, so nothing ends up
  silent. A file added with **Add to queue** keeps exactly the tracks ticked for it.
- Converted tracks get a bitrate that suits the format and channels unless one is set. 7.1 converted to AC-3 or
  E-AC-3 (which hold at most 5.1) is mixed down, and a track title naming the old format is updated.
- `keepthelight_cli.exe`: `-audio-tracks`, `-audio-default`, `-sub-tracks`, `-sub-default`, and the rules
  `-audio-lang`, `-drop-commentary`, `-sub-lang`, `-subs`. Without them every track is kept, as before.

## 1.6.1 - 2026-10-25

- The **Queue** section now sits right under **Files**, so both ways of choosing videos are together, followed
  by the settings that apply to both (Source, Video, Audio, Advanced).
- The queue starts closed and opens by itself when files are added, or when files were left from last time.
- A long queue scrolls inside its section (about six files show), so the settings stay close. When the next file
  starts, the list scrolls to it.

## 1.6.0 - 2026-10-25

- **One remembered place for converted files.** **Save converted files** (next to the original, or in a chosen
  folder) moved from the Queue section to the Files section and now applies to single files as well as the
  queue. The output file is filled in from it, and the choice is remembered.
  - Choosing another place with **Save as…** (or typing a path) makes that folder the remembered one. Saving
    next to the original sets it back to "next to the original".
  - Changing the folder moves the output file there and keeps its name.
  - A new input file always gets a fresh output name in the remembered folder (before, a name typed for an
    earlier file was kept).
  - The folder chosen for batches in 1.3.1-1.5.0 carries over.

## 1.5.0 - 2026-10-24

- **Resolution** (under Video): shrink the video to fit 4K (2160p), 1440p, 1080p or 720p. The shape is kept
  (a 3840x1600 film becomes 1920x800 at 1080p), and so is the HDR:
  - HDR10, HLG and HDR10+ metadata carry over unchanged (they describe brightness and colour, not pixels).
  - Dolby Vision: the black-bar sizes it records (the "active area") are scaled with the picture, scene by
    scene, so films that change aspect ratio stay correct. All other Dolby Vision data is kept unchanged.
  - A smaller picture encodes much faster: 1080p is about a quarter of the work of 4K.
  - Videos are never enlarged; one that already fits is left as it is, with a note.
  - The plan line shows the result (for example "resized 3840x1600 → 1920x800"), and the queue list shows each
    file's choice. A target file size's quality guide is judged against the new size.
- **Decode on the GPU** (under Advanced): the graphics chip reads the source video, so the CPU only encodes,
  which speeds up x265, most with 4K sources. Before each file a few frames are test-decoded; if the GPU cannot
  decode that file, it is decoded on the CPU as before. The log says which was used. The setting is remembered
  and used from the next file that starts.
- `keepthelight_cli.exe` has the same options: `-scale 1080p` and `-hw-decode`.

## 1.4.3 - 2026-10-24

- **One window at a time.** Starting Keep the Light while it is already open brings the open window to the front
  instead of opening a second one (two windows would share, and fight over, the same remembered queue and
  settings). Videos dropped on `Keep the Light.exe` while it is open go to that window: one file into the form,
  several into the queue. If the open window does not respond, a new one opens after a few seconds.

## 1.4.2 - 2026-10-24

- The Keep the Light logo now appears at the top of the window, to the left of the name.

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
