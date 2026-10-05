package fingerprint

//go:generate protoc --go_out=. --go_opt=paths=source_relative fingerprint_test.proto

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"net/http"
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

const testDataDir = "testdata"

// testdataAssets are the release assets on rpajarola/dedup-testdata
// containing testdata's media files, grouped by type.
var testdataAssets = []string{"testdata-images.tar.gz", "testdata-videos.tar.gz", "testdata-audio.tar.gz", "testdata-noimage.tar.gz"}

const testdataReleaseURL = "https://github.com/rpajarola/dedup-testdata/releases/latest/download/"

// TestMain downloads testdata's media files if missing and fixes up the
// modification times of testdata files to match the "filedate" fingerprint
// recorded in their .textproto file before running tests. Checkouts don't
// preserve mtimes, so without this the filedate fingerprint test case would
// never match.
func TestMain(m *testing.M) {
	if err := fetchTestdata(testDataDir, testdataAssets...); err != nil {
		log.Printf("fetchTestdata: %v", err)
	}
	fixTestdataDates(testDataDir)
	os.Exit(m.Run())
}

// fetchTestdata downloads and extracts the given release tarballs into dir,
// but only if any .textproto's source file is currently missing.
func fetchTestdata(dir string, assets ...string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("readdir %s: %w", dir, err)
	}
	complete := true
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".textproto") {
			continue
		}
		sourceFile, _, err := readFiledate(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, sourceFile)); err != nil {
			complete = false
			break
		}
	}
	if complete {
		return nil
	}
	for _, asset := range assets {
		if err := downloadAndExtract(testdataReleaseURL+asset, dir); err != nil {
			return fmt.Errorf("fetch %s: %w", asset, err)
		}
	}
	return nil
}

// downloadAndExtract fetches a .tar.gz from url and extracts its regular
// files flat into destDir.
func downloadAndExtract(url, destDir string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		f, err := os.OpenFile(filepath.Join(destDir, filepath.Base(hdr.Name)), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			return err
		}
		_, err = io.Copy(f, tr)
		f.Close()
		if err != nil {
			return err
		}
	}
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

			sourceFile, ts, err := readFiledate(protoPath)
			if err != nil {
				log.Printf("skip %s: %v", protoPath, err)
				continue
			}
			dataPath := filepath.Join(dir, sourceFile)

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

// readFiledate parses the textproto file and returns its source file name
// and the Unix timestamp from the "filedate" want_fingerprint entry.
func readFiledate(path string) (string, int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	var tc FingerprintTestCase
	if err := prototext.Unmarshal(data, &tc); err != nil {
		return "", 0, fmt.Errorf("parse: %w", err)
	}
	for _, fp := range tc.WantFingerprint {
		if fp.GetWantKind() == "filedate" {
			ts, err := strconv.ParseInt(fp.GetWantHash(), 10, 64)
			if err != nil {
				return "", 0, fmt.Errorf("parse filedate %q: %w", fp.GetWantHash(), err)
			}
			return tc.GetSourceFile(), ts, nil
		}
	}
	return "", 0, fmt.Errorf("no filedate fingerprint found")
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
	for _, tc := range getTestCases(t, testDataDir) {
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
