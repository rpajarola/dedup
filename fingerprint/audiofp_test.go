package fingerprint

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestAudioFingerprinter(t *testing.T) {
	t.Parallel()
	afp := &AudioFingerprinter{}
	for _, tc := range getTestCases(t, testDataDir) {
		t.Run(filepath.Base(tc.Name), func(t *testing.T) {
			t.Parallel()
			defer maybeUpdateTestCase(t, tc)
			if tc.Got.GetAudio() == nil {
				tc.Got.Audio = &AudioTestCase{}
			}
			tc.Got.Audio.WantChromaprint = ""
			tc.Got.Audio.WantSimhash = ""
			if tc.Got.Audio.Skip {
				t.Skip()
			}
			fps, err := afp.Init(tc.SourceFile)
			if err != nil {
				t.Fatalf("afp.Init(%v): %v", tc.SourceFile, err)
			}
			if fps == nil {
				tc.Got.Audio.Comment = []string{"No audio data"}
				return
			}
			defer fps.Cleanup()
			fp, err := fps.(*audioFingerprinterState).getChromaprint()
			if err != nil {
				t.Fatalf("getChromaprint(%v): %v", tc.SourceFile, err)
			}
			if len(fp) == 0 {
				tc.Got.Audio.Comment = []string{"No audio data"}
				return
			}
			tc.Got.Audio.WantChromaprint = chromaprintEncode(fp)
			tc.Got.Audio.WantSimhash = fmt.Sprintf("%08x", chromaprintSimHash(fp))
		})
	}
}

// TestChromaprintEncode checks the compressor and SimHash against values
// produced by libchromaprint's chromaprint_encode_fingerprint and
// chromaprint_hash_fingerprint.
func TestChromaprintEncode(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		fp          []uint32
		wantEncoded string
		wantSimHash uint32
	}{
		{nil, "AQAAAA", 0},
		{[]uint32{0}, "AQAAAQA", 0},
		{
			[]uint32{1, 0x80000000, 0xffffffff, 0x12345678, 0xdeadbeef, 0xdeadbeef, 7},
			"AQAAB0GOJEmSJEmSJEmSJEmCJM2kREsUJUiiUUmWLQtAJUoSRZGURAEY",
			0x9224166f,
		},
	} {
		if got := chromaprintEncode(tc.fp); got != tc.wantEncoded {
			t.Errorf("chromaprintEncode(%x) = %q, want %q", tc.fp, got, tc.wantEncoded)
		}
		if got := chromaprintSimHash(tc.fp); got != tc.wantSimHash {
			t.Errorf("chromaprintSimHash(%x) = %08x, want %08x", tc.fp, got, tc.wantSimHash)
		}
	}
}
