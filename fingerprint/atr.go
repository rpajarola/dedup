package fingerprint

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
)

// Reader for Atari 8-bit ATR disk images with an Atari DOS 2.0/2.5
// filesystem (single, enhanced and double density). An ATR file is a
// 16-byte header followed by the disk's sectors (numbered from 1; on
// double density disks the first three sectors are 128 bytes). DOS 2
// keeps its directory in sectors 361-368 (8 entries of 16 bytes per
// sector); files are chains of sectors whose last 3 bytes hold the
// file's directory index, the next sector number and the number of data
// bytes in the sector.
//
// Disks without DOS 2 (boot disks, SpartaDOS, MyDOS, ...) yield no files.

const atrHeaderSize = 16

type atrImage struct {
	data       []byte // without the header
	sectorSize int
	// fullBoot is set if the first three sectors of a double density
	// image are stored as full 256-byte sectors (only their first 128
	// bytes are used).
	fullBoot bool
}

// isATR reports whether r (of the given size) is an ATR image.
func isATR(r io.ReaderAt, size int64) bool {
	head := make([]byte, atrHeaderSize)
	if size <= atrHeaderSize {
		return false
	}
	if _, err := r.ReadAt(head, 0); err != nil {
		return false
	}
	if head[0] != 0x96 || head[1] != 0x02 {
		return false
	}
	ss := binary.LittleEndian.Uint16(head[4:])
	return ss == 128 || ss == 256
}

func (a *atrImage) sector(n int) ([]byte, error) {
	if n < 1 {
		return nil, fmt.Errorf("ATR: sector %d out of range", n)
	}
	off, size := 0, a.sectorSize
	switch {
	case a.sectorSize == 128 || a.fullBoot:
		off = (n - 1) * a.sectorSize
	case n <= 3:
		off, size = (n-1)*128, 128
	default:
		off = 3*128 + (n-4)*a.sectorSize
	}
	if off+size > len(a.data) {
		return nil, fmt.Errorf("ATR: sector %d out of range", n)
	}
	if n <= 3 {
		size = 128
	}
	return a.data[off : off+size], nil
}

// readATR returns the files in an ATR image's DOS 2 filesystem.
func readATR(data []byte) ([]archiveEntry, error) {
	if len(data) < atrHeaderSize {
		return nil, fmt.Errorf("ATR: image too short")
	}
	a := &atrImage{data: data[atrHeaderSize:], sectorSize: int(binary.LittleEndian.Uint16(data[4:]))}
	a.fullBoot = a.sectorSize == 256 && len(a.data)%256 == 0
	vtoc, err := a.sector(360)
	if err != nil || vtoc[0] != 2 {
		return nil, nil // not a DOS 2 disk
	}
	var res []archiveEntry
	for i := range 64 {
		dir, err := a.sector(361 + i/8)
		if err != nil {
			return nil, err
		}
		e := dir[16*(i%8) : 16*(i%8+1)]
		flags := e[0]
		if flags == 0 {
			break // never used: end of directory
		}
		// Skip deleted (0x80) entries and files still open for
		// writing (0x01), which are incomplete.
		if flags&0x80 != 0 || flags&0x40 == 0 || flags&0x01 != 0 {
			continue
		}
		name := atasciiName(e[5:13])
		if ext := atasciiName(e[13:16]); ext != "" {
			name += "." + ext
		}
		// An entry using no sectors is an empty file (DOS 2 itself
		// always allocates one, but other tools don't).
		if binary.LittleEndian.Uint16(e[1:]) == 0 {
			res = append(res, archiveEntry{name: name, hash: sha256.Sum256(nil)})
			continue
		}
		h, err := a.fileHash(i, int(binary.LittleEndian.Uint16(e[3:])))
		if err != nil {
			return nil, fmt.Errorf("ATR: %v: %w", name, err)
		}
		res = append(res, archiveEntry{name: name, hash: h})
	}
	return res, nil
}

// fileHash returns the hash of file number fileNo, starting at sector
// start.
func (a *atrImage) fileHash(fileNo, start int) ([sha256.Size]byte, error) {
	h := sha256.New()
	seen := map[int]bool{}
	for n := start; n != 0; {
		if seen[n] {
			return [sha256.Size]byte{}, fmt.Errorf("sector chain loops at %d", n)
		}
		seen[n] = true
		sec, err := a.sector(n)
		if err != nil {
			return [sha256.Size]byte{}, err
		}
		l := len(sec)
		if int(sec[l-3]>>2) != fileNo {
			return [sha256.Size]byte{}, fmt.Errorf("sector %d belongs to file %d, not %d", n, sec[l-3]>>2, fileNo)
		}
		count := int(sec[l-1])
		if l == 128 {
			count &= 0x7f // bit 7 is the "short sector" flag
		}
		if count > l-3 {
			return [sha256.Size]byte{}, fmt.Errorf("sector %d: byte count %d too large", n, count)
		}
		h.Write(sec[:count])
		n = int(sec[l-3]&0x03)<<8 | int(sec[l-2])
	}
	var res [sha256.Size]byte
	copy(res[:], h.Sum(nil))
	return res, nil
}

// atasciiName decodes a space (or NUL) padded ATASCII name field;
// characters outside printable ASCII are kept as \xNN escapes.
func atasciiName(b []byte) string {
	var sb strings.Builder
	for _, c := range b {
		if c >= 0x20 && c < 0x7f {
			sb.WriteByte(c)
		} else {
			fmt.Fprintf(&sb, `\x%02x`, c)
		}
	}
	return strings.TrimRight(strings.ReplaceAll(sb.String(), `\x00`, " "), " ")
}
