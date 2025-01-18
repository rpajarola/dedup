package fingerprint

import (
	"path/filepath"
	"testing"
)

func TestEXIFFingerprinter(t *testing.T) {
	fp := &EXIFFingerprinter{}
	for _, tc := range getTestCases(t, testDataDir, largeTestDataDir) {
		t.Run(filepath.Base(tc.Name), func(t *testing.T) {
			if tc.Got.GetExif() == nil {
				tc.Got.Exif = &EXIFTestCase{}
			}
			if tc.Got.Exif.Skip {
				t.Skip()
			}
			if e := fp.Init(tc.SourceFile); e != nil {
				t.Fatalf("fp.Init(%v): %v", tc.SourceFile, e)
			}

			if fp.xf == nil {
				tc.Got.Exif.Comment = []string{"No EXIF data"}
			} else {
				photoID, isUnique, _ := fp.getPhotoID()
				tc.Got.Exif.WantCameraModel = fp.getCameraModel()
				tc.Got.Exif.WantCameraSerial = fp.getCameraSerial()
				tc.Got.Exif.WantPhotoId = photoID
				tc.Got.Exif.WantUniquePhotoId = isUnique
			}
			maybeUpdateTestCase(t, tc)
		})
	}
}
