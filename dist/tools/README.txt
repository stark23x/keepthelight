Keep the Light - tools folder
=============================

Keep the Light runs these helper programs:

  ffmpeg.exe, ffprobe.exe   NOT INCLUDED - needed for everything (see below)
  dovi_tool.exe             included - Dolby Vision
  hdr10plus_tool.exe        included - keeps HDR10+ metadata
  MP4Box (GPAC)             NOT INCLUDED - needed for Dolby Vision only (see below)
  Tesseract OCR             NOT INCLUDED - only to convert PGS subtitles to SRT: install it from
                            https://github.com/UB-Mannheim/tesseract/wiki (Keep the Light finds it)

Getting FFmpeg
--------------
  1. Go to https://www.gyan.dev/ffmpeg/builds/ and download "ffmpeg-release-full.7z".
     (Windows 11 opens .7z files; on Windows 10 use 7-Zip.) The smaller "ffmpeg-release-essentials.zip"
     works for everything except Dolby Vision profile 5, whose colours need the libplacebo filter that
     only the full build has. Any FFmpeg build that includes libx265 works.
  2. Open it, go into its "bin" folder, and copy ffmpeg.exe and ffprobe.exe into THIS folder.
  3. In Keep the Light, press "Check again" (or restart it).

Getting MP4Box (only for Dolby Vision files)
--------------------------------------------
  Install GPAC from https://gpac.io/downloads/ with its normal installer. Keep the Light finds it in
  its usual place (Program Files\GPAC) or on PATH. Copying mp4box.exe alone into this folder does
  NOT work: it needs the DLLs that come with it.

Why these two are not included: the Windows builds of FFmpeg and GPAC contain GPL-licensed code, and
passing them on would mean also providing the complete source code of everything in those builds.

Visual C++ Runtime
------------------
dovi_tool and hdr10plus_tool need the Microsoft Visual C++ Runtime. Most PCs already have it; if not,
Keep the Light says so and links to Microsoft's installer (https://aka.ms/vs/17/release/vc_redist.x64.exe).

Licences
--------
The included tools are open-source programs by quietvoid, redistributed unchanged under the MIT
licence. Their licence texts, exact versions and source links are in the "licenses" folder.
