# Keep the Light

**Keep the Light** is a free Windows program that makes your video files smaller without losing the things that make
them look great on a modern TV. It is built for people who keep a movie and TV library on a **Plex** server, and works just as well for Jellyfin
and Emby.

Most high-end videos today use **HDR** (High Dynamic Range), which gives brighter highlights and richer colours.
Some also use **HDR10+** or **Dolby Vision**, which adjust the picture scene by scene. Many converting programs throw
this information away, and the picture ends up looking flat or washed out. Keep the Light keeps it.

It converts videos to **HEVC** (also called H.265), a modern format that often makes files half the size at the same
quality.

---

## New since version 1.27

- **Plex-style names.** After a TMDB match, the new file can be named the way Plex expects, with the details of the
  new file added: `Heat (1995).2160p.HEVC.DV.{tmdb-949}.mkv` or `The Office (2005).S01E02.Diversity Day.1080p.HEVC.mkv`.
  Plex reads the `{tmdb-949}` part and the `S01E02` part to know exactly which film or episode it is. This is optional,
  and nothing is renamed until you have seen and approved the names.
- **Track details from track names.** Many files have a track called "English" or "Director's Commentary" without
  the language or commentary mark set. Plex uses those marks to choose the right track, so Keep the Light fills in
  what the name says.
- **Track names in English.** A track named only in its own language, such as "日本語" or "Deutsch", is renamed in
  English, such as "Japanese AC-3 5.1", so you can read it in Plex.
- **Track names for a whole season.** Rename the tracks of one episode in the queue, and Keep the Light offers to
  give the other episodes of that show the same names.
- **Tells Plex when new files are ready.** When the queue finishes, Plex is asked to look at the new files at once,
  instead of at its next scheduled scan.
- **Marks converted files.** Every file Keep the Light converts carries a small note inside it. You are warned
  before converting a file twice (which costs quality and saves little), and the Library scan shows what is done.
- **Schedule.** Start the queue at a set time, for example 01:00, and pause it in the morning to carry on the next
  night.
- **Watch folder.** Videos you copy into a chosen folder join the queue by themselves.
- **Logs of failed files are kept,** so a problem can be looked into later.
- **Uses the whole window.** Two columns on wide screens, a Text size setting, and the window remembers its size and
  place.
- **Plex names tab: a file renamer.** Rename files the way Plex expects straight away, without converting them: the
  queue's files, or any folder you add. Subtitle files beside them are renamed too, and **Undo last rename** puts
  them back.
- **Your own subtitle files.** Add an SRT file (or ASS/SSA) as an extra subtitle track. Files named after the video,
  like "Film.en.srt", are found and added by themselves, with their language read from the name.
- **DVD subtitles to SRT.** VobSub subtitles from DVDs, which are pictures, can now be read as text like Blu-ray
  subtitles, so Plex no longer has to burn them into the picture.
- **The file's bitrate is always shown,** with the video's own bitrate, and with Target bitrate how your target
  compares ("23% less than now").
- **Double episodes.** When TMDB counts one file as two episodes (a one-hour special), every later episode used to
  be looked up one number short. Keep the Light now spots such a file, renumbers the rest of the season to match
  TMDB, and names the double one `S05E09-E10` so Plex shows both. With Target file size, a double episode gets
  twice the size.

---

## What it can do

### Keep the picture looking right
- **Keeps HDR10, HLG, HDR10+ and Dolby Vision.** The extra picture information is copied into the new file, so it
  still looks right on an HDR TV.
- **Fixes a common Dolby Vision problem.** Many Plex players can't show Dolby Vision "profile 7" (used on 4K Blu-ray
  discs). Keep the Light converts it to "profile 8.1", which they can.
- **Converts Dolby Vision "profile 5"** (streaming downloads), whose colours look green and purple anywhere except on
  a Dolby Vision player. The new file is ordinary HDR10 that plays everywhere, with Dolby Vision 8.1 on top for Dolby
  Vision players. Needs the "full" FFmpeg build.
- **Checks its own work.** After converting, it looks at the new file to make sure the HDR information made it
  through.

### Choose the quality and size
- **Three ways to choose quality:**
  - **Constant quality:** pick a number, and every scene gets the same quality.
  - **Target bitrate:** pick how much data per second.
  - **Target file size:** say "make it 8 GB", and Keep the Light works out the rest. A double episode (named
    `S05E09-E10`, or marked as one on the Plex names tab) gets twice the size by itself.
- **Quality guide.** A plain-English note under the setting tells you if your choice is too low, about right, or
  more than you need.
- **30-second test.** Convert just 30 seconds first, and compare it with the same 30 seconds of the original before
  doing the whole film. The test is given a quality score.
- **Smaller resolutions.** Shrink the video to 4K, 1440p, 1080p, 720p, 576p or 480p. The shape of the picture is
  kept, and HDR and Dolby Vision are kept too.
- **Crop black bars (576p and 480p only).** Old shows are often stored inside a bigger frame with black bars around
  them. Keep the Light can find the bars and cut them off.
- **Noise reduction.** Cleans up grain and "fizz" in the picture. This often makes files smaller too.
- **Deinterlace.** Old TV shows and DVDs often have striped edges on anything that moves. Keep the Light spots
  this by looking at the picture itself, and fixes it, either with smooth motion like the original broadcast or at
  the standard frame rate.

### Fast, using your graphics card
- **Three encoders:** x265 (on the processor, best quality), NVIDIA NVENC, and Intel QuickSync (both on the graphics
  card, much faster).
- **Full-GPU mode.** With NVIDIA or Intel graphics, the whole job can run on the graphics card. Keep the Light tries
  this automatically and falls back to the normal way if it doesn't work.

### Sound and subtitles
- **Pick your tracks.** Keep, drop or convert each audio and subtitle track, for example to keep only English.
- **Rules for many files.** Set up rules once, such as "English audio only, keep forced subtitles", and they apply to
  every file you add.
- **Convert audio** to AC-3, E-AC-3, AAC, Opus or FLAC, and mix 7.1 down to 5.1 or stereo.
- **One track per language.** If a file has the same language twice, keep only the best or the most compatible one.
- **Add a compatible audio track.** If a film only has TrueHD or DTS sound, which many TVs can't play, Keep the Light
  adds a copy your TV can play and keeps the original.
- **Turn subtitles into SRT.** Styled subtitles (ASS) and picture subtitles (PGS from Blu-rays, VobSub from DVDs) can
  become plain SRT text, which every Plex player can show. Pictures are read with free text-recognition software
  (Tesseract).
- **Add your own subtitle files.** An SRT, ASS or SSA file becomes an extra track. Files named after the video are
  found by themselves; older text encodings are fixed so accents show correctly, and the timing can be moved.
- **Leaves out empty tracks.** Tracks with nothing in them can make a conversion fail, so they are skipped
  automatically.
- **Names untitled tracks** from what they are, for example "English AC-3 5.1". A track named only in its own
  language ("日本語") gets the English name.
- **Fills in missing track details** from track names: a track called "English Commentary" gets the English language
  and the commentary flag, which Plex uses to pick tracks.

### Movie and show information
- **Edit the details.** Change the title, date, description, cover picture, track names, languages and chapter names.
- **Look it up on TMDB.** Fill in the title, date, description and poster from The Movie Database (TMDB), for films
  and for TV episodes. This needs a free TMDB account.
- **Whole queues at once.** Look up every file in the queue in one go. When a name could mean several shows, a small
  **"Which one?"** window asks you once per show, and your answer fills in every episode.
- **Remembers your choices,** so next season's episodes match by themselves.
- **Plex-style names (optional).** Matched files can be named the way Plex expects, such as
  "The Office (2005).S01E02.Diversity Day.1080p.HEVC.mkv" or "Heat (1995).2160p.HEVC.DV.{tmdb-949}.mkv", with the new
  file's resolution and HDR kind. You see every proposed name before anything changes.
- **Plex names tab.** A file renamer: see every file's current name next to its Plex-style name, fix any that
  need it, and rename them all at once, with the subtitle files beside them. One click undoes the last batch.
- **"Metadata only" mode.** Change just the details without converting the video at all. This is fast, with no
  quality loss. It can also replace the original file, which goes to the Recycle Bin.

### Plex-friendly checks
- **Plex check.** Every file you add is checked for things that would make Plex convert it while you watch. Examples:
  sound your TV can't play, subtitles Plex has to "burn in", older video formats, and very high bitrates.
- **Knows your settings.** Each problem is marked as fixed by your settings (✓), still there (⚠), or not fixable.
  Most problems have a one-click **Fix** button.
- **Library scan.** Point it at a folder, for example your whole Movies folder. It shows how much space converting
  each file would save, and which files are likely to cause problems in Plex. Plex extras like trailers and
  featurettes can be skipped. **Sort by** biggest saving (the start), biggest file, highest bitrate, name, folder or
  Plex trouble spots first; the queue follows the order shown. The results are kept until the next scan, even after closing Keep the Light: they come
  back with the date of the scan, and files that have gone or changed since are marked.
- **Finished files are checked again,** to confirm they are clean, including that picture and sound are stored
  together in the file (if not, streaming players can show a black picture; Keep the Light fixes that on the spot).
- **Tells Plex** when new files are ready, so they appear straight away (set up once with your Plex server and token).
- **Marks converted files,** so you are warned before converting a file twice, and the Library scan shows what is done.

### The queue
- **Convert many files one after another.** Add files, a whole folder, or picks from the Library scan.
- **Progress for everything.** See the whole batch's progress and time left, plus the time left for each file and how
  long each one took.
- **Pause and Resume.** Freeze a conversion and carry on later from the same spot.
- **Stop after this file,** or stop right away.
- **Reorder files** with up, down, top and bottom buttons.
- **Change one file** in the queue without affecting the others, or copy one file's track choices and track names
  to similar files. Rename one episode's tracks, and the rest of the season can follow.
- **Remembers the queue** if you close Keep the Light.
- **Schedule:** start the queue at a set time (say 01:00), and pause it in the morning to carry on the next night.
- **Watch folder:** videos you copy into a chosen folder join the queue by themselves, with the profile you pick.
- **When finished:** play a sound, or put the PC to sleep or shut it down.
- **Space saved** is shown for each file, for each run, and as a running total.

### Made for real-world setups
- **Works with network drives and NAS boxes.** It can copy a file to your PC, convert it there, and copy the result
  back, which is faster and safer over a network.
- **Handles busy files.** If another program (like a backup app) is holding a file, it waits and tries again
  instead of failing.
- **Checks free space** before starting.
- **Saves the log of every failed file,** so problems can be looked into later.
- **Runs in the background politely.** "Low priority" keeps your PC responsive while it works.
- **Profiles.** Save your favourite settings under a name, such as "4K movies" or "Old TV shows", and use them with
  one click. Profiles can be exported and imported.
- **Command-line version** for scripts and automation, with the same features as the window.

### Easy to use
- **Four tabs:** Convert, Queue, Library scan and Plex names, with the Start button always at the bottom.
- **Uses the whole window.** On a wide screen the Convert tab shows its settings in two columns, so most of them fit
  without scrolling. A **Text size** setting makes everything bigger or smaller, and the window remembers its size
  and place.
- **Plain-English help** behind small **?** buttons, and clear warnings that explain what to do.
- **File pickers remember** the last folder you used.
- **Right-click copy and paste,** keyboard-friendly controls, and helpful screens when nothing is loaded yet.

---

## What you need
- **Windows 10 or 11.**
- **FFmpeg**, a free video toolkit. It has to be downloaded once; Keep the Light shows you where.
- **For Dolby Vision:** GPAC (free).
- **For turning picture subtitles into text:** Tesseract OCR (free).
- **For TMDB lookups:** a free TMDB account and API key.

Keep the Light tells you if anything is missing, with buttons that take you to the download pages. **Setup** below
explains each step.

*This product uses the TMDB API but is not endorsed or certified by TMDB.*

---

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
| FFmpeg | everything | Download `ffmpeg-release-full.7z` from [gyan.dev](https://www.gyan.dev/ffmpeg/builds/) and copy `ffmpeg.exe` and `ffprobe.exe` from its `bin` folder into `tools`. The smaller `ffmpeg-release-essentials.zip` works for everything except Dolby Vision profile 5, which needs the full build's libplacebo filter. |
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
- **Layout:** four tabs: **Convert** (the file and its settings), **Queue**, **Library scan** and **Plex names**. Start, Add to queue,
  Test 30 s and the progress bar stay in view at the bottom of the window; the main button says exactly what it will
  do ("Convert this file", or "Start queue (3 files)" when files are waiting; **Add to queue** is its own button).
  The Queue tab shows how many files are waiting. Settings for this PC (temp folder, priority, GPU
  decoding, working locally, TMDB key, sounds) are under **⚙ Settings** at the top; profiles never include them.
- **Window size:** the page grows with the window. When the window is wide (about 1200 points or more at the current
  text size), the Convert tab shows two columns: Files, the profile bar, Video and Advanced on the left; Audio,
  Subtitles and Metadata on the right. A narrower window shows one column, as before. The other tabs use the full
  width. **Text size** (⚙ Settings → Window, 90%–150%) makes everything bigger or smaller on top of Windows' own
  display scaling, so the window looks right on a 1080p or a 4K screen; Ctrl +, Ctrl − and Ctrl + mouse wheel do the
  same, and Ctrl 0 goes back to 100%. The window opens where it was last time, at the same size (or maximised).
  What the input file is (its name, HDR10, Dolby Vision, resolution, "HEVC 10-bit") shows right under it, with a
  short **Checks** list: problems first (⚠), then what is fine (✓; more of them open with a click). Hints that only
  explain a setting open from a small **?** next to its label. With no file chosen, a large box shows how to start.
  A track with no language gets a **Set language…** picker in its row. Short messages ("Imported 3 profiles") fade
  by themselves. Keyboard users see a clear outline on whatever has focus. The Audio and
  Subtitles rules for new files and the queue are in a box of their own under each file's tracks. A notice about
  optional tools (GPAC for Dolby Vision, say) is a single line (**Details** opens it) and can be hidden; Dolby
  Vision files still say so if a tool is missing. **Browse** dialogs open where you last were: videos, the output
  folder, the scan folder and the others each remember their own folder (a path already typed in the box wins).
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
  **Pause** freezes the running file and **Resume** carries on from the same spot (as long as Keep the Light stays
  open; the PC can sleep while paused). **Stop after this file** lets the running file finish and then stops the queue;
  **Stop queue** cancels the running file at once. ⤒ / ↑ / ↓ / ⤓ move a waiting file to the top, up, down or to the
  bottom.
  **When the queue is finished** (on the Queue tab) can put the PC to sleep or shut it down (with 60 seconds to cancel).
  Each file is a two-line row: its name and status, then what it is with small chips (⚠ 2 Plex, x265 CRF 18, 480p,
  SRT…); click a row for its full settings and Plex details.
  The Queue tab shows the whole batch's progress ("File 2 of 5 · 38% of the batch done", weighted by each film's
  length) and the time left for all of it; each file shows its own time left while it runs, an estimate while it
  waits (from the speed measured for similar files, or, for short files such as TV episodes, from how long the
  first one or two really took; "estimate after the first minutes" until there is one), and how
  long it took once it is done.
- **Low priority** (⚙ Settings): other programs come first, so the PC stays responsive while encoding; it
  still runs at full speed when the PC is otherwise idle. Remembered, and takes effect at once.
- **Crop black bars** (under Resolution, only for 576p and 480p): black bars around the picture (a 4:3 show stored in
  a 1920×1080 frame, say) are found by looking at five points of the film, cut, and the picture is then shrunk to
  fit (that show becomes about 640×480, with no bars). Nothing is cut if any part of the film fills more of the
  frame. Not for Dolby Vision files. Command line: `-crop`.
- **Resolution** (under Video): shrink the video to fit 4K, 1440p, 1080p, 720p, 576p or 480p, keeping its shape
  (a 3840x1600 film becomes 1920x800 at 1080p, or 854x356 at 480p) and its HDR10, HDR10+ and Dolby Vision data. A
  smaller picture encodes much faster (1080p is about a quarter of the work of 4K). 576p (fits 1024x576) and 480p
  (fits 854x480) are standard-definition sizes, for phones, tablets or saving the most space. Videos are never
  enlarged.
- **Deinterlace** (under Video): for interlaced video (old TV shows, DVDs, recordings), whose moving edges look
  striped ("combing"). Keep the Light looks at the picture itself, because such files are often labelled wrong, and
  works out the field order the same way. It checks standard-definition files, files marked interlaced and HD files
  with no marking; films are left alone. **Smooth motion** (the default) gives each half-picture its own frame
  (50/60 fps, the fluid look of the original broadcast); **Standard frame rate** keeps 25/30 fps; **Off** leaves it.
  The filter (bwdif) works in 10-bit, so it is safe for HDR. The window shows an **Interlaced** badge, and the log
  says what was done. Command line: `-deinterlace smooth|standard|off`.
- **Noise reduction** (under Video): Off / Light / Medium / Strong (fast, on the CPU) or High quality (on the GPU
  when the FFmpeg build and graphics driver allow it, through Vulkan or OpenCL; otherwise a slower CPU filter).
  Only filters that work in 10-bit are used, so HDR keeps its gradations; Dolby Vision is unaffected. Besides a
  cleaner picture, it usually makes files noticeably smaller. Light keeps film grain recognisable; Strong can look
  smoothed. The log says where "High quality" ran.
- **The file's bitrate** is shown in its card next to its length: the file's size and bitrate, and the video's own
  bitrate ("1h 6m 32s · 1.9 GB · 4.2 Mb/s (video 3.9 Mb/s)"). The video's figure comes from the file when it says,
  otherwise it is worked out from the file's bitrate less the audio ("about"). With **Target bitrate**, a line
  compares your target with it ("Your target of 3.0 Mb/s is 23% less than this video's own bitrate"), and warns
  when the target is not lower (nothing would be saved).
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
  normal way, and so do files marked "full range" (the graphics card's resizer cannot keep that). If the
  all-on-the-graphics-card path still fails partway, the file is tried again the normal way at once. If the chosen GPU encoder cannot work on this PC at all (NVENC without an NVIDIA card, QuickSync
  without Intel graphics), the window says so before you start.
- **Space saved:** each finished file shows its size before and after ("18.7 GB → 9.2 GB · saved 9.5 GB (51%)"),
  the Queue heading adds up the run, and a single running total ("4.2 TB saved in total") is kept for this PC.
  No history is stored.
- **Already converted:** every file whose video Keep the Light encodes gets a small tag inside it ("Keep the Light
  1.31.0 · x265 CRF 18 · 1920×1080"). The library scan shows such files as "✓ Already converted" (with no saving
  counted, and a filter to list them); on the Convert tab and in the queue they get an **Already converted** badge
  and a warning, because converting a converted file again costs quality and saves little.
- **Tell Plex when files are ready** (⚙ Settings → Plex): enter your Plex server's address and token and press
  **Check**. When a queue finishes, Plex is asked to scan the folders of the new files, so they show up straight
  away instead of at its next scheduled scan. Keep the Light works out which library holds them even when this PC
  and the server see the same drive by different paths (`W:\Movies` here, `D:\PlexRaid01\Movies` on the server).
  The token is kept in Windows' Credential Manager. If Plex cannot be reached, the queue still finishes normally.
- **Plex check** (automatic): every file added to Convert or the Queue is checked for the things likely to make
  Plex convert it while you watch (the same checks as the library scan, below), so problems show up without
  scanning the library. Because the check knows your settings, each problem is marked as **fixed** by them (for
  example styled subtitles that will become SRT), **still there** with the setting that would fix it ("Turn on
  'Add a compatible audio track when needed'"). The Queue tab says
  how many waiting files will still have problems ("1 of 10 waiting files will still have Plex problems").
  On the Convert tab, a problem that a setting fixes has a **Fix** button that changes it for you. Each
  finished file is checked again, and the result is shown in its row and log. Warnings never stop a conversion.
- **Library scan** (its own tab): choose a folder (for example `\\PlexRaid01\Movies`), with or
  without its subfolders, and Keep the Light reads every video in it (only the file information; nothing is changed).
  Each file shows its video (codec, resolution, HDR10 / HLG / Dolby Vision profile), bitrate, audio and size, an
  estimate of its size as HEVC and the space that would save, and the things likely to make Plex convert it while you
  watch: no AC-3, E-AC-3 or AAC audio (TrueHD, DTS or FLAC only), Dolby Vision profile 7, forced picture subtitles
  (PGS/VobSub, which Plex burns in), H.264 10-bit, VC-1 / MPEG-2, very high bitrates, and interlaced video (checked
  in SD files and files marked interlaced). Show all files, those worth
  converting (at least 300 MB and a quarter saved) or those with trouble spots; **Select worth converting**, then
  **Add selected to queue** with the current settings or a profile. The estimate assumes x265 at very good quality; a
  30-second test shows the real result. Files Keep the Light made (ending in the name tag) are left out, unless their
  picture and sound are stored far apart (a problem of Dolby Vision files made before version 1.35.1, which can play
  with a black picture on streaming players): those are listed with a **Fix (remux)** button, which copies the file
  again in the right order without re-encoding and puts the old copy in the Recycle Bin (or renames it "(original)" on
  a drive without one) once the new one is checked. From the command line: `keepthelight_cli -fix-interleave "file.mkv"`.
  Plex extras are left out too (**Skip Plex extras**, on by default): files in Behind The Scenes, Deleted Scenes, Featurettes,
  Interviews, Scenes, Shorts, Trailers and Other folders, files ending in -trailer, -featurette, -deleted and the
  like, and Plex Versions folders. The summary says how many were skipped.
- **Add a compatible audio track when needed** (Audio rules): if a file has no AC-3, E-AC-3 or AAC track (TrueHD,
  DTS or FLAC only), an E-AC-3 5.1 copy of the main track is added next to the original, so TVs and streamers that
  cannot play the original get sound without Plex converting it. Files that already have one are left as they are,
  and that track is kept beside the main one even with "One track per language" (TrueHD 7.1 Atmos and E-AC-3 5.1).
- **Copy tracks that are already compatible** (Audio rules, on to begin with): when the rules convert audio, an
  AC-3, E-AC-3 or AAC track that fits the channels and bitrate is copied as it is instead of converted again. The
  Convert tab marks it "already compatible"; you can still choose a conversion for it.
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
- **Same tracks for others** (on a queued file with its own track choices or track names): gives the other waiting
  files the same audio and subtitle choices and track names, by track position (which tracks are kept, converted or
  dropped, the default and forced flags, SRT conversion, and each track's name, language and commentary / SDH mark).
  Only files whose tracks match (same number of tracks, languages and formats) are changed; the others are listed
  and left as they were. Each file keeps its own title, description and cover.
- **Track names for a whole season:** when you **Edit** an episode in the queue, rename its tracks and save, Keep the
  Light asks "Use these track names for the other 9 waiting episodes of The Office?". Yes gives those episodes the
  same track names, languages, commentary / SDH marks and track choices; only episodes whose tracks are laid out the
  same way are changed, and the others are listed. "The same show" is worked out from the file names (the TMDB
  match, when there is one, gives the show's name). Each episode keeps its own title, description and picture.
- **Schedule** (Queue tab): start the queue at a set time (01:00, say), and optionally pause it at another (07:00)
  and carry on at the start time the next day. Keep the Light must be open and the PC awake.
- **Watch folder** (⚙ Settings): videos placed in a chosen folder join the queue by themselves, with the profile you
  pick, once they have finished copying (a file is added once, never twice). Optionally the queue starts at once.
- **Saved logs of failed files:** the full log of every failed file is saved automatically (the 50 newest are
  kept). **Open logs folder** is in ⚙ Settings, and a failed row has **Saved log**.
- **Clear the queue** removes every file from the queue (after asking); a file being transcoded goes on. **Clear
  finished** removes only the finished ones.
- **Sound when finished** (⚙ Settings, remembered): when a transcode or the whole queue is done, Keep the Light
  plays a sound (Notify by default) and flashes its taskbar button; a different sound (Critical stop) means a file
  failed. Choose any Windows sound, a sound from Windows' Media folder (Alarm 1-10 and so on), your own .wav, or none.
- **Right-click** text boxes for Cut, Copy, Paste and Select all, and any other text (logs, notes) for Copy and
  Select all. A finished or failed file in the queue has a **Copy log** button in its log.
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
- **A Dolby Vision label without Dolby Vision data** (a re-encode that kept the original's label): converted as the
  video it really is (often SDR), with a note.
- **Dolby Vision 5** (mostly streaming downloads): it has no ordinary picture underneath (its colours are in Dolby's
  own format, green and purple anywhere but on a Dolby Vision player), so its Dolby Vision data is applied first, on
  the graphics card (FFmpeg's libplacebo filter, in the "full" FFmpeg build), turning it into a normal HDR10 picture.
  That is encoded like any HDR10 file, with the HDR10 information read from the Dolby Vision data, and the Dolby
  Vision data becomes profile 8.1. The new file plays as HDR10 everywhere (Plex can convert it with the right
  colours) and as Dolby Vision on Dolby Vision players; **Dolby Vision: Drop (plain HDR10)** makes plain HDR10. The HDR10 picture is
  a close match to what a Dolby Vision player shows, not an exact one. With NVENC or QuickSync the HDR10
  mastering-display information cannot be written (x265 writes it); most TVs do not need it.

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
  was found. When a title has several possible matches (or none), a **Which one?** window asks once per title: the
  top 3 results with posters, a year to narrow it down, **This one** to fill in every waiting file with that title, or
  **Skip** (such rows keep a **Choose…** button). Matching a file with Edit works too: when you save it,
  every other waiting file with the same title in its name follows (each episode with its own season and episode),
  and the choice is remembered, so the next season matches straight away (**Forget remembered matches** in
  ⚙ Settings). Each title is searched once per lookup. Files whose title was already changed are skipped.
  This product uses the TMDB API but is not endorsed or certified by TMDB.
- **Plex-style names** (optional: **Plex-style names after a TMDB match**, under Files): a file matched on TMDB can
  be named the way Plex expects, with the new file's resolution, `HEVC` and its kind of HDR:
  - films: `Heat (1995).2160p.HEVC.DV.{tmdb-949}.mkv`
  - episodes: `The Office (2005).S01E02.Diversity Day.1080p.HEVC.mkv`

  The resolution is the new file's (a 4K film made 1080p says 1080p; a wide 1920×800 film counts as 1080p). The HDR
  kind is DV, HDR10+, HDR10 or HLG, and is left out for ordinary (SDR) video. There is no "_KTL" ending. Nothing is
  renamed until you have seen it: on the Convert tab the name goes into the Output box, where you can change it; in
  the queue each row shows its proposed name, and **Review names** lists every change with a tick box and **Apply**.
  A name that already exists, or that two files would share, is not used. Only the new files are named so; the
  originals keep their names.
- **Plex names** (its own tab): a file renamer for the queue's waiting files and for files added on the tab with
  **Add files…** or **Add folder…** (they do not join the queue). Each row shows the file's name, its Plex-style name
  (from its TMDB match, with the file's own resolution, video format and HDR kind, such as
  `Heat (1995).1080p.H264.{tmdb-949}`), whether it is ready, and the file's Plex check. You can type any name in the
  box; the extension never changes.
  - Warnings: a name another file already has, or that two files would get, is not used; an episode name without
    S01E02, or a film name without its (year), is marked ⚠ "Plex may not recognise this" (it can still be used).
  - **Look up on TMDB** finds the selected files (or all of them); titles with several matches are asked about in
    the same **Which one?** window as the queue's.
  - **Rename now** renames the files themselves straight away, after showing the list of changes. Subtitle files
    named after the video (`Film.en.srt`, `Film.forced.srt`) are renamed with it. A file in the queue follows its
    new name, and so does its converted file's name. A file another program has open is left alone and reported.
  - **Undo last rename** puts the last batch back. **Rename log** opens a record of every rename (old → new).
  - **Use for the converted files**: for queue files, the converted file gets the Plex name (described as the new
    HEVC file) and the original keeps its name, as **Review names** on the Queue tab does.
  - Folders are never renamed or created.
  - **Double episodes:** some shows have a one-hour episode that TMDB counts as two (Laverne & Shirley season 5:
    "We're in the Army, Now" is episodes 9 and 10), while your file is one, named S05E09. Every later file is then
    one number short of TMDB's. Keep the Light spots such a file (about twice as long as the season's others, where
    TMDB's episode and the next share a title or together match its length) and asks "Treat it as a double
    episode?". You can also choose it on any episode's row: **Double episode (two in this file)**, or for the
    reverse case **First half of a split episode** (one TMDB episode in two files). The later files of that season
    are then looked up one number on (or back); each row says what it is on TMDB. **Rename now** renames the whole
    season in one go, in an order that never overwrites a file, and **Undo last rename** puts it all back. The
    double file is named like `Laverne & Shirley (1976).S05E09-E10.We're in the Army, Now.480p.H264.mkv` (Plex
    shows both episodes), and the halves of a split one end in `-pt1` / `-pt2`.
  - A file already named `S05E09-E10` (or `S05E09E10`, `S05E09-10`) is looked up as both episodes: the title
    "… (Parts 1 & 2)", the first part's date and both descriptions.
  - **A second opinion from TVmaze:** for each season, Keep the Light also asks [TVmaze](https://www.tvmaze.com)
    (free, no key) how it numbers the episodes. When TMDB and TVmaze disagree (a different number of episodes, or
    different titles at the same numbers), the tab says so and lets you choose, with **Compare** showing both lists
    side by side. Plex can number a show either way (Edit the show in Plex → Advanced → Episode ordering), so pick
    the one your Plex uses; TVmaze's numbering is often the same as TVDB's. **Use TVmaze's numbering** keeps the
    files' own numbers and gives them TVmaze's episode titles (the show's name, year and poster still come from
    TMDB); a TVmaze episode about twice the usual length still gets twice the size with Target file size. The choice
    is remembered per show and season, and **Use TMDB's instead** undoes it. Turn the second opinion off in
    ⚙ Settings. Without TVmaze (offline, show not found), TMDB alone is used, as before.
  - **When TMDB has nothing,** TVmaze fills the file in by itself: a show TMDB does not have (name, year, episode
    titles, poster), or an episode it does not have. The searches (Edit, and "Which one?") list TVmaze's shows after
    TMDB's, marked **TVmaze**. A "TVmaze second opinion" line above the list says what TVmaze said about each
    season (agrees, numbers it differently, not found, not reachable).
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
- **Empty tracks** (a subtitle or audio track with nothing in it, which the file's own statistics show) are marked
  "EMPTY" and left out automatically: such a track can make FFmpeg fail partway through an encode. Tick one to keep
  it anyway.
- **Choosing tracks:** the Audio and Subtitles sections list every track of the file. Each audio track can be
  copied, converted (E-AC-3, AC-3, AAC, Opus, FLAC) or dropped; each subtitle track kept or dropped, with its
  forced flag. One audio track and (optionally) one subtitle track are marked as the default. **Set all tracks
  to…** changes every track at once.
- **Name untitled tracks** (Metadata, remembered, also for the queue and in profiles): tracks without a name get
  one from what they are as written: "English AC-3 5.1", "English TrueHD Atmos 7.1", "English SRT Forced". Names
  already set are kept; the Metadata section shows the planned names. The one exception: a name that is only the
  language in its own words ("日本語", "한국어", "Русский", "Deutsch", "Español") is replaced with the English name
  ("Japanese AC-3 5.1"), because Plex shows the name as it is. A name that says more ("日本語 コメンタリー") is
  kept, and so is one whose language the track does not have. Type a name in Metadata to keep another.
- **Fill in track details from their names** (a rule, on by default): a missing language, or a missing commentary,
  SDH or forced mark, is filled in from words in the track's name ("English", "Director's Commentary", "SDH",
  "日本語"…). Plex uses these marks to pick tracks. What the file already has is kept. Suggestions show in Metadata
  as "(name)", and the log lists what was filled in.
- **Language names:** tracks show the English name of their language for about 100 languages, including Cantonese
  (`yue`) and Mandarin (`cmn`), which are kept apart from each other.
- **Subtitles to SRT:** ASS/SSA (styled text), PGS (Blu-ray picture subtitles) and VobSub (DVD picture subtitles)
  tracks can be converted to plain SRT, which every Plex player shows without Plex burning them into the picture. Per track: **as is**, **→ SRT**
  (replaces it) or **as is + SRT** (an SRT copy next to it, named "… (SRT)"); for the queue, the rule **Convert
  ASS, PGS and VobSub to SRT**. Timings, language, default and forced flags are kept.
  - ASS/SSA: converted by FFmpeg, instantly. Fonts, colours, positions and effects are lost; plain dialogue
    converts cleanly, heavily styled tracks (karaoke, signs) may show doubled lines.
  - PGS: the pictures are read with **Tesseract OCR** (free, installed separately; see Setup), in the track's
    language when Tesseract has it, otherwise English. A film takes from seconds to a couple of minutes. Clean
    Blu-ray subtitles come out very accurately, but skim the result once: OCR can mistake l / I / 1, and
    italics are not marked.
  - VobSub (DVD): read the same way. DVD subtitles are small four-colour pictures, so each is drawn from the
    track's own colours and enlarged before Tesseract reads it. Accuracy is a little lower than for Blu-ray
    subtitles; skim the result.
  - In MP4, which cannot hold picture subtitles, a PGS or VobSub track can be kept as text; ASS becomes plain text
    there anyway.
- **Your own subtitle files** (Subtitles → **Add subtitle file…**): an SRT, ASS or SSA file is added as an extra
  track after the file's own. Each has a language, an optional name, **default**, **forced** and **SDH** marks, and
  a **timing** offset in seconds (+ shows them later) for a file that is out of step. The language and marks are
  read from the file name ("Film.en.srt", "Film.eng.forced.srt", "Film.English SDH.srt").
  - **Found by themselves:** subtitle files beside the video named after it are listed with a tick when the file
    is loaded, and files added to the queue get theirs too (⚙ Settings → **Add subtitle files named after each
    video**, on by default).
  - Each file is checked before converting: it must hold subtitles, and a file in an older text encoding
    (Windows-1252, UTF-16) is converted to UTF-8 so accents show correctly in Plex.
  - Works with **Copy video untouched** and **Metadata only** too, and in MP4 (as text). Command line:
    `-add-sub Film.en.srt`.
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
- Black bars are cropped only at 576p and 480p, and never in Dolby Vision files. Resizing keeps the frame count, so
  it never changes the frame rate (deinterlacing with smooth motion doubles it on purpose).
- GPU decoding uses the GPU that runs the main display (Direct3D 11). The decoded frames are copied back to the
  CPU, which costs a little but keeps everything else exactly as without it. For Dolby Vision profile 5 it is used
  only when a check on the file shows the converted picture is the same as with the processor decoding (48 frames
  compared); the colour conversion runs on the graphics card that matches the encoder.
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
