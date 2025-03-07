package fingerprint

import (
	"path/filepath"
	"testing"
)

func TestVideoPHashFingerprinter(t *testing.T) {
	t.Parallel()
	fp := &VideoPHashFingerprinter{}
	for _, tc := range getTestCases(t, testDataDir, largeTestDataDir) {
		t.Run(filepath.Base(tc.Name), func(t *testing.T) {
			t.Parallel()
			if tc.Got.GetVideoPhash() == nil {
				tc.Got.VideoPhash = &VideoPHashTestCase{}
			}
			tc.Got.VideoPhash.WantRicopHash = ""
			if tc.Got.VideoPhash.Skip {
				t.Skip()
			}
			fps, e := fp.Init(tc.SourceFile)
			if e != nil {
				t.Fatalf("fp.Init(%v): %v", tc.SourceFile, e)
			}
			if fps == nil {
				tc.Got.VideoPhash.Comment = []string{"No video data"}
				return
			}
			vpfps := fps.(*videoPHashFingerprinterState)
			ricopHash, err := vpfps.GetRicop()
			if err != nil {
				t.Fatalf("getRicop(%v): %v", tc.SourceFile, err)
			}
			if ricopHash == NoFingerprint {
				tc.Got.VideoPhash.Comment = []string{"No video data"}
			}
			tc.Got.VideoPhash.WantRicopHash = ricopHash.Hash
		})
		maybeUpdateTestCase(t, tc)
	}
}
