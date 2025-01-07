package fingerprint

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testDataDir = "testcases"

type testCase struct {
	SourceFile string
	Skip       bool

	WantErr                bool
	WantCameraModel        string
	WantCameraSerial       string
	WantPhotoID            string
	WantUniquePhotoID      bool
	WantModelSerialPhotoID string
	WantQuality            int
}

func readTestCase(t *testing.T, fname string) testCase {
	t.Helper()

	raw, err := os.ReadFile(fname)
	if err != nil {
		t.Fatal(err)
	}
	var tc testCase
	if err := json.Unmarshal(raw, &tc); err != nil {
		t.Fatalf("json.Unmarshal(%v): %v", fname, err)
	}
	return tc
}

func TestEXIFFingerprinter(t *testing.T) {
	os.Chdir(testDataDir)
	f, err := os.Open(".")
	if err != nil {
		t.Fatalf("os.Open(%v): %v", testDataDir, err)
	}

	fnames, err := f.Readdirnames(0)
	if err != nil {
		t.Fatalf("Readdirnames(%v): %v", testDataDir, err)
	}

	fp := &EXIFFingerprinter{}
	for _, fname := range fnames {
		if !strings.HasSuffix(fname, ".json") {
			continue
		}
		tc := readTestCase(t, fname)
		if tc.Skip {
			continue
		}
		t.Run(filepath.Base(fname), func(t *testing.T) {

			if e := fp.Init(tc.SourceFile); e != nil {
				t.Fatalf("fp.Init(%v): %v", tc.SourceFile, e)
			}
			f, e := fp.getModelSerialPhotoID()
			gotErr := e != nil
			if gotErr != tc.WantErr {
				t.Fatalf("unexpected error: got %v (%v), want %v", gotErr, e, tc.WantErr)
			}
			if tc.WantModelSerialPhotoID != "" && f.Kind != "EXIFModelSerialPhotoID" {
				t.Errorf("unexpected Kind, got %q, want %q", f.Kind, "EXIFModelSerialPhotoID")
			}
			if fp.getCameraModel() != tc.WantCameraModel {
				t.Errorf("unexpected camera model, got %q, want %q", fp.getCameraModel(), tc.WantCameraModel)
			}
			if fp.getCameraSerial() != tc.WantCameraSerial {
				t.Errorf("unexpected camera serial, got %q, want %q", fp.getCameraSerial(), tc.WantCameraSerial)
			}
			photoID, isUnique, _ := fp.getPhotoID()
			if photoID != tc.WantPhotoID {
				t.Errorf("unexpected photo ID, got %q, want %q", photoID, tc.WantPhotoID)
			}
			if isUnique != tc.WantUniquePhotoID {
				t.Errorf("unexpected photo ID uniqueness, got %v, want %v", isUnique, tc.WantUniquePhotoID)
			}
			if f.Hash != tc.WantModelSerialPhotoID {
				t.Errorf("got ModelSerialPhotoID %q, want %q", f.Hash, tc.WantModelSerialPhotoID)
			}
			if f.Quality != tc.WantQuality {
				t.Errorf("got quality %v, want %v", f.Quality, tc.WantQuality)
			}
		})
	}
}
