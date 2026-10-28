package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Editing metadata: the file's title, date and description, each kept track's
// name, language and SDH/commentary flags, chapter names, and the cover image.
// It applies to any job, and to "metadata only" jobs, which copy every stream
// unchanged into a new file with the new information (FFmpeg cannot change a
// file in place).

// MetaEdit holds only what the user changed; everything else is kept.
type MetaEdit struct {
	Tags        map[string]string `json:"tags,omitempty"`   // title, date, description: set ("" removes)
	Tracks      []TrackMeta       `json:"tracks,omitempty"` // per source track
	Chapters    *ChapterEdit      `json:"chapters,omitempty"`
	Cover       string            `json:"cover,omitempty"`       // image to use as the cover (MKV)
	RemoveCover bool              `json:"removeCover,omitempty"` // leave the cover out
}

type TrackMeta struct {
	Kind       string  `json:"kind"` // "a" audio, "s" subtitle
	N          int     `json:"n"`    // source track number (1-based)
	Title      *string `json:"title,omitempty"`
	Lang       *string `json:"lang,omitempty"`
	SDH        *bool   `json:"sdh,omitempty"`
	Commentary *bool   `json:"commentary,omitempty"`
}

type ChapterEdit struct {
	Remove bool              `json:"remove,omitempty"`
	Names  map[string]string `json:"names,omitempty"` // chapter number -> name
}

var metaTagKeys = map[string]bool{"title": true, "date": true, "description": true}

func (m *MetaEdit) empty() bool {
	return m == nil || (len(m.Tags) == 0 && len(m.Tracks) == 0 && m.Chapters == nil && m.Cover == "" && !m.RemoveCover)
}

// parseMetaJSON reads -meta-json: a JSON object, or @file.
func parseMetaJSON(s string) (*MetaEdit, error) {
	if s == "" {
		return nil, nil
	}
	data := []byte(s)
	if strings.HasPrefix(s, "@") {
		b, err := os.ReadFile(s[1:])
		if err != nil {
			return nil, err
		}
		data = b
	}
	var m MetaEdit
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("bad metadata: %w", err)
	}
	return &m, m.validate()
}

func (m *MetaEdit) validate() error {
	if m == nil {
		return nil
	}
	for k := range m.Tags {
		if !metaTagKeys[k] {
			return fmt.Errorf("unknown metadata field %q (title, date, description)", k)
		}
	}
	for _, t := range m.Tracks {
		if (t.Kind != "a" && t.Kind != "s") || t.N < 1 {
			return fmt.Errorf("bad track in metadata")
		}
	}
	if m.Cover != "" {
		switch strings.ToLower(filepath.Ext(m.Cover)) {
		case ".jpg", ".jpeg", ".png":
		default:
			return fmt.Errorf("the cover must be a .jpg or .png image")
		}
		if st, err := os.Stat(m.Cover); err != nil || st.IsDir() {
			return fmt.Errorf("cover image not found: %s", m.Cover)
		}
	}
	if m.Chapters != nil {
		for k := range m.Chapters.Names {
			if n, err := strconv.Atoi(k); err != nil || n < 1 {
				return fmt.Errorf("bad chapter number %q", k)
			}
		}
	}
	return nil
}

func (m *MetaEdit) track(kind string, n int) *TrackMeta {
	if m == nil {
		return nil
	}
	for i := range m.Tracks {
		if m.Tracks[i].Kind == kind && m.Tracks[i].N == n {
			return &m.Tracks[i]
		}
	}
	return nil
}

// trackMetaArgs writes one output track's name and language, as edited.
func trackMetaArgs(tm *TrackMeta, spec string) []string {
	if tm == nil {
		return nil
	}
	var a []string
	if tm.Title != nil {
		a = append(a, "-metadata:s:"+spec, "title="+*tm.Title)
	}
	if tm.Lang != nil {
		l := langKey(*tm.Lang)
		if l == "" {
			l = "und"
		}
		a = append(a, "-metadata:s:"+spec, "language="+l)
	}
	return a
}

// withFlags returns the track's flags with SDH / commentary set as edited.
func withFlags(src map[string]bool, tm *TrackMeta) map[string]bool {
	if tm == nil || (tm.SDH == nil && tm.Commentary == nil) {
		return src
	}
	d := map[string]bool{}
	for k, v := range src {
		d[k] = v
	}
	if tm.SDH != nil {
		d["hearing_impaired"] = *tm.SDH
	}
	if tm.Commentary != nil {
		d["comment"] = *tm.Commentary
	}
	return d
}

// ---------- the output's metadata, chapters and attachments ----------

// metaPrep is what the final output command needs for metadata: extra inputs
// (a chapter file) and output options. Files it writes go into dir.
type metaPrep struct {
	Inputs []string
	Args   []string
}

// prepareMeta builds the metadata part of the command that writes the output.
// src is the input number of the original file, next the first free input number.
func prepareMeta(r *runner, cfg *Config, plan *Plan, src, next int, mp4 bool, dir string) (*metaPrep, error) {
	m, t := cfg.Meta, plan.Src
	if t == nil {
		t = &Tracks{}
	}
	mp := &metaPrep{}
	// file tags: from the original (not from an intermediate file), then the edits
	mp.Args = append(mp.Args, "-map_metadata", strconv.Itoa(src))
	if m != nil {
		keys := make([]string, 0, len(m.Tags))
		for k := range m.Tags {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			mp.Args = append(mp.Args, "-metadata", k+"="+m.Tags[k])
		}
	}

	// chapters
	switch {
	case m != nil && m.Chapters != nil && m.Chapters.Remove:
		mp.Args = append(mp.Args, "-map_chapters", "-1")
	case m != nil && m.Chapters != nil && len(m.Chapters.Names) > 0 && len(t.Chapters) > 0:
		f := filepath.Join(dir, "chapters.txt")
		if !r.dry {
			if err := os.WriteFile(f, []byte(ffmetadataChapters(t.Chapters, m.Chapters.Names)), 0o644); err != nil {
				return nil, fmt.Errorf("writing the chapter names: %w", err)
			}
		}
		mp.Inputs = append(mp.Inputs, "-f", "ffmetadata", "-i", f)
		mp.Args = append(mp.Args, "-map_chapters", strconv.Itoa(next))
	default:
		mp.Args = append(mp.Args, "-map_chapters", strconv.Itoa(src))
	}

	// attachments (MKV only): fonts and the like as they are, covers re-attached
	if mp4 {
		return mp, nil
	}
	mp.Args = append(mp.Args, "-map", fmt.Sprintf("%d:t?", src), "-c:t", "copy")
	n := t.Attachments
	attach := func(file, name, mime string) {
		mp.Args = append(mp.Args, "-attach", file, fmt.Sprintf("-metadata:s:t:%d", n), "mimetype="+mime,
			fmt.Sprintf("-metadata:s:t:%d", n), "filename="+name)
		n++
	}
	replace := m != nil && (m.Cover != "" || m.RemoveCover)
	if !replace {
		for i, c := range t.Covers { // FFmpeg reads these as pictures: save each, attach it again
			ext := coverExt(c)
			file := filepath.Join(dir, fmt.Sprintf("cover%d%s", i, ext))
			if err := r.run(cfg.Ffmpeg, "-hide_banner", "-v", "error", "-y", "-i", cfg.Input, "-map", fmt.Sprintf("0:v:%d", c.VIndex),
				"-c", "copy", "-frames:v", "1", "-f", "image2", file); err != nil {
				return nil, fmt.Errorf("copying the cover image: %w", err)
			}
			name, mime := c.Filename, c.Mime
			if name == "" {
				name = "cover" + ext
			}
			if mime == "" {
				mime = mimeFor(ext)
			}
			attach(file, name, mime)
		}
	}
	if m != nil && m.Cover != "" {
		ext := strings.ToLower(filepath.Ext(m.Cover))
		if ext == ".jpeg" {
			ext = ".jpg"
		}
		attach(m.Cover, "cover"+ext, mimeFor(ext))
	}
	return mp, nil
}

func coverExt(c cover) string {
	switch {
	case c.Codec == "png" || strings.Contains(c.Mime, "png"):
		return ".png"
	}
	return ".jpg"
}

func mimeFor(ext string) string {
	if ext == ".png" {
		return "image/png"
	}
	return "image/jpeg"
}

// ffmetadataChapters writes the chapters with their new names as an FFmpeg
// metadata file (times are kept exactly).
func ffmetadataChapters(chs []chapter, names map[string]string) string {
	esc := strings.NewReplacer(`\`, `\\`, "=", `\=`, ";", `\;`, "#", `\#`, "\n", `\`+"\n")
	var b strings.Builder
	b.WriteString(";FFMETADATA1\n")
	for _, c := range chs {
		title := c.Title
		if n, ok := names[strconv.Itoa(c.N)]; ok {
			title = n
		}
		fmt.Fprintf(&b, "[CHAPTER]\nTIMEBASE=1/1000000\nSTART=%d\nEND=%d\n", int64(math.Round(c.Start*1e6)), int64(math.Round(c.End*1e6)))
		if title != "" {
			fmt.Fprintf(&b, "title=%s\n", esc.Replace(title))
		}
	}
	return b.String()
}

// ---------- replacing the original ----------

// verifyCopy checks that a metadata-only result is complete before the
// original is given up: it must read as video, with the same length.
func verifyCopy(ctx context.Context, ffprobe, original, result string) error {
	a, err := probeFile(ctx, ffprobe, original)
	if err != nil {
		return fmt.Errorf("could not read the original to compare: %w", err)
	}
	b, err := probeFile(ctx, ffprobe, result)
	if err != nil {
		return fmt.Errorf("the new file cannot be read: %w", err)
	}
	if b.Codec == "" || b.Width == 0 {
		return fmt.Errorf("the new file has no video")
	}
	if tol := max(1.0, a.Duration*0.002); a.Duration > 0 && math.Abs(a.Duration-b.Duration) > tol {
		return fmt.Errorf("the new file is %.1f s long, the original %.1f s", b.Duration, a.Duration)
	}
	return nil
}

// replaceOriginal puts the finished file (newFile) in the original's place:
// the original goes to the Recycle Bin (or, on a drive without one, is renamed
// "... (original)"), and the new file takes the name final. It reports what
// was done.
func replaceOriginal(ctx context.Context, ffprobe, original, newFile, final string) ([]string, error) {
	if err := verifyCopy(ctx, ffprobe, original, newFile); err != nil {
		return nil, fmt.Errorf("the original was NOT replaced: %w. The new file is kept as %s", err, newFile)
	}
	if !samePath(final, original) && fileExists(final) {
		return nil, fmt.Errorf("the original was NOT replaced: %s already exists. The new file is kept as %s", final, newFile)
	}
	where, err := disposeOriginal(original)
	if err != nil {
		return nil, fmt.Errorf("the original was NOT replaced, because it could not be moved (%v). The new file is kept as %s", err, newFile)
	}
	if err := renameFile(newFile, final); err != nil {
		return nil, fmt.Errorf("the original was %s, but the new file could not be renamed to %s: %v. It is %s", where, final, err, newFile)
	}
	return []string{"The original was " + where + ".", "Saved as " + final}, nil
}

// disposeOriginal moves the original to the Recycle Bin when its drive has
// one; otherwise (network drives, USB sticks) it is renamed, never deleted.
func disposeOriginal(path string) (string, error) {
	if hasRecycleBin(path) {
		if err := recycle(path); err == nil {
			return "moved to the Recycle Bin", nil
		}
	}
	ext := filepath.Ext(path)
	kept := strings.TrimSuffix(path, ext) + " (original)" + ext
	for i := 2; fileExists(kept); i++ {
		kept = fmt.Sprintf("%s (original %d)%s", strings.TrimSuffix(path, ext), i, ext)
	}
	if err := renameFile(path, kept); err != nil {
		return "", err
	}
	return "renamed to " + filepath.Base(kept) + " (this drive has no Recycle Bin, so it was not deleted)", nil
}

func samePath(a, b string) bool {
	a1, e1 := filepath.Abs(a)
	b1, e2 := filepath.Abs(b)
	return e1 == nil && e2 == nil && strings.EqualFold(a1, b1)
}

// replacedName is where a metadata-only result goes when it replaces the
// original: the original's folder and name, with the chosen format's extension.
func replacedName(input, ext string) string {
	if ext == "" {
		ext = filepath.Ext(input)
	}
	return strings.TrimSuffix(input, filepath.Ext(input)) + ext
}

// ---------- disk space ----------

// volumeKey names the drive a path is on ("D:", "\\server\share", or "/" elsewhere).
func volumeKey(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if v := filepath.VolumeName(abs); v != "" {
		return strings.ToUpper(v)
	}
	return string(filepath.Separator)
}

// metaNeedsFiles: the output command needs files written first (a chapter
// list, or covers to re-attach), so a temp folder is used.
func metaNeedsFiles(cfg *Config, plan *Plan) bool {
	if isMP4(cfg.Output) && (cfg.Meta == nil || cfg.Meta.Chapters == nil) {
		return false
	}
	m := cfg.Meta
	if m != nil && m.Chapters != nil && !m.Chapters.Remove && len(m.Chapters.Names) > 0 {
		return true
	}
	replace := m != nil && (m.Cover != "" || m.RemoveCover)
	return plan.Src != nil && len(plan.Src.Covers) > 0 && !replace && !isMP4(cfg.Output)
}

// metaInfo is what the window shows in its Metadata section.
func metaInfo(t *Tracks) map[string]any {
	chs := []map[string]any{}
	for _, c := range t.Chapters {
		chs = append(chs, map[string]any{"n": c.N, "start": c.Start, "title": c.Title})
	}
	tags := map[string]string{}
	for k := range metaTagKeys {
		tags[k] = t.Tags[k]
	}
	if tags["description"] == "" {
		tags["description"] = t.Tags["comment"]
	}
	cov := ""
	if len(t.Covers) > 0 {
		cov = t.Covers[0].Filename
		if cov == "" {
			cov = "cover image"
		}
	}
	return map[string]any{"tags": tags, "chapters": chs, "cover": cov}
}
