package fingerprint

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestArchiveFingerprinter(t *testing.T) {
	t.Parallel()
	afp := &ArchiveFingerprinter{}
	for _, tc := range getTestCases(t, testDataDir) {
		t.Run(filepath.Base(tc.Name), func(t *testing.T) {
			t.Parallel()
			defer maybeUpdateTestCase(t, tc)
			if tc.Got.GetArchive() == nil {
				tc.Got.Archive = &ArchiveTestCase{}
			}
			tc.Got.Archive.WantContentTree = ""
			tc.Got.Archive.WantContentSet = ""
			tc.Got.Archive.WantErr = false
			tc.Got.Archive.Comment = withoutComment(tc.Got.Archive.Comment, "Not an archive")
			if tc.Got.Archive.Skip {
				t.Skip()
			}
			fps, err := afp.Init(tc.SourceFile)
			if err != nil {
				t.Fatalf("afp.Init(%v): %v", tc.SourceFile, err)
			}
			if fps == nil {
				tc.Got.Archive.Comment = append(tc.Got.Archive.Comment, "Not an archive")
				return
			}
			defer fps.Cleanup()
			got, err := fps.Get()
			if err != nil {
				// Expected failures are recorded (with a comment
				// explaining why) rather than failing the test.
				tc.Got.Archive.WantErr = true
				return
			}
			for _, fp := range got {
				switch fp.Kind {
				case "ArchiveContentTree":
					tc.Got.Archive.WantContentTree = fp.Hash
				case "ArchiveContentSet":
					tc.Got.Archive.WantContentSet = fp.Hash
				}
			}
		})
	}
}

// TestArchiveRepackEquivalence checks that the archive fixtures, which all
// package (variants of) the same files, match or don't match each other as
// intended. See dedup-testdata's sources.yaml for how each was made.
func TestArchiveRepackEquivalence(t *testing.T) {
	t.Parallel()
	fingerprints := func(name string) (tree, set string) {
		t.Helper()
		fn := filepath.Join(testDataDir, name)
		if _, err := os.Stat(fn); err != nil {
			t.Skipf("missing testdata: %v", err)
		}
		fps, err := (&ArchiveFingerprinter{}).Init(fn)
		if err != nil || fps == nil {
			t.Fatalf("Init(%v) = %v, %v", fn, fps, err)
		}
		defer fps.Cleanup()
		got, err := fps.Get()
		if err != nil {
			t.Fatalf("Get(%v): %v", fn, err)
		}
		for _, fp := range got {
			switch fp.Kind {
			case "ArchiveContentTree":
				tree = fp.Hash
			case "ArchiveContentSet":
				set = fp.Hash
			}
		}
		return tree, set
	}

	repacks := []string{
		"archive_proj.zip",              // proj/...
		"archive_proj_dotslash.tar.gz",  // ./...
		"archive_proj_noprefix.tar.bz2", // no common directory
		"archive_proj_versioned.tar.xz", // proj-1.0/...
		"archive_proj.tar.zst",
		"archive_proj_solid.7z",
		"archive_proj.rar",
		"archive_proj_nfd.zip",        // NFD-normalized file names
		"archive_proj_with_noise.zip", // + README, FILE_ID.DIZ, .nfo, SHA256SUMS, __MACOSX, .DS_Store
	}
	wantTree, wantSet := fingerprints(repacks[0])
	for _, name := range repacks[1:] {
		tree, set := fingerprints(name)
		if tree != wantTree {
			t.Errorf("%v: ArchiveContentTree = %v, want %v (same as %v)", name, tree, wantTree, repacks[0])
		}
		if set != wantSet {
			t.Errorf("%v: ArchiveContentSet = %v, want %v (same as %v)", name, set, wantSet, repacks[0])
		}
	}

	// A renamed file changes the tree but not the set of contents.
	if tree, set := fingerprints("archive_proj_renamed.zip"); tree == wantTree || set != wantSet {
		t.Errorf("archive_proj_renamed.zip: tree equal = %v (want false), set equal = %v (want true)", tree == wantTree, set == wantSet)
	}
	// Modified content changes both.
	if tree, set := fingerprints("archive_proj_modified.zip"); tree == wantTree || set == wantSet {
		t.Errorf("archive_proj_modified.zip: tree equal = %v, set equal = %v, want both false", tree == wantTree, set == wantSet)
	}
	// Single compressed files match regardless of compressor.
	gzTree, gzSet := fingerprints("archive_manual.txt.gz")
	if xzTree, xzSet := fingerprints("archive_manual.txt.xz"); xzTree != gzTree || xzSet != gzSet {
		t.Errorf("archive_manual.txt.xz = %v, %v; want same as .gz: %v, %v", xzTree, xzSet, gzTree, gzSet)
	}
}

func TestCanonicalizeArchiveEntries(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		desc string
		in   []string
		want []string
	}{
		{
			desc: "leading ./ and /",
			in:   []string{"./a", "/b/c", "./d/../e"},
			want: []string{"a", "b/c", "e"},
		},
		{
			desc: "common directory is stripped",
			in:   []string{"proj-1.0/src/a.c", "proj-1.0/src/b.c", "proj-1.0/doc/x"},
			want: []string{"src/a.c", "src/b.c", "doc/x"},
		},
		{
			desc: "single file loses its directory",
			in:   []string{"x/y/z.txt"},
			want: []string{"z.txt"},
		},
		{
			desc: "backslashes and NFD",
			in:   []string{"p\\café.txt", `p\b`},
			want: []string{"café.txt", "b"},
		},
		{
			desc: "OS clutter anywhere",
			in:   []string{"p/a", "p/b", "p/.DS_Store", "__MACOSX/p/._a", "p/sub/Thumbs.db", "p/sub/._x", "p/desktop.ini"},
			want: []string{"a", "b"},
		},
		{
			desc: "top-level release noise around a wrapper directory",
			in:   []string{"README.txt", "FILE_ID.DIZ", "x.nfo", "SHA256SUMS", "proj/a", "proj/b", "proj/readme.md", "proj/x.sfv"},
			want: []string{"a", "b"},
		},
		{
			desc: "release noise deeper down is content",
			in:   []string{"proj/a", "proj/src/b", "proj/src/README", "proj/src/x.md5"},
			want: []string{"a", "src/b", "src/README", "src/x.md5"},
		},
		{
			desc: "noise outside the common directory is content",
			in:   []string{"a/b/c", "a/b/d", "e/README"},
			want: []string{"a/b/c", "a/b/d", "e/README"},
		},
		{
			desc: "only release noise",
			in:   []string{"x/README", "x/file_id.diz"},
			want: []string{"README", "file_id.diz"},
		},
		{
			desc: "duplicate names: last wins",
			in:   []string{"a", "./a", "b"},
			want: []string{"a", "b"},
		},
	} {
		var entries []archiveEntry
		for i, name := range tc.in {
			entries = append(entries, archiveEntry{name: name, hash: [32]byte{byte(i)}})
		}
		var got []string
		for _, e := range canonicalizeArchiveEntries(entries) {
			got = append(got, e.name)
		}
		slices.Sort(got)
		want := slices.Sorted(slices.Values(tc.want))
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("%v: canonicalizeArchiveEntries(%q) mismatch (-want +got):\n%s", tc.desc, tc.in, diff)
		}
	}

	// The last of several entries with the same name wins.
	got := canonicalizeArchiveEntries([]archiveEntry{{name: "a", hash: [32]byte{1}}, {name: "./a", hash: [32]byte{2}}})
	if len(got) != 1 || got[0].hash[0] != 2 {
		t.Errorf("duplicate names: got %v, want only the second entry", got)
	}
}
