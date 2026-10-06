package fingerprint

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiskImageFingerprinter(t *testing.T) {
	t.Parallel()
	dfp := &DiskImageFingerprinter{}
	for _, tc := range getTestCases(t, testDataDir) {
		t.Run(filepath.Base(tc.Name), func(t *testing.T) {
			t.Parallel()
			defer maybeUpdateTestCase(t, tc)
			if tc.Got.GetDiskImage() == nil {
				tc.Got.DiskImage = &DiskImageTestCase{}
			}
			tc.Got.DiskImage.WantContentTree = ""
			tc.Got.DiskImage.WantContentSet = ""
			tc.Got.DiskImage.WantErr = false
			tc.Got.DiskImage.Comment = withoutComment(tc.Got.DiskImage.Comment, "Not a disk image")
			if tc.Got.DiskImage.Skip {
				t.Skip()
			}
			fps, err := dfp.Init(tc.SourceFile)
			if err != nil {
				t.Fatalf("dfp.Init(%v): %v", tc.SourceFile, err)
			}
			if fps == nil {
				tc.Got.DiskImage.Comment = append(tc.Got.DiskImage.Comment, "Not a disk image")
				return
			}
			defer fps.Cleanup()
			got, err := fps.Get()
			if err != nil {
				// Expected failures are recorded (with a comment
				// explaining why) rather than failing the test.
				tc.Got.DiskImage.WantErr = true
				return
			}
			for _, fp := range got {
				switch fp.Kind {
				case "ArchiveContentTree":
					tc.Got.DiskImage.WantContentTree = fp.Hash
				case "ArchiveContentSet":
					tc.Got.DiskImage.WantContentSet = fp.Hash
				}
			}
		})
	}
}

// TestDiskImageArchiveEquivalence checks that disk images containing the
// same files as archive_proj.zip match it (see dedup-testdata's
// sources.yaml for how each was made).
func TestDiskImageArchiveEquivalence(t *testing.T) {
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
	wantTree, wantSet := fingerprints("archive_proj.zip", &ArchiveFingerprinter{})

	for _, tc := range []struct {
		name        string
		sameTree    bool
		explanation string
	}{
		{"disk_proj_rockridge.iso", true, "Rock Ridge names"},
		{"disk_proj_joliet.iso", true, "Joliet names"},
		{"disk_proj_plain.iso", false, "plain ISO 9660 has only uppercase 8.3 names (DOCS/CAF_.TXT)"},
		{"disk_proj.squashfs", true, ""},
		{"disk_proj_ext4.img", true, "ext4, no partition table"},
		{"disk_proj_mbr.img", true, "ext4 in an MBR partition"},
		{"disk_proj_gpt.img", true, "ext4 in a GPT partition, next to an empty FAT EFI partition"},
		// go-diskfs mangles the lowercase-flagged non-ASCII 8.3 short
		// name of café.txt (see TODO.md), so the FAT images' trees
		// don't match (yet).
		{"disk_proj_floppy.img", false, "FAT12 floppy"},
		{"disk_proj_fat16.img", false, "FAT16"},
		{"disk_proj_fat32.img", false, "FAT32"},
	} {
		tree, set := fingerprints(tc.name, &DiskImageFingerprinter{})
		if set != wantSet {
			t.Errorf("%v (%v): ArchiveContentSet = %v, want %v (same as archive_proj.zip)", tc.name, tc.explanation, set, wantSet)
		}
		if (tree == wantTree) != tc.sameTree {
			t.Errorf("%v (%v): ArchiveContentTree equal to archive_proj.zip's = %v, want %v", tc.name, tc.explanation, tree == wantTree, tc.sameTree)
		}
	}
}

func TestIsDiskImage(t *testing.T) {
	t.Parallel()
	block := func(f func(b []byte)) []byte {
		b := make([]byte, 34*1024)
		f(b)
		return b
	}
	for _, tc := range []struct {
		desc string
		head []byte
		want bool
	}{
		{"zeros", block(func(b []byte) {}), false},
		{"too short", []byte("CD001"), false},
		{"ISO 9660", block(func(b []byte) { copy(b[32769:], "CD001") }), true},
		{"squashfs", block(func(b []byte) { copy(b, "hsqs") }), true},
		{"ext4", block(func(b []byte) { b[1080], b[1081] = 0x53, 0xef }), true},
		{"GPT", block(func(b []byte) { copy(b[512:], "EFI PART") }), true},
		{"55AA without BPB or partitions", block(func(b []byte) { b[510], b[511] = 0x55, 0xaa }), false},
		{"FAT boot sector", block(func(b []byte) {
			copy(b, []byte{0xeb, 0x3c, 0x90})
			b[11], b[12] = 0x00, 0x02 // 512 bytes/sector
			b[13] = 4                 // sectors/cluster
			b[14] = 1                 // reserved sectors
			b[16] = 2                 // FATs
			b[510], b[511] = 0x55, 0xaa
		}), true},
		{"MBR", block(func(b []byte) {
			b[446+4] = 0x83  // Linux partition
			b[446+12] = 0x10 // length
			b[510], b[511] = 0x55, 0xaa
		}), true},
		{"MBR with bad boot flag", block(func(b []byte) {
			b[446] = 0x12
			b[446+4] = 0x83
			b[446+12] = 0x10
			b[510], b[511] = 0x55, 0xaa
		}), false},
	} {
		if got := isDiskImage(tc.head); got != tc.want {
			t.Errorf("%v: isDiskImage = %v, want %v", tc.desc, got, tc.want)
		}
	}
}

func TestDecodeFATShortNames(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, want string }{
		{"PROJ/README.TXT", "PROJ/README.TXT"},
		{"PROJ/CAF\x90.TXT", "PROJ/CAFÉ.TXT"}, // 0x90 is É in CP437
		{"DIR\x81/x", "DIRü/x"},
		{"already/ünicode", "already/ünicode"},
	} {
		if got := decodeFATShortNames(tc.in); got != tc.want {
			t.Errorf("decodeFATShortNames(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
