package fingerprint

import (
	"path/filepath"
	"testing"
)

func TestEXIFFingerprinter(t *testing.T) {
	t.Parallel()
	fp := &EXIFFingerprinter{}
	for _, tc := range getTestCases(t, testDataDir, largeTestDataDir) {
		t.Run(filepath.Base(tc.Name), func(t *testing.T) {
			t.Parallel()
			if tc.Got.GetExif() == nil {
				tc.Got.Exif = &EXIFTestCase{}
			}
			if tc.Got.Exif.Skip {
				t.Skip()
			}
			fps, e := fp.Init(tc.SourceFile)
			if e != nil {
				t.Fatalf("fp.Init(%v): %v", tc.SourceFile, e)
			}
			if fps == nil {
				tc.Got.Exif.Comment = []string{"No EXIF data"}
				maybeUpdateTestCase(t, tc)
				return
			}

			xfps := fps.(*exifFingerprinterState)
			photoID, isUnique, _ := xfps.getPhotoID()
			tc.Got.Exif.WantCameraModel = xfps.getCameraModel()
			tc.Got.Exif.WantCameraSerial = xfps.getCameraSerial()
			tc.Got.Exif.WantPhotoId = photoID
			tc.Got.Exif.WantUniquePhotoId = isUnique
			maybeUpdateTestCase(t, tc)
		})
	}
}
