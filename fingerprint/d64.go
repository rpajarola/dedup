package fingerprint

import (
	"crypto/sha256"
	"fmt"
	"io"
	"strings"
)

// Reader for Commodore 1541/1571 disk images (D64, D71): 256-byte
// sectors, a varying number of sectors per track, one flat directory on
// track 18, and files stored as chains of sectors whose first two bytes
// link to the next sector (track 0 = last sector, whose second byte is
// then the index of its last used byte).

const d64SectorSize = 256

// d64SectorsPerTrack returns the number of sectors on a 1541 track
// (1-based; tracks 36-40 are the non-standard extended tracks).
func d64SectorsPerTrack(track int) int {
	switch {
	case track <= 17:
		return 21
	case track <= 24:
		return 19
	case track <= 30:
		return 18
	}
	return 17
}

// d64Geometry describes a supported image layout.
type d64Geometry struct {
	tracks   int // per side
	sides    int
	size     int64 // without error bytes
	dirTrack int
}

var d64Geometries = []d64Geometry{
	{tracks: 35, sides: 1, size: 174848, dirTrack: 18},
	{tracks: 40, sides: 1, size: 196608, dirTrack: 18},
	{tracks: 35, sides: 2, size: 349696, dirTrack: 18}, // D71
}

// d64GeometryOf returns the geometry of an image of the given size (which
// may include one trailing error byte per sector).
func d64GeometryOf(size int64) (d64Geometry, bool) {
	for _, g := range d64Geometries {
		if size == g.size || size == g.size+g.size/d64SectorSize {
			return g, true
		}
	}
	return d64Geometry{}, false
}

type d64Image struct {
	data []byte
	geo  d64Geometry
}

// sector returns track/sector (track 1-based; D71's second side is
// tracks 36-70).
func (d *d64Image) sector(track, sector int) ([]byte, error) {
	maxTrack := d.geo.tracks * d.geo.sides
	if track < 1 || track > maxTrack {
		return nil, fmt.Errorf("D64: track %d out of range", track)
	}
	sideTrack := (track-1)%d.geo.tracks + 1
	if sector < 0 || sector >= d64SectorsPerTrack(sideTrack) {
		return nil, fmt.Errorf("D64: sector %d/%d out of range", track, sector)
	}
	n := 0
	for t := 1; t < track; t++ {
		n += d64SectorsPerTrack((t-1)%d.geo.tracks + 1)
	}
	off := (n + sector) * d64SectorSize
	return d.data[off : off+d64SectorSize], nil
}

// isD64 reports whether r (of the given size) looks like a D64/D71 image:
// a supported size, and a BAM that points at the directory track and
// has DOS version 'A'.
func isD64(r io.ReaderAt, size int64) bool {
	g, ok := d64GeometryOf(size)
	if !ok {
		return false
	}
	bamOff := int64(0)
	for t := 1; t < g.dirTrack; t++ {
		bamOff += int64(d64SectorsPerTrack(t)) * d64SectorSize
	}
	bam := make([]byte, 3)
	if _, err := r.ReadAt(bam, bamOff); err != nil {
		return false
	}
	return int(bam[0]) == g.dirTrack && bam[2] == 'A'
}

// readD64 returns the files in a D64/D71 image.
func readD64(data []byte) ([]archiveEntry, error) {
	g, ok := d64GeometryOf(int64(len(data)))
	if !ok {
		return nil, fmt.Errorf("D64: unsupported image size %d", len(data))
	}
	d := &d64Image{data: data, geo: g}
	bam, err := d.sector(g.dirTrack, 0)
	if err != nil {
		return nil, err
	}
	if int(bam[0]) != g.dirTrack || bam[2] != 'A' {
		return nil, fmt.Errorf("D64: no valid BAM at track %d", g.dirTrack)
	}
	var res []archiveEntry
	seenDir := map[[2]int]bool{}
	for t, s := int(bam[0]), int(bam[1]); t != 0; {
		if seenDir[[2]int{t, s}] {
			return nil, fmt.Errorf("D64: directory sector chain loops at %d/%d", t, s)
		}
		seenDir[[2]int{t, s}] = true
		dir, err := d.sector(t, s)
		if err != nil {
			return nil, err
		}
		for i := range 8 {
			e := dir[32*i : 32*(i+1)]
			typ := e[2]
			// Skip deleted entries (type 0) and unclosed
			// ("splat") files, which are incomplete.
			if typ&0x80 == 0 || typ&0x0f == 0 {
				continue
			}
			name := petsciiName(e[5:21])
			h, err := d.fileHash(int(e[3]), int(e[4]))
			if err != nil {
				return nil, fmt.Errorf("D64: %v: %w", name, err)
			}
			res = append(res, archiveEntry{name: name, hash: h})
		}
		t, s = int(dir[0]), int(dir[1])
	}
	return res, nil
}

// fileHash returns the hash of the file data in the sector chain starting
// at track/sector.
func (d *d64Image) fileHash(t, s int) ([sha256.Size]byte, error) {
	h := sha256.New()
	seen := map[[2]int]bool{}
	for t != 0 {
		if seen[[2]int{t, s}] {
			return [sha256.Size]byte{}, fmt.Errorf("sector chain loops at %d/%d", t, s)
		}
		seen[[2]int{t, s}] = true
		sec, err := d.sector(t, s)
		if err != nil {
			return [sha256.Size]byte{}, err
		}
		t, s = int(sec[0]), int(sec[1])
		if t == 0 {
			// Last sector: s is the index of the last used byte.
			if s >= 2 {
				h.Write(sec[2 : s+1])
			}
			break
		}
		h.Write(sec[2:])
	}
	var res [sha256.Size]byte
	copy(res[:], h.Sum(nil))
	return res, nil
}

// petsciiName decodes a 16-byte, 0xA0-padded PETSCII file name the way
// c1541 does: unshifted letters (0x41-0x5A) become lowercase ASCII,
// shifted ones (0xC1-0xDA) uppercase, and other bytes outside printable
// ASCII are kept as \xNN escapes so names stay unambiguous.
func petsciiName(b []byte) string {
	var sb strings.Builder
	for _, c := range b {
		switch {
		case c == 0xa0:
			// padding (shifted space)
		case c >= 0x41 && c <= 0x5a:
			sb.WriteByte(c + 0x20)
		case c >= 0xc1 && c <= 0xda:
			sb.WriteByte(c - 0x80)
		case c >= 0x20 && c <= 0x40, c == 0x5b, c == 0x5d:
			sb.WriteByte(c)
		default:
			fmt.Fprintf(&sb, `\x%02x`, c)
		}
	}
	return sb.String()
}
