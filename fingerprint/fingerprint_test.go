package fingerprint

//go:generate protoc --go_out=. --go_opt=paths=source_relative fingerprint_test.proto

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
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

func updateTestCase(t *testing.T, tc *FingerprintTestCase) {
	t.Helper()
	fname := filepath.Join(testDataDir, tc.Name) + ".new"
	tc.Name = ""
	raw := []byte(prototext.Format(tc))
	if err := os.WriteFile(fname, raw, 0644); err != nil {
		t.Fatalf("os.WriteFile(%v): %v", fname, err)
	}
	fmt.Printf("updated test case: %v\n", fname)
}

func TestGetFingerprint(t *testing.T) {
	for _, tc := range getTestCases(t) {
		t.Run(filepath.Base(tc.Name), func(t *testing.T) {
			gotFps, gotErr := GetFingerprint(tc.SourceFile)
			gotTc := proto.Clone(tc).(*FingerprintTestCase)

			gotTc.WantErr = gotErr != nil
			gotTc.WantFingerprint = nil
			for _, gotFp := range gotFps {
				gotTc.WantFingerprint = append(gotTc.WantFingerprint, &WantFingerprint{
					WantKind:    gotFp.Kind,
					WantHash:    gotFp.Hash,
					WantQuality: int32(gotFp.Quality),
				})
			}
			got := prototext.Format(gotTc)
			want := prototext.Format(tc)
			if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
				t.Errorf("Unexpected test result, +=got, -=want:\n\n%v", diff)
				updateTestCase(t, gotTc)
			}
		})
	}
}
