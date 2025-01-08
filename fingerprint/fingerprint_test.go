package fingerprint

//go:generate protoc --go_out=. --go_opt=paths=source_relative fingerprint_test.proto

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/prototext"
)

const testDataDir = "testcases"

func readTestCase(t *testing.T, fname string) *FingerprintTestCase {
	t.Helper()
	raw, err := os.ReadFile(fname)
	if err != nil {
		t.Fatal(err)
	}
	tc := &FingerprintTestCase{}
	if err := prototext.Unmarshal(raw, tc); err != nil {
		t.Fatalf("prototext.Unmarshal(%v): %v", fname, err)
	}
	return tc
}

func getTestCases(t *testing.T) []*FingerprintTestCase {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	defer os.Chdir(cwd)
	os.Chdir(testDataDir)

	var res []*FingerprintTestCase

	f, err := os.Open(".")
	if err != nil {
		t.Fatalf("os.Open(%v): %v", testDataDir, err)
	}
	fnames, err := f.Readdirnames(0)
	if err != nil {
		t.Fatalf("Readdirnames(%v): %v", testDataDir, err)
	}
	for _, fname := range fnames {

		if !strings.HasSuffix(fname, ".textproto") {
			continue
		}
		tc := readTestCase(t, fname)
		if tc.Skip {
			continue
		}
		tc.Name = fname
		tc.SourceFile, err = filepath.Abs(tc.SourceFile)
		if err != nil {
			t.Fatalf("filepath.Abs(%v): %v", tc.SourceFile, err)
		}
		res = append(res, tc)
	}
	return res
}

func TestGetFingerprint(t *testing.T) {
	for _, tc := range getTestCases(t) {
		t.Run(filepath.Base(tc.Name), func(t *testing.T) {
			gotFps, gotErr := GetFingerprint(tc.SourceFile)
			gotIsErr := gotErr != nil
			if gotIsErr != tc.WantErr {
				t.Fatalf("unexpected error: got %v (%v), want %v", gotIsErr, gotErr, tc.WantErr)
			}
			gotFpMap := make(map[string]Fingerprint)
			wantFpMap := make(map[string]*WantFingerprint)
			for _, gotFp := range gotFps {
				if _, ok := gotFpMap[gotFp.Hash]; ok {
					t.Errorf("duplicate fingerprint: %v", gotFp)
				}
				gotFpMap[gotFp.Hash] = gotFp
			}
			gotHashes := slices.Collect(maps.Keys(gotFpMap))

			for _, wantFp := range tc.WantFingerprint {
				wantFpMap[wantFp.WantHash] = wantFp
				gotFp, ok := gotFpMap[wantFp.WantHash]
				if !ok {
					t.Errorf("missing fingerprint: got %q, want %q (%v)", gotHashes, wantFp.WantHash, wantFp.WantKind)
					continue
				}
				if gotFp.Kind != wantFp.WantKind {
					t.Errorf("unexpected fingerprint kind for %v: got %v, want %v", gotFp.Hash, gotFp.Kind, wantFp.WantKind)
				}
				if gotFp.Quality != int(wantFp.WantQuality) {
					t.Errorf("unexpected fingerprint quality for %v: got %v, want %v", gotFp.Hash, gotFp.Quality, wantFp.WantQuality)
				}
			}
			for _, gotFp := range gotFpMap {
				if _, ok := wantFpMap[gotFp.Hash]; !ok {
					fmt.Printf("----- extra fingerprint %v -----\n", tc.Name)
					fmt.Printf("want_fingerprint {\n")
					fmt.Printf("	want_kind: %q\n", gotFp.Kind)
					fmt.Printf("	want_hash: %q\n", gotFp.Hash)
					fmt.Printf("	want_quality: %v\n", gotFp.Quality)
					fmt.Printf("}\n")
					fmt.Printf("----- /extra fingerprint -----\n")
					t.Errorf("extra fingerprint kind=%v hash=%v quality=%v", gotFp.Kind, gotFp.Hash, gotFp.Quality)
				}
			}
		})
	}
}
