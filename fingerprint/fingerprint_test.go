package fingerprint

//go:generate protoc --go_out=. --go_opt=paths=source_relative fingerprint_test.proto

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/protocolbuffers/txtpbfmt/parser"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
)

const largeTestDataDir = "large_testdata"
const testDataDir = "testdata"

// TestMain fixes up the modification times of testdata files to match the
// "filedate" fingerprint recorded in their .textproto file before running
// tests. Checkouts don't preserve mtimes, so without this the filedate
// fingerprint test case would never match.
func TestMain(m *testing.M) {
	fixTestdataDates(testDataDir, largeTestDataDir)
	os.Exit(m.Run())
}

// fixTestdataDates sets the modification time of testdata files to match the
// "filedate" fingerprint recorded in the corresponding .textproto file.
func fixTestdataDates(dirs ...string) {
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			log.Printf("readdir %s: %v", dir, err)
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if !strings.HasSuffix(name, ".textproto") {
				continue
			}
			protoPath := filepath.Join(dir, name)
			dataPath := strings.TrimSuffix(protoPath, ".textproto")

			ts, err := readFiledate(protoPath)
			if err != nil {
				log.Printf("skip %s: %v", protoPath, err)
				continue
			}

			if _, err := os.Stat(dataPath); err != nil {
				log.Printf("skip %s: source file not found: %v", dataPath, err)
				continue
			}

			modTime := time.Unix(ts, 0)
			if err := os.Chtimes(dataPath, modTime, modTime); err != nil {
				log.Printf("chtimes %s: %v", dataPath, err)
				continue
			}
		}
	}
}

// readFiledate parses the textproto file and returns the Unix timestamp from
// the "filedate" want_fingerprint entry.
func readFiledate(path string) (int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var tc FingerprintTestCase
	if err := prototext.Unmarshal(data, &tc); err != nil {
		return 0, fmt.Errorf("parse: %w", err)
	}
	for _, fp := range tc.WantFingerprint {
		if fp.GetWantKind() == "filedate" {
			ts, err := strconv.ParseInt(fp.GetWantHash(), 10, 64)
			if err != nil {
				return 0, fmt.Errorf("parse filedate %q: %w", fp.GetWantHash(), err)
			}
			return ts, nil
		}
	}
	return 0, fmt.Errorf("no filedate fingerprint found")
}

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

func getTestCases(t *testing.T, tcDirs ...string) []TestCase {
	var res []TestCase
	for _, tcDir := range tcDirs {
		tcs := getTestCasesFromDir(t, tcDir)
		res = append(res, tcs...)
	}
	return res
}

func getTestCasesFromDir(t *testing.T, tcDir string) []TestCase {
	t.Helper()
	var res []TestCase

	f, err := os.Open(tcDir)
	if err != nil {
		t.Fatalf("os.Open(%v): %v", tcDir, err)
	}
	fnames, err := f.Readdirnames(0)
	if err != nil {
		t.Fatalf("Readdirnames(%v): %v", tcDir, err)
	}
	for _, fname := range fnames {

		if !strings.HasSuffix(fname, ".textproto") {
			continue
		}
		fname = filepath.Join(tcDir, fname)
		want := readTestCase(t, fname)
		if want.Skip {
			continue
		}
		sourceFile := filepath.Join(tcDir, want.SourceFile)
		got := proto.Clone(want).(*FingerprintTestCase)
		tc := TestCase{
			Name:       fname,
			SourceFile: sourceFile,
			Got:        got,
			Want:       want,
		}
		res = append(res, tc)
	}
	return res
}

func updateTestCase(t *testing.T, tc TestCase) {
	t.Helper()
	fname := tc.Name + ".new"
	raw := []byte(prototext.Format(tc.Got))
	raw, err := parser.Format(raw)
	if err != nil {
		t.Fatalf("parser.Format(%v): %v", fname, err)
	}
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
	t.Parallel()
	for _, tc := range getTestCases(t, testDataDir, largeTestDataDir) {
		t.Run(filepath.Base(tc.Name), func(t *testing.T) {
			t.Parallel()
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
