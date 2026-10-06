package fingerprint

import (
	"math/bits"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestVideoPHashFingerprinter(t *testing.T) {
	t.Parallel()
	fp := &VideoPHashFingerprinter{}
	for _, tc := range getTestCases(t, testDataDir) {
		t.Run(filepath.Base(tc.Name), func(t *testing.T) {
			t.Parallel()
			defer maybeUpdateTestCase(t, tc)
			if tc.Got.GetVideoPhash() == nil {
				tc.Got.VideoPhash = &VideoPHashTestCase{}
			}
			tc.Got.VideoPhash.WantSimhash = ""
			tc.Got.VideoPhash.Comment = withoutComment(tc.Got.VideoPhash.Comment, "No video data")
			if tc.Got.VideoPhash.Skip {
				t.Skip()
			}
			fps, e := fp.Init(tc.SourceFile)
			if e != nil {
				t.Fatalf("fp.Init(%v): %v", tc.SourceFile, e)
			}
			if fps == nil {
				tc.Got.VideoPhash.Comment = append(tc.Got.VideoPhash.Comment, "No video data")
				return
			}
			defer fps.Cleanup()
			hs, err := fps.(*videoPHashFingerprinterState).frameHashes()
			if err != nil {
				t.Fatalf("frameHashes(%v): %v", tc.SourceFile, err)
			}
			if len(hs) <= 1 {
				tc.Got.VideoPhash.Comment = append(tc.Got.VideoPhash.Comment, "No video data")
				return
			}
			tc.Got.VideoPhash.WantSimhash = videoSimHash(hs).Hash
		})
	}
}

// TestVideoSimHashEquivalence checks that VideoPHashSimHash puts
// re-encodes of the same clip within a small Hamming distance of each
// other, and different clips far apart.
func TestVideoSimHashEquivalence(t *testing.T) {
	t.Parallel()
	simhash := func(name string) uint64 {
		t.Helper()
		fn := filepath.Join(testDataDir, name)
		if _, err := os.Stat(fn); err != nil {
			t.Skipf("missing testdata: %v", err)
		}
		fps, err := (&VideoPHashFingerprinter{}).Init(fn)
		if err != nil || fps == nil {
			t.Fatalf("Init(%v) = %v, %v", fn, fps, err)
		}
		defer fps.Cleanup()
		hs, err := fps.(*videoPHashFingerprinterState).frameHashes()
		if err != nil {
			t.Fatalf("frameHashes(%v): %v", fn, err)
		}
		h, err := strconv.ParseUint(videoSimHash(hs).Hash, 16, 64)
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	groups := [][]string{
		// The same 5s clip as MPEG-4/AVI, MPEG-2/TS, H.264/MP4 (2
		// frames shorter) and a 4s, 640x360 GIF.
		{"BigBuckBunny.avi", "BigBuckBunny.ts", "BigBuckBunny.mp4", "BigBuckBunny.gif"},
		// The same 13s clip at two resolutions.
		{"sample_640x360.avi", "sample_960x540.avi"},
		// The same 28s clip at four resolutions.
		{"sample_1280x720.avi", "sample_1920x1080.avi", "sample_2560x1440.avi", "sample_3840x2160.avi"},
		{"druid_peak_trailer_2014.mp4"},
		{"computerchess5.flv"},
	}
	const maxSame, minDifferent = 8, 20
	hashes := make([][]uint64, len(groups))
	for i, g := range groups {
		for _, name := range g {
			hashes[i] = append(hashes[i], simhash(name))
		}
	}
	for i, g := range groups {
		for j := range g {
			for k := range g[:j] {
				if d := bits.OnesCount64(hashes[i][j] ^ hashes[i][k]); d > maxSame {
					t.Errorf("%v vs %v: distance %d, want <= %d (same clip)", g[j], g[k], d, maxSame)
				}
			}
			for i2, g2 := range groups[:i] {
				for k, name2 := range g2 {
					if d := bits.OnesCount64(hashes[i][j] ^ hashes[i2][k]); d < minDifferent {
						t.Errorf("%v vs %v: distance %d, want >= %d (different clips)", g[j], name2, d, minDifferent)
					}
				}
			}
		}
	}
}
