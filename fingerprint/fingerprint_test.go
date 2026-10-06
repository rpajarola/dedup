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
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/protocolbuffers/txtpbfmt/parser"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/testing/protocmp"
)

const testDataDir = "testdata"

// testdataAssets are the release assets on rpajarola/dedup-testdata
// containing testdata's media files, grouped by type.
var testdataAssets = []string{"testdata-images.tar.gz", "testdata-videos.tar.gz", "testdata-audio.tar.gz", "testdata-archives.tar.gz", "testdata-diskimages.tar.gz", "testdata-retrodisks.tar.gz", "testdata-noimage.tar.gz"}

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

// Every test function reads all test cases and writes its own section of
// each one back to a shared .new file. To keep those from clobbering each
// other, test cases are only read from disk once per run (so a .new file
// written mid-run by one test isn't picked up as the baseline by another),
// and updates are merged field by field into a single result per file.
var (
	testCaseMu      sync.Mutex
	testCaseCache   = map[string]*FingerprintTestCase{} // by .textproto name
	testCaseUpdates = map[string]*FingerprintTestCase{} // by .textproto name
)

func readTestCase(t *testing.T, fname string) *FingerprintTestCase {
	t.Helper()
	testCaseMu.Lock()
	defer testCaseMu.Unlock()
	if tc, ok := testCaseCache[fname]; ok {
		return proto.Clone(tc).(*FingerprintTestCase)
	}
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
	testCaseCache[fname] = tc
	return proto.Clone(tc).(*FingerprintTestCase)
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

// mergeChangedFields copies every top-level field that differs between want
// and got from got into dst.
func mergeChangedFields(dst, want, got proto.Message) {
	d, w, g := dst.ProtoReflect(), want.ProtoReflect(), proto.Clone(got).ProtoReflect()
	fields := d.Descriptor().Fields()
	for i := range fields.Len() {
		fd := fields.Get(i)
		if fieldEqual(w, g, fd) {
			continue
		}
		if g.Has(fd) {
			d.Set(fd, g.Get(fd))
		} else {
			d.Clear(fd)
		}
	}
}

func fieldEqual(a, b protoreflect.Message, fd protoreflect.FieldDescriptor) bool {
	a1, b1 := a.New(), b.New()
	if a.Has(fd) {
		a1.Set(fd, a.Get(fd))
	}
	if b.Has(fd) {
		b1.Set(fd, b.Get(fd))
	}
	return proto.Equal(a1.Interface(), b1.Interface())
}

// updateTestCase merges tc's changes into the pending update for tc.Name
// (see testCaseUpdates) and writes the result to tc.Name + ".new".
func updateTestCase(t *testing.T, tc TestCase) {
	t.Helper()
	testCaseMu.Lock()
	defer testCaseMu.Unlock()
	merged, ok := testCaseUpdates[tc.Name]
	if !ok {
		merged = proto.Clone(tc.Want).(*FingerprintTestCase)
		testCaseUpdates[tc.Name] = merged
	}
	mergeChangedFields(merged, tc.Want, tc.Got)
	fname := tc.Name + ".new"
	raw := []byte(prototext.Format(merged))
	raw, err := parser.Format(raw)
	if err != nil {
		t.Fatalf("parser.Format(%v): %v", fname, err)
	}
	if err := os.WriteFile(fname, raw, 0644); err != nil {
		t.Fatalf("os.WriteFile(%v): %v", fname, err)
	}
	fmt.Printf("updated test case: %v\n", fname)
}

// withoutComment returns comments without c. Tests use it to clear the
// comment they add for "nothing to fingerprint" (e.g. "No image data")
// before re-running, so it doesn't outlive the condition, while keeping
// any hand-written comments.
func withoutComment(comments []string, c string) []string {
	var res []string
	for _, s := range comments {
		if s != c {
			res = append(res, s)
		}
	}
	return res
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

// TestUpdateTestCaseMerges checks that updates from different test
// functions to the same test case are merged rather than overwriting each
// other.
func TestUpdateTestCaseMerges(t *testing.T) {
	t.Parallel()
	fname := filepath.Join(t.TempDir(), "x.textproto")
	if err := os.WriteFile(fname, []byte(`source_file: "x"
xmp: { comment: "old" }
`), 0644); err != nil {
		t.Fatal(err)
	}
	newTC := func() TestCase {
		want := readTestCase(t, fname)
		return TestCase{Name: fname, Got: proto.Clone(want).(*FingerprintTestCase), Want: want}
	}
	tc1 := newTC()
	tc1.Got.Exif = &EXIFTestCase{WantCameraModel: "cam"}
	tc2 := newTC()
	tc2.Got.Xmp = nil
	tc2.Got.Audio = &AudioTestCase{WantSimhash: "1234"}
	tc3 := newTC() // no changes: must not revert the others
	updateTestCase(t, tc1)
	updateTestCase(t, tc2)
	updateTestCase(t, tc3)

	raw, err := os.ReadFile(fname + ".new")
	if err != nil {
		t.Fatal(err)
	}
	got := &FingerprintTestCase{}
	if err := prototext.Unmarshal(raw, got); err != nil {
		t.Fatal(err)
	}
	want := &FingerprintTestCase{
		SourceFile: "x",
		Exif:       &EXIFTestCase{WantCameraModel: "cam"},
		Audio:      &AudioTestCase{WantSimhash: "1234"},
	}
	if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
		t.Errorf("merged .new mismatch, +=got, -=want:\n%v", diff)
	}
}
