package fingerprint

import (
	"path/filepath"
	"testing"
	//"github.com/google/go-cmp/cmp"
	//"google.golang.org/protobuf/encoding/prototext"
	//"google.golang.org/protobuf/testing/protocmp"
)

func TestImgPHashFingerprinter(t *testing.T) {
	fp := &ImgPHashFingerprinter{}
	for _, tc := range getTestCases(t, testDataDir, largeTestDataDir) {
		t.Run(filepath.Base(tc.Name), func(t *testing.T) {
			if tc.Got.GetImgPhash() == nil {
				tc.Got.ImgPhash = &ImgPHashTestCase{}
			}
			if tc.Got.ImgPhash.Skip {
				t.Skip()
			}
			fps, e := fp.Init(tc.SourceFile)
			if e != nil {
				t.Fatalf("fp.Init(%v): %v", tc.SourceFile, e)
			}
			ipfps := fps.(*imgPHashFingerprinterState)
			azrHash, err := ipfps.getAzr()
			if err != nil {
				t.Fatalf("getAzr(%v): %v", tc.SourceFile, err)
			}
			nr90Hash, err := ipfps.getNr90()
			if err != nil {
				t.Fatalf("getNr90(%v): %v", tc.SourceFile, err)
			}

			if azrHash.Hash == "00000000" {
				tc.Got.ImgPhash.Comment = append(tc.Got.ImgPhash.Comment, "no image data")
				tc.Got.ImgPhash.Skip = true
				tc.Got.ImgPhash.WantAzrHash = ""
				tc.Got.ImgPhash.WantNr90Hash = ""
			} else {
				tc.Got.ImgPhash.WantAzrHash = azrHash.Hash
				tc.Got.ImgPhash.WantNr90Hash = nr90Hash.Hash
			}
			maybeUpdateTestCase(t, tc)
		})
	}
}
