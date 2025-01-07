package fingerprint

//go:generate protoc --go_out=. --go_opt=paths=source_relative fingerprint_test.proto

import (
	"path/filepath"
	"testing"
)

func TestEXIFFingerprinter(t *testing.T) {
	fp := &EXIFFingerprinter{}
	for _, tc := range getTestCases(t) {
		t.Run(filepath.Base(tc.Name), func(t *testing.T) {
			if tc.Exif.Skip {
				t.Skip()
			}

			if e := fp.Init(tc.SourceFile); e != nil {
				t.Fatalf("fp.Init(%v): %v", tc.SourceFile, e)
			}

			if fp.getCameraModel() != tc.Exif.WantCameraModel {
				t.Errorf("unexpected camera model, got %q, want %q", fp.getCameraModel(), tc.Exif.WantCameraModel)
			}
			if fp.getCameraSerial() != tc.Exif.WantCameraSerial {
				t.Errorf("unexpected camera serial, got %q, want %q", fp.getCameraSerial(), tc.Exif.WantCameraSerial)
			}
			photoID, isUnique, _ := fp.getPhotoID()
			if photoID != tc.Exif.WantPhotoId {
				t.Errorf("unexpected photo ID, got %q, want %q", photoID, tc.Exif.WantPhotoId)
			}
			if isUnique != tc.Exif.WantUniquePhotoId {
				t.Errorf("unexpected photo ID uniqueness, got %v, want %v", isUnique, tc.Exif.WantUniquePhotoId)
			}
		})
	}
}
