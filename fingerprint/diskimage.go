package fingerprint

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"unicode/utf8"

	diskfs "github.com/diskfs/go-diskfs"
	"github.com/diskfs/go-diskfs/filesystem"
	"github.com/diskfs/go-diskfs/filesystem/squashfs"
	"golang.org/x/text/encoding/charmap"
)

// DiskImageFingerprinter fingerprints the files inside disk images: ISO
// 9660 CD/DVD images (with Rock Ridge/Joliet names), raw disk images
// holding FAT12/16/32, ext2/3/4 or squashfs filesystems, either directly
// ("superfloppy") or in MBR/GPT partitions, and retro computer disk
// images (see retroDiskFormats).
//
// It emits the same ArchiveContentTree/ArchiveContentSet fingerprints as
// ArchiveFingerprinter (with the same canonicalization), so a disk image
// matches an archive containing the same files. If an image has more than
// one filesystem with files in it, each one's files are put under
// "part<N>/" (N being the partition number).
type DiskImageFingerprinter struct{}

type diskImageFingerprinterState struct {
	filename string
	// read, if set, reads a retro disk image format (see
	// retroDiskFormats); otherwise go-diskfs is used.
	read func(data []byte) ([]archiveEntry, error)
}

// retroDiskFormats are disk image formats of old home computers, read
// by our own parsers (go-diskfs doesn't support them). Their images are
// at most a few MB, so they're read into memory whole.
var retroDiskFormats = []struct {
	detect func(r io.ReaderAt, size int64) bool
	read   func(data []byte) ([]archiveEntry, error)
}{
	{isADF, readADF},
	{isD64, readD64},
	{isApple2, readApple2},
	{isATR, readATR},
}

func init() {
	fingerprinters = append(fingerprinters, &DiskImageFingerprinter{})
}

func (dfp *DiskImageFingerprinter) Init(filename string) (FingerprinterState, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("open %v: %v", filename, err)
	}
	defer f.Close()
	// Disk images have no file type of their own; don't second-guess
	// files that were recognized as something else.
	if getFiletype(f) != "" {
		return nil, nil
	}
	head := make([]byte, 34*1024)
	n, _ := io.ReadFull(f, head)
	head = head[:n]
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	for _, rf := range retroDiskFormats {
		if rf.detect(f, st.Size()) {
			return &diskImageFingerprinterState{filename: filename, read: rf.read}, nil
		}
	}
	if !isDiskImage(head) {
		return nil, nil
	}
	return &diskImageFingerprinterState{filename: filename}, nil
}

func (dfps *diskImageFingerprinterState) Get() ([]Fingerprint, error) {
	var entries []archiveEntry
	var err error
	if dfps.read != nil {
		var data []byte
		if data, err = os.ReadFile(dfps.filename); err == nil {
			entries, err = dfps.read(data)
		}
	} else {
		entries, err = readDiskImage(dfps.filename)
	}
	if err != nil {
		return nil, fmt.Errorf("reading disk image %v: %w", dfps.filename, err)
	}
	return archiveFingerprints(entries), nil
}

func (dfps *diskImageFingerprinterState) Cleanup() {}

// isDiskImage reports whether head (the start of a file) looks like a
// supported disk image.
func isDiskImage(head []byte) bool {
	at := func(off int, magic string) bool {
		return len(head) >= off+len(magic) && string(head[off:off+len(magic)]) == magic
	}
	switch {
	case at(32769, "CD001"): // ISO 9660 primary volume descriptor
		return true
	case at(0, "hsqs"): // squashfs (little-endian)
		return true
	case len(head) >= 1082 && binary.LittleEndian.Uint16(head[1080:]) == 0xef53: // ext2/3/4 superblock
		return true
	case at(512, "EFI PART"), at(4096, "EFI PART"): // GPT header
		return true
	case len(head) >= 512 && head[510] == 0x55 && head[511] == 0xaa:
		return isFATBootSector(head) || isMBR(head)
	}
	return false
}

// isFATBootSector reports whether b starts with a plausible FAT boot
// sector (BIOS parameter block).
func isFATBootSector(b []byte) bool {
	if !(b[0] == 0xeb && b[2] == 0x90) && b[0] != 0xe9 {
		return false
	}
	bytesPerSector := binary.LittleEndian.Uint16(b[11:])
	sectorsPerCluster := b[13]
	reserved := binary.LittleEndian.Uint16(b[14:])
	numFATs := b[16]
	switch bytesPerSector {
	case 512, 1024, 2048, 4096:
	default:
		return false
	}
	return sectorsPerCluster != 0 && sectorsPerCluster&(sectorsPerCluster-1) == 0 &&
		reserved >= 1 && (numFATs == 1 || numFATs == 2)
}

// isMBR reports whether b starts with a plausible MBR partition table: all
// four entries have a valid boot flag, and at least one is in use.
func isMBR(b []byte) bool {
	used := false
	for i := range 4 {
		e := b[446+16*i : 446+16*(i+1)]
		if e[0] != 0x00 && e[0] != 0x80 {
			return false
		}
		if e[4] != 0 && binary.LittleEndian.Uint32(e[12:]) != 0 {
			used = true
		}
	}
	return used
}

// readDiskImage returns the regular files in all filesystems in a disk
// image, with their content hashes.
func readDiskImage(filename string) ([]archiveEntry, error) {
	d, err := diskfs.Open(filename, diskfs.WithOpenMode(diskfs.ReadOnly))
	if err != nil {
		return nil, err
	}
	defer d.Close()

	type partFS struct {
		part int
		fsys filesystem.FileSystem
	}
	var filesystems []partFS
	// A filesystem spanning the whole image (ISO, superfloppy, ...)
	// takes precedence over a partition table, so that e.g. hybrid ISOs
	// (which also have an MBR) are read as ISOs.
	//
	// squashfs is opened directly: d.GetFilesystem passes the disk's
	// logical sector size (512) as the squashfs block size, which
	// squashfs.Read rejects (as of go-diskfs v1.9.4).
	magic := make([]byte, 4)
	if _, err := d.Backend.ReadAt(magic, 0); err == nil && string(magic) == "hsqs" {
		fsys, err := squashfs.Read(d.Backend, d.Size, 0, 0)
		if err != nil {
			return nil, err
		}
		filesystems = append(filesystems, partFS{0, fsys})
	} else if fsys, err := d.GetFilesystem(0); err == nil {
		filesystems = append(filesystems, partFS{0, fsys})
	} else if pt, err := d.GetPartitionTable(); err == nil {
		for i := range pt.GetPartitions() {
			if fsys, err := d.GetFilesystem(i + 1); err == nil {
				filesystems = append(filesystems, partFS{i + 1, fsys})
			}
		}
	}
	if len(filesystems) == 0 {
		return nil, fmt.Errorf("no supported filesystem found")
	}

	var perFS [][]archiveEntry
	var parts []int
	for _, pf := range filesystems {
		entries, err := readFS(pf.fsys)
		if err != nil {
			return nil, fmt.Errorf("partition %d: %w", pf.part, err)
		}
		switch pf.fsys.Type() {
		case filesystem.TypeFat12, filesystem.TypeFat16, filesystem.TypeFat32:
			for i := range entries {
				entries[i].name = decodeFATShortNames(entries[i].name)
			}
		}
		if len(entries) > 0 {
			perFS = append(perFS, entries)
			parts = append(parts, pf.part)
		}
	}
	if len(perFS) == 1 {
		return perFS[0], nil
	}
	var res []archiveEntry
	for i, entries := range perFS {
		for _, e := range entries {
			e.name = path.Join(fmt.Sprintf("part%d", parts[i]), e.name)
			res = append(res, e)
		}
	}
	return res, nil
}

// readFS returns the regular files in fsys with their content hashes.
//
// This doesn't use fs.WalkDir, and hashes empty files without opening
// them, to work around go-diskfs's FAT implementation (as of v1.9.4)
// failing to Stat the root directory of FAT12/16 filesystems (which isn't
// stored in a cluster) and to open empty files (which have no start
// cluster).
func readFS(fsys fs.FS) ([]archiveEntry, error) {
	var res []archiveEntry
	var walk func(dir string, depth int) error
	walk = func(dir string, depth int) error {
		if depth > 64 {
			return fmt.Errorf("%v: directories nested too deeply", dir)
		}
		des, err := fs.ReadDir(fsys, dir)
		if err != nil {
			return fmt.Errorf("%v: %w", dir, err)
		}
		for _, de := range des {
			if de.Name() == "." || de.Name() == ".." {
				continue
			}
			name := path.Join(dir, de.Name())
			switch {
			case de.IsDir():
				if err := walk(name, depth+1); err != nil {
					return err
				}
			case de.Type().IsRegular():
				if info, err := de.Info(); err == nil && info.Size() == 0 {
					res = append(res, archiveEntry{name: name, hash: sha256.Sum256(nil)})
					continue
				}
				h, err := hashFSFile(fsys, name)
				if err != nil {
					return fmt.Errorf("%v: %w", name, err)
				}
				res = append(res, archiveEntry{name: name, hash: h})
			}
		}
		return nil
	}
	return res, walk(".", 0)
}

func hashFSFile(fsys fs.FS, name string) ([sha256.Size]byte, error) {
	f, err := fsys.Open(name)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	defer f.Close()
	return hashReader(f)
}

// decodeFATShortNames decodes path components that aren't valid UTF-8 as
// code page 437. FAT stores names that fit in 8.3 (e.g. "café.txt") as
// short names in the DOS OEM code page, without a long (UTF-16) name,
// and go-diskfs returns their bytes as-is. The actual code page isn't
// recorded anywhere; 437 (US) agrees with the common western European
// 850 on the accented letters.
func decodeFATShortNames(name string) string {
	if utf8.ValidString(name) {
		return name
	}
	parts := strings.Split(name, "/")
	for i, p := range parts {
		if !utf8.ValidString(p) {
			if d, err := charmap.CodePage437.NewDecoder().String(p); err == nil {
				parts[i] = d
			}
		}
	}
	return strings.Join(parts, "/")
}
