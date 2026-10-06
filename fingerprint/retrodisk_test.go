package fingerprint

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestRetroDiskEquivalence checks which retro disk image fixtures (see
// dedup-testdata's sources.yaml; all hold the archive_proj files) match
// each other and archive_proj.zip.
func TestRetroDiskEquivalence(t *testing.T) {
	t.Parallel()
	fingerprints := func(name string, fp Fingerprinter) (tree, set string) {
		t.Helper()
		fn := filepath.Join(testDataDir, name)
		if _, err := os.Stat(fn); err != nil {
			t.Skipf("missing testdata: %v", err)
		}
		fps, err := fp.Init(fn)
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
	zipTree, zipSet := fingerprints("archive_proj.zip", &ArchiveFingerprinter{})
	type fps struct{ tree, set string }
	get := func(name string) fps {
		tree, set := fingerprints(name, &DiskImageFingerprinter{})
		return fps{tree, set}
	}
	sameAs := func(desc string, want fps, names ...string) {
		t.Helper()
		for _, name := range names {
			if got := get(name); got != want {
				t.Errorf("%v: %v = %+v, want %+v", desc, name, got, want)
			}
		}
	}

	// Amiga: directories and exact names, so they match the zip
	// completely, whether OFS, FFS or FFS+INTL+DIRCACHE, and with a
	// proj/ wrapper directory or not.
	sameAs("ADF vs zip", fps{zipTree, zipSet},
		"retro_proj_ofs.adf", "retro_proj_ffs.adf", "retro_proj_ffs_intl_dc.adf")

	// ProDOS: directories, but uppercase names, so only the set matches
	// the zip; both sector orders match each other.
	prodos := get("retro_proj_prodos.po")
	if prodos.set != zipSet || prodos.tree == zipTree {
		t.Errorf("ProDOS: %+v, want same set as zip (%v) and different tree", prodos, zipSet)
	}
	sameAs("ProDOS in DOS sector order", prodos, "retro_proj_prodos.dsk")

	// DOS 3.3: flat, and the 64K blob.bin is stored with length 0 (the
	// binary file length field is 16 bits); both orders match.
	sameAs("DOS 3.3 in ProDOS sector order", get("retro_proj_dos33.dsk"), "retro_proj_dos33.po")

	// C64 and Atari DOS 2: flat, so README ends up at the top level
	// where it's release noise; otherwise the same files, so all D64s
	// and ATRs have the same set. Disk name, ID, file order, error
	// bytes and density don't matter.
	d64 := get("retro_proj.d64")
	sameAs("D64", d64, "retro_proj_reordered.d64", "retro_proj_errorbytes.d64")
	atr := get("retro_proj_dos2sd.atr")
	sameAs("ATR", atr, "retro_proj_dos2ed.atr", "retro_proj_dos2dd.atr")
	if d64.set != atr.set {
		t.Errorf("D64 set %v != ATR set %v", d64.set, atr.set)
	}
}

func TestRetroDiskDetection(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		desc   string
		detect func([]byte) bool
		data   []byte
		want   bool
	}{
		{"ADF size, no DOS boot block", adfDetect, make([]byte, 901120), false},
		{"ADF", adfDetect, append([]byte("DOS\x01"), make([]byte, 901116)...), true},
		{"ADF wrong size", adfDetect, append([]byte("DOS\x01"), make([]byte, 1000)...), false},
		{"D64 size, no BAM", d64Detect, make([]byte, 174848), false},
		{"Apple II size, no filesystem", apple2Detect, make([]byte, a2Size140K), false},
		{"ATR", atrDetect, append([]byte{0x96, 0x02, 0, 0, 0x80, 0}, make([]byte, 200)...), true},
		{"ATR bad sector size", atrDetect, append([]byte{0x96, 0x02, 0, 0, 0x81, 0}, make([]byte, 200)...), false},
	} {
		if got := tc.detect(tc.data); got != tc.want {
			t.Errorf("%v: detected = %v, want %v", tc.desc, got, tc.want)
		}
	}
}

func adfDetect(b []byte) bool    { return isADF(bytes.NewReader(b), int64(len(b))) }
func d64Detect(b []byte) bool    { return isD64(bytes.NewReader(b), int64(len(b))) }
func apple2Detect(b []byte) bool { return isApple2(bytes.NewReader(b), int64(len(b))) }
func atrDetect(b []byte) bool    { return isATR(bytes.NewReader(b), int64(len(b))) }

func TestRetroNames(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ got, want string }{
		// c1541 stores lowercase ASCII as unshifted PETSCII letters.
		{petsciiName([]byte("MAIN.C\xa0\xa0\xa0\xa0\xa0\xa0\xa0\xa0\xa0\xa0")), "main.c"},
		{petsciiName([]byte("\xc1BC\xff")), `Abc\xff`},
		{atasciiName([]byte("README  ")), "README"},
		{atasciiName([]byte("caf\xe9\x00\x00\x00\x00")), `caf\xe9`},
		// GS/OS Technical Note #8's example: "Desk.Accs" with flags
		// 1011100111000000.
		{prodosName([]byte("DESK.ACCS"), 0b1011100111000000), "Desk.Accs"},
		{prodosName([]byte("DESK.ACCS"), 0b0011100111000000), "DESK.ACCS"},
		{prodosName([]byte("A\xc2C"), 0), "ABC"},
	} {
		if tc.got != tc.want {
			t.Errorf("got %q, want %q", tc.got, tc.want)
		}
	}
}
