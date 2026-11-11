package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
)

// ---------- picture and sound stored together ----------
//
// A video file holds the picture and the sound in small pieces, and players
// expect pieces that play at the same moment to sit near each other in the
// file ("interleaved"). If the picture is stored far behind the sound, a
// player that streams the file (a Plex client, an Android TV box) has to read
// a long way ahead to find the first picture: many give up and show a black
// screen while the sound plays. Keep the Light checks each new file, and
// fixes one that is badly stored by copying it again with the pieces in order
// (a remux: nothing is re-encoded, so there is no loss of quality).

// interleaveLimit is how far (seconds) picture and sound may be apart near
// the start of the file before it counts as badly stored. Well-made files are
// within a second.
const interleaveLimit = 10.0

// interleaveGap reads the first pieces of a file and returns how far apart
// (seconds) the picture and the sound are there. A file without sound, or
// without a picture, returns 0.
func interleaveGap(ctx context.Context, ffprobe, path string) (float64, error) {
	out, err := toolCommand(ctx, ffprobe, "-v", "error", "-read_intervals", "%+15", // the first 15 s (by time, so a badly stored file is read only as far as its sound reaches 15 s)
		"-show_entries", "stream=index,codec_type:stream_disposition=attached_pic:packet=stream_index,pts_time,dts_time",
		"-of", "json", path).Output()
	if err != nil {
		return 0, fmt.Errorf("could not read the file: %v", err)
	}
	return parseInterleave(out)
}

func parseInterleave(data []byte) (float64, error) {
	var j struct {
		Streams []struct {
			Index       int            `json:"index"`
			CodecType   string         `json:"codec_type"`
			Disposition map[string]int `json:"disposition"`
		} `json:"streams"`
		Packets []struct {
			Stream int    `json:"stream_index"`
			PTS    string `json:"pts_time"`
			DTS    string `json:"dts_time"`
		} `json:"packets"`
	}
	if err := json.Unmarshal(data, &j); err != nil {
		return 0, err
	}
	video, audio := -1, map[int]bool{}
	for _, s := range j.Streams {
		switch {
		case s.CodecType == "video" && s.Disposition["attached_pic"] == 0 && video < 0:
			video = s.Index
		case s.CodecType == "audio":
			audio[s.Index] = true
		}
	}
	if video < 0 || len(audio) == 0 {
		return 0, nil
	}
	// in the order the pieces are stored: the furthest each has reached so far
	vMax, aMax, gap := 0.0, math.Inf(-1), 0.0
	for _, p := range j.Packets {
		t, err := strconv.ParseFloat(p.PTS, 64)
		if err != nil {
			if t, err = strconv.ParseFloat(p.DTS, 64); err != nil {
				continue
			}
		}
		switch {
		case p.Stream == video:
			vMax = math.Max(vMax, t)
		case audio[p.Stream]:
			aMax = math.Max(aMax, t)
		default:
			continue
		}
		if !math.IsInf(aMax, -1) {
			gap = math.Max(gap, math.Abs(aMax-vMax))
		}
	}
	if math.IsInf(aMax, -1) { // no sound among the first pieces: it is all picture
		return vMax, nil
	}
	return gap, nil
}

// badInterleave reports whether a file's picture and sound are stored far
// apart (and how far); a file that cannot be read is not reported.
func badInterleave(ctx context.Context, ffprobe, path string) (bool, float64) {
	gap, err := interleaveGap(ctx, ffprobe, path)
	return err == nil && gap > interleaveLimit, gap
}

// remuxInterleaved copies in to out with picture and sound stored in order.
// Nothing is re-encoded. The file is read twice (picture from one reading,
// everything else from the other), so FFmpeg has every piece's time from the
// start; covers are saved and attached again (FFmpeg reads them as pictures).
func remuxInterleaved(ctx context.Context, ffmpeg, ffprobe, in, out, work string) error {
	t, err := probeTracks(ctx, ffprobe, in)
	if err != nil {
		return err
	}
	mp4 := isMP4(out)
	args := []string{"-hide_banner", "-v", "error", "-y", "-i", in, "-i", in,
		"-map", "0:V:0", "-map", "1:a?", "-map", "1:s?"}
	if !mp4 {
		args = append(args, "-map", "1:t?")
	}
	args = append(args, "-map_metadata", "1", "-map_chapters", "1", "-c", "copy")
	if !mp4 {
		n := t.Attachments
		for i, c := range t.Covers {
			ext := coverExt(c)
			file := filepath.Join(work, fmt.Sprintf("ktl-cover%d%s", i, ext))
			if b, err := toolCommand(ctx, ffmpeg, "-hide_banner", "-v", "error", "-y", "-i", in, "-map", fmt.Sprintf("0:v:%d", c.VIndex),
				"-c", "copy", "-frames:v", "1", "-f", "image2", file).CombinedOutput(); err != nil {
				return fmt.Errorf("copying the cover image failed: %v %s", err, strings.TrimSpace(string(b)))
			}
			defer os.Remove(file)
			name, mime := c.Filename, c.Mime
			if name == "" {
				name = "cover" + ext
			}
			if mime == "" {
				mime = mimeFor(ext)
			}
			args = append(args, "-attach", file, fmt.Sprintf("-metadata:s:t:%d", n), "mimetype="+mime,
				fmt.Sprintf("-metadata:s:t:%d", n), "filename="+name)
			n++
		}
		args = append(args, "-default_mode", "passthrough")
	} else {
		args = append(args, "-movflags", "+faststart+use_metadata_tags")
		if tag := videoTag(ctx, ffprobe, in); tag == "dvh1" || tag == "hvc1" || tag == "dvhe" || tag == "hev1" {
			args = append(args, "-tag:v", tag)
		}
	}
	args = append(args, "-max_interleave_delta", "0", out)
	if b, err := toolCommand(ctx, ffmpeg, args...).CombinedOutput(); err != nil {
		_ = os.Remove(out)
		msg := strings.TrimSpace(string(b))
		if len(msg) > 400 {
			msg = msg[len(msg)-400:]
		}
		return fmt.Errorf("the remux failed: %v %s", err, msg)
	}
	return nil
}

// videoTag is the main picture's codec tag in an MP4 ("hvc1", "dvh1").
func videoTag(ctx context.Context, ffprobe, path string) string {
	out, err := toolCommand(ctx, ffprobe, "-v", "error", "-select_streams", "V:0", "-show_entries", "stream=codec_tag_string",
		"-of", "default=nw=1:nk=1", path).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// fixingName is the temporary name of a file being remuxed (the library scan skips ".partial" files).
func fixingName(path string) string {
	ext := filepath.Ext(path)
	return strings.TrimSuffix(path, ext) + ".partial" + ext
}

// fixInterleaveNew checks a file Keep the Light has just made and, if its
// picture and sound are stored far apart, remuxes it in place (the new file
// is only replaced once the remux has been checked).
func fixInterleaveNew(ctx context.Context, ffmpeg, ffprobe, path, work string) {
	bad, gap := badInterleave(ctx, ffprobe, path)
	if !bad {
		return
	}
	fmt.Printf("\nPicture and sound are stored %.0f s apart in the new file: streaming players could show a black picture.\nRemuxing it with them in order (no re-encoding)\n", gap)
	tmp := fixingName(path)
	if err := remuxInterleaved(ctx, ffmpeg, ffprobe, path, tmp, work); err != nil {
		fmt.Println("WARNING:", err, "- the file is kept as it is")
		return
	}
	if err := verifyCopy(ctx, ffprobe, path, tmp); err != nil {
		_ = os.Remove(tmp)
		fmt.Println("WARNING: the remuxed file did not check out (" + err.Error() + "); the file is kept as it is")
		return
	}
	if still, g := badInterleave(ctx, ffprobe, tmp); still {
		_ = os.Remove(tmp)
		fmt.Printf("WARNING: the remux did not help (still %.0f s apart); the file is kept as it is\n", g)
		return
	}
	if err := renameFile(tmp, path); err != nil {
		_ = os.Remove(tmp)
		fmt.Println("WARNING: the remuxed file could not replace the new file:", err)
		return
	}
	fmt.Println("Fixed: picture and sound are now stored together.")
}

// fixInterleaveFile fixes a file that is already in the library: the remux
// is checked, then the old copy goes to the Recycle Bin (or is renamed
// "(original)" on a drive without one) and the remux takes its name.
func fixInterleaveFile(ctx context.Context, ffmpeg, ffprobe, path string) ([]string, error) {
	bad, gap := badInterleave(ctx, ffprobe, path)
	if !bad {
		return []string{"Picture and sound are already stored together: nothing to fix."}, nil
	}
	tmp := fixingName(path)
	if err := remuxInterleaved(ctx, ffmpeg, ffprobe, path, tmp, os.TempDir()); err != nil {
		return nil, err
	}
	if still, g := badInterleave(ctx, ffprobe, tmp); still {
		_ = os.Remove(tmp)
		return nil, fmt.Errorf("the remux did not help (still %.0f s apart); the file is unchanged", g)
	}
	lines, err := replaceOriginal(ctx, ffprobe, path, tmp, path)
	if err != nil {
		return nil, err
	}
	return append([]string{fmt.Sprintf("Picture and sound were %.0f s apart; they are now stored together.", gap)}, lines...), nil
}

// runFixInterleave is "-fix-interleave FILE": check one file and fix it if needed.
func runFixInterleave(path string) int {
	ffmpeg, err := need("ffmpeg", "")
	if err == nil {
		var ffprobe string
		if ffprobe, err = need("ffprobe", ""); err == nil {
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()
			var lines []string
			if lines, err = fixInterleaveFile(ctx, ffmpeg, ffprobe, path); err == nil {
				for _, l := range lines {
					fmt.Println(l)
				}
				return 0
			}
		}
	}
	fmt.Fprintln(os.Stderr, "ERROR:", err)
	return 1
}
