package fingerprint

//go:generate protoc --go_out=. --go_opt=paths=source_relative exif_test.proto

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/prototext"
)

const testDataDir = "testcases"

func readTestCase(t *testing.T, fname string) *ExifTestCase {
	t.Helper()

	raw, err := os.ReadFile(fname)
	if err != nil {
		t.Fatal(err)
	}
	tc := &ExifTestCase{}
	if err := prototext.Unmarshal(raw, tc); err != nil {
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
		if !strings.HasSuffix(fname, ".textproto") {
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
			if tc.WantPhotoIdFp != "" && f.Kind != "EXIFModelSerialPhotoID" {
				t.Errorf("unexpected Kind, got %q, want %q", f.Kind, "EXIFModelSerialPhotoID")
			}
			if fp.getCameraModel() != tc.WantCameraModel {
				t.Errorf("unexpected camera model, got %q, want %q", fp.getCameraModel(), tc.WantCameraModel)
			}
			if fp.getCameraSerial() != tc.WantCameraSerial {
				t.Errorf("unexpected camera serial, got %q, want %q", fp.getCameraSerial(), tc.WantCameraSerial)
			}
			photoID, isUnique, _ := fp.getPhotoID()
			if photoID != tc.WantPhotoId {
				t.Errorf("unexpected photo ID, got %q, want %q", photoID, tc.WantPhotoId)
			}
			if isUnique != tc.WantUniquePhotoId {
				t.Errorf("unexpected photo ID uniqueness, got %v, want %v", isUnique, tc.WantUniquePhotoId)
			}
			if f.Hash != tc.WantPhotoIdFp {
				t.Errorf("got Photo ID fingerprint %q, want %q", f.Hash, tc.WantPhotoIdFp)
			}
			if f.Quality != int(tc.WantPhotoIdFpQuality) {
				t.Errorf("got quality %v, want %v", f.Quality, tc.WantPhotoIdFpQuality)
			}
		})
	}
}
