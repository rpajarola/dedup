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

type TestCase struct {
	Name       string
	SourceFile string
	Got        *FingerprintTestCase
	Want       *FingerprintTestCase
}

func readTestCase(t *testing.T, fname string) *FingerprintTestCase {
	t.Helper()
	raw, err := os.ReadFile(fname + ".new")
	if err != nil {
		if raw, err = os.ReadFile(fname); err != nil {
			t.Fatal(err)
		}
	}
	tc := &FingerprintTestCase{}
	if err := prototext.Unmarshal(raw, tc); err != nil {
		t.Fatalf("prototext.Unmarshal(%v): %v", fname, err)
	}
	return tc
}

func getTestCases(t *testing.T) []TestCase {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	defer os.Chdir(cwd)
	os.Chdir(testDataDir)

	var res []TestCase

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
		want := readTestCase(t, fname)
		if want.Skip {
			continue
		}
		got := proto.Clone(want).(*FingerprintTestCase)
		tc := TestCase{
			Name: fname,
			Got:  got,
			Want: want,
		}
		tc.SourceFile, err = filepath.Abs(want.SourceFile)
		if err != nil {
			t.Fatalf("filepath.Abs(%v): %v", want.SourceFile, err)
		}
		res = append(res, tc)
	}
	return res
}

func updateTestCase(t *testing.T, tc TestCase) {
	t.Helper()
	fname := filepath.Join(testDataDir, tc.Name) + ".new"
	raw := []byte(prototext.Format(tc.Got))
	if err := os.WriteFile(fname, raw, 0644); err != nil {
		t.Fatalf("os.WriteFile(%v): %v", fname, err)
	}
	fmt.Printf("updated test case: %v\n", fname)
}

func maybeUpdateTestCase(t *testing.T, tc TestCase) {
	t.Helper()
	got := prototext.Format(tc.Got)
	want := prototext.Format(tc.Want)
	if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
		t.Errorf("Unexpected test result, +=got, -=want:\n\n%v", diff)
		updateTestCase(t, tc)
	}
}

func TestGetFingerprint(t *testing.T) {
	for _, tc := range getTestCases(t) {
		t.Run(filepath.Base(tc.Name), func(t *testing.T) {
			gotFps, gotErr := GetFingerprint(tc.SourceFile)
			tc.Got.WantErr = gotErr != nil
			tc.Got.WantFingerprint = nil
			for _, gotFp := range gotFps {
				tc.Got.WantFingerprint = append(tc.Got.WantFingerprint, &WantFingerprint{
					WantKind:    gotFp.Kind,
					WantHash:    gotFp.Hash,
					WantQuality: int32(gotFp.Quality),
				})
			}
			maybeUpdateTestCase(t, tc)
		})
	}
}
