package main

import "testing"

func TestCopyCompatibleAudio(t *testing.T) {
	tr := &Tracks{Audio: []srcTrack{
		{N: 1, Codec: "truehd", Profile: "Dolby TrueHD + Dolby Atmos", Lang: "eng", Channels: 8, Disp: map[string]bool{"default": true}},
		{N: 2, Codec: "eac3", Lang: "eng", Channels: 6, BitRate: 640000},
		{N: 3, Codec: "ac3", Lang: "fre", Channels: 6, BitRate: 448000},
		{N: 4, Codec: "aac", Lang: "ger", Channels: 6, BitRate: 900000}, // bigger than the rule would make it
	}}
	codecs := func(a []outAudio) map[int]string {
		m := map[int]string{}
		for _, x := range a {
			m[x.Src.N] = x.Codec + "/" + x.Why
		}
		return m
	}
	// "Convert to E-AC-3": the compatible tracks that fit are copied
	audio, _, _, err := selectTracks(&Config{Audio: "eac3"}, tr, false)
	if err != nil {
		t.Fatal(err)
	}
	got := codecs(audio)
	if got[1] != "eac3/" || got[2] != "copy/already compatible" || got[3] != "copy/already compatible" || got[4] != "eac3/" {
		t.Errorf("copy compatible: %v", got)
	}
	// stereo: a 5.1 track does not fit, so it is converted
	audio, _, _, _ = selectTracks(&Config{Audio: "aac", AudioChannels: "stereo"}, tr, false)
	if got := codecs(audio); got[2] != "aac/" {
		t.Errorf("stereo: %v", got)
	}
	// the rule turned off: everything converted, as before
	audio, _, _, _ = selectTracks(&Config{Audio: "eac3", ConvertCompatible: true}, tr, false)
	if got := codecs(audio); got[2] != "eac3/" || got[3] != "eac3/" {
		t.Errorf("rule off: %v", got)
	}
	// the owner's example: keep TrueHD 7.1 Atmos as it is and the file's own E-AC-3 5.1 (no new one made)
	audio, _, _, _ = selectTracks(&Config{Audio: "copy", AudioLangs: []string{"eng"}, AudioOnePerLang: "best", AddCompatAudio: true}, tr, false)
	if got := codecs(audio); len(audio) != 2 || got[1] != "copy/" || got[2] != "copy/" {
		t.Errorf("best + compatible: %v (%d tracks)", got, len(audio))
	}
	// without the compatible rule, one per language keeps only the best
	audio, _, _, _ = selectTracks(&Config{Audio: "copy", AudioLangs: []string{"eng"}, AudioOnePerLang: "best"}, tr, false)
	if len(audio) != 1 || audio[0].Src.N != 1 {
		t.Errorf("best only: %+v", audio)
	}
	// no compatible track of its own: one is made from the main track, as before
	only := &Tracks{Audio: []srcTrack{{N: 1, Codec: "truehd", Lang: "eng", Channels: 8, Disp: map[string]bool{"default": true}}, {N: 2, Codec: "dts", Lang: "eng", Channels: 6}}}
	audio, _, _, _ = selectTracks(&Config{Audio: "copy", AudioOnePerLang: "best", AddCompatAudio: true}, only, false)
	if len(audio) != 2 || audio[1].Codec != "eac3" || audio[1].Src.N != 1 {
		t.Errorf("made from the main track: %+v", audio)
	}
}
