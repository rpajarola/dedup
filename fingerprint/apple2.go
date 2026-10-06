package fingerprint

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"path"
	"strings"
)

// Reader for Apple II disk images with a DOS 3.3 or ProDOS filesystem:
// 140K 5.25" images in either DOS 3.3 sector order (.dsk, .do) or ProDOS
// block order (.po, and some .dsk), and 800K ProDOS images (.po). The
// order isn't recorded anywhere, so all combinations are tried and the
// first one whose catalog/volume directory looks valid wins.

const (
	a2Size140K = 35 * 16 * 256
	a2Size800K = 1600 * 512
)

// a2ProDOSSector maps the position of a 256-byte half of a ProDOS block
// within a track (block%8*2 + half) to the DOS 3.3 logical sector stored
// there; since the mapping is its own inverse, it also maps a DOS 3.3
// sector to its position in a ProDOS-order image.
var a2ProDOSSector = [16]int{0, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1, 15}

type a2Disk struct {
	data        []byte
	prodosOrder bool
}

// sector returns DOS 3.3 track/sector t/s.
func (d *a2Disk) sector(t, s int) ([]byte, error) {
	if t < 0 || t >= 35 || s < 0 || s >= 16 || len(d.data) != a2Size140K {
		return nil, fmt.Errorf("Apple II: track/sector %d/%d out of range", t, s)
	}
	if d.prodosOrder {
		s = a2ProDOSSector[s]
	}
	off := (t*16 + s) * 256
	return d.data[off : off+256], nil
}

// block returns ProDOS block n (512 bytes; a fresh copy for DOS-order
// images, whose blocks aren't contiguous).
func (d *a2Disk) block(n int) ([]byte, error) {
	if n < 0 || (n+1)*512 > len(d.data) {
		return nil, fmt.Errorf("Apple II: block %d out of range", n)
	}
	if d.prodosOrder {
		return d.data[n*512 : (n+1)*512], nil
	}
	b := make([]byte, 0, 512)
	for half := range 2 {
		s := a2ProDOSSector[n%8*2+half]
		off := (n/8*16 + s) * 256
		b = append(b, d.data[off:off+256]...)
	}
	return b, nil
}

// isApple2 reports whether r (of the given size) looks like an Apple II
// DOS 3.3 or ProDOS disk image.
func isApple2(r io.ReaderAt, size int64) bool {
	if size != a2Size140K && size != a2Size800K {
		return false
	}
	data := make([]byte, size)
	if _, err := r.ReadAt(data, 0); err != nil {
		return false
	}
	_, _, ok := a2Detect(data)
	return ok
}

// a2Detect returns the disk's sector order and whether it has a ProDOS
// (rather than DOS 3.3) filesystem.
//
// A DOS 3.3 disk's VTOC (track 17 sector 0) and usually its first
// catalog sector (sector 15) are at the same place in both orders, so
// the order is chosen by how plausible the whole catalog looks in each.
func a2Detect(data []byte) (d *a2Disk, prodos bool, ok bool) {
	if len(data) == a2Size800K {
		d := &a2Disk{data: data, prodosOrder: true}
		return d, true, a2ValidProDOS(d)
	}
	for _, order := range []bool{false, true} {
		if d := (&a2Disk{data: data, prodosOrder: order}); a2ValidProDOS(d) {
			return d, true, true
		}
	}
	var best *a2Disk
	bestScore := 0
	for _, order := range []bool{false, true} {
		d := &a2Disk{data: data, prodosOrder: order}
		if !a2ValidDOS33(d) {
			continue
		}
		if score := a2DOS33CatalogScore(d); score > bestScore {
			best, bestScore = d, score
		}
	}
	return best, false, best != nil
}

// a2DOS33CatalogScore walks the DOS 3.3 catalog and returns the number of
// plausible entries (track/sector list in range, name in high-bit ASCII)
// minus the number of implausible ones.
func a2DOS33CatalogScore(d *a2Disk) int {
	vtoc, _ := d.sector(17, 0)
	score := 0
	seen := map[[2]int]bool{}
	for t, s := int(vtoc[1]), int(vtoc[2]); t != 0 && len(seen) < 32; {
		if seen[[2]int{t, s}] {
			break
		}
		seen[[2]int{t, s}] = true
		cat, err := d.sector(t, s)
		if err != nil {
			return score - 1
		}
		for i := range 7 {
			e := cat[0x0b+35*i : 0x0b+35*(i+1)]
			if e[0] == 0 || e[0] == 0xff {
				continue
			}
			ok := e[0] < 35 && e[1] < 16
			for _, c := range e[3:33] {
				ok = ok && c >= 0xa0
			}
			// The entry's track/sector list sector is usually
			// at a different place in the two orders, so check
			// that it looks like one: in-range pairs, at least
			// one used, and none after the first unused one.
			if ok {
				ok = a2ValidTSList(d, int(e[0]), int(e[1]))
			}
			if ok {
				score++
			} else {
				score--
			}
		}
		t, s = int(cat[1]), int(cat[2])
	}
	return score
}

func a2ValidTSList(d *a2Disk, t, s int) bool {
	tsl, err := d.sector(t, s)
	if err != nil || tsl[1] >= 35 || tsl[2] >= 16 {
		return false
	}
	used, ended := 0, false
	for i := 0x0c; i+1 < 256; i += 2 {
		switch {
		case tsl[i] == 0 && tsl[i+1] == 0:
			ended = true
		case ended || tsl[i] >= 35 || tsl[i+1] >= 16:
			return false
		default:
			used++
		}
	}
	return used > 0
}

func a2ValidDOS33(d *a2Disk) bool {
	vtoc, err := d.sector(17, 0)
	if err != nil {
		return false
	}
	return vtoc[1] >= 1 && vtoc[1] < 35 && vtoc[2] < 16 && vtoc[3] == 3 &&
		vtoc[0x34] == 35 && vtoc[0x35] == 16 && binary.LittleEndian.Uint16(vtoc[0x36:]) == 256
}

func a2ValidProDOS(d *a2Disk) bool {
	b, err := d.block(2)
	if err != nil {
		return false
	}
	return binary.LittleEndian.Uint16(b[0:]) == 0 && b[4]>>4 == 0xf && b[4]&0xf != 0 &&
		b[4+0x1f] == 0x27 && b[4+0x20] == 0x0d
}

// readApple2 returns the files in an Apple II disk image.
func readApple2(data []byte) ([]archiveEntry, error) {
	d, prodos, ok := a2Detect(data)
	if !ok {
		return nil, fmt.Errorf("Apple II: no DOS 3.3 or ProDOS filesystem found")
	}
	if prodos {
		var res []archiveEntry
		err := d.readProDOSDir(2, "", 0, &res)
		return res, err
	}
	return d.readDOS33()
}

// readDOS33 reads a DOS 3.3 catalog. File contents are as the program
// sees them: binary (B) files without their 4-byte address/length header,
// BASIC (A, I) files without their 2-byte length, text (T) files up to
// the first 0x00, and other types as stored.
func (d *a2Disk) readDOS33() ([]archiveEntry, error) {
	vtoc, _ := d.sector(17, 0)
	var res []archiveEntry
	seen := map[[2]int]bool{}
	for t, s := int(vtoc[1]), int(vtoc[2]); t != 0; {
		if seen[[2]int{t, s}] {
			return nil, fmt.Errorf("DOS 3.3: catalog loops at %d/%d", t, s)
		}
		seen[[2]int{t, s}] = true
		cat, err := d.sector(t, s)
		if err != nil {
			return nil, err
		}
		for i := range 7 {
			e := cat[0x0b+35*i : 0x0b+35*(i+1)]
			if e[0] == 0 || e[0] == 0xff { // unused / deleted
				continue
			}
			var nb []byte
			for _, c := range e[3:33] {
				nb = append(nb, c&0x7f)
			}
			name := strings.TrimRight(string(nb), " ")
			content, err := d.dos33File(int(e[0]), int(e[1]))
			if err != nil {
				return nil, fmt.Errorf("DOS 3.3: %v: %w", name, err)
			}
			switch e[2] & 0x7f {
			case 0x00: // text
				if i := strings.IndexByte(string(content), 0); i >= 0 {
					content = content[:i]
				}
			case 0x01, 0x02: // Integer/Applesoft BASIC
				if len(content) >= 2 {
					n := int(binary.LittleEndian.Uint16(content))
					content = content[2:min(2+n, len(content))]
				}
			case 0x04: // binary
				if len(content) >= 4 {
					n := int(binary.LittleEndian.Uint16(content[2:]))
					content = content[4:min(4+n, len(content))]
				}
			}
			res = append(res, archiveEntry{name: name, hash: sha256.Sum256(content)})
		}
		t, s = int(cat[1]), int(cat[2])
	}
	return res, nil
}

// dos33File returns the sectors listed by the track/sector list chain
// starting at t/s.
func (d *a2Disk) dos33File(t, s int) ([]byte, error) {
	var content []byte
	seen := map[[2]int]bool{}
	for t != 0 {
		if seen[[2]int{t, s}] {
			return nil, fmt.Errorf("track/sector list loops at %d/%d", t, s)
		}
		seen[[2]int{t, s}] = true
		tsl, err := d.sector(t, s)
		if err != nil {
			return nil, err
		}
		for i := 0x0c; i+1 < 256; i += 2 {
			dt, ds := int(tsl[i]), int(tsl[i+1])
			if dt == 0 {
				break
			}
			sec, err := d.sector(dt, ds)
			if err != nil {
				return nil, err
			}
			content = append(content, sec...)
		}
		t, s = int(tsl[1]), int(tsl[2])
	}
	return content, nil
}

// readProDOSDir reads the ProDOS directory starting at key block key.
func (d *a2Disk) readProDOSDir(key int, prefix string, depth int, res *[]archiveEntry) error {
	if depth > 32 {
		return fmt.Errorf("ProDOS: directories nested too deeply at %q", prefix)
	}
	seen := map[int]bool{}
	first := true
	for blk := key; blk != 0; {
		if seen[blk] {
			return fmt.Errorf("ProDOS: directory block chain loops at %d", blk)
		}
		seen[blk] = true
		b, err := d.block(blk)
		if err != nil {
			return err
		}
		for i := range 13 {
			if first && i == 0 {
				continue // directory header
			}
			e := b[4+0x27*i : 4+0x27*(i+1)]
			storage, nameLen := e[0]>>4, int(e[0]&0x0f)
			if storage == 0 {
				continue // deleted
			}
			name := path.Join(prefix, prodosName(e[1:1+nameLen], binary.LittleEndian.Uint16(e[0x1c:])))
			keyPtr := int(binary.LittleEndian.Uint16(e[0x11:]))
			eof := int(e[0x15]) | int(e[0x16])<<8 | int(e[0x17])<<16
			switch storage {
			case 0xd: // subdirectory
				if err := d.readProDOSDir(keyPtr, name, depth+1, res); err != nil {
					return err
				}
			case 1, 2, 3: // seedling, sapling, tree
				h, err := d.prodosFileHash(storage, keyPtr, eof)
				if err != nil {
					return fmt.Errorf("ProDOS: %v: %w", name, err)
				}
				*res = append(*res, archiveEntry{name: name, hash: h})
			case 5: // extended (GS/OS forked) file: use the data fork
				ext, err := d.block(keyPtr)
				if err != nil {
					return err
				}
				dataEOF := int(ext[5]) | int(ext[6])<<8 | int(ext[7])<<16
				h, err := d.prodosFileHash(ext[0]&0x0f, int(binary.LittleEndian.Uint16(ext[1:])), dataEOF)
				if err != nil {
					return fmt.Errorf("ProDOS: %v: %w", name, err)
				}
				*res = append(*res, archiveEntry{name: name, hash: h})
			}
		}
		first = false
		blk = int(binary.LittleEndian.Uint16(b[2:]))
	}
	return nil
}

// prodosFileHash returns the hash of the first eof bytes of a seedling,
// sapling or tree file. Block number 0 in an index means a sparse
// (all-zero) block.
func (d *a2Disk) prodosFileHash(storage byte, key, eof int) ([sha256.Size]byte, error) {
	var blocks []int
	indexBlocks := func(idx int) ([]int, error) {
		b, err := d.block(idx)
		if err != nil {
			return nil, err
		}
		var res []int
		for i := range 256 {
			res = append(res, int(b[i])|int(b[256+i])<<8)
		}
		return res, nil
	}
	switch storage {
	case 1:
		blocks = []int{key}
	case 2:
		var err error
		if blocks, err = indexBlocks(key); err != nil {
			return [sha256.Size]byte{}, err
		}
	case 3:
		master, err := indexBlocks(key)
		if err != nil {
			return [sha256.Size]byte{}, err
		}
		for _, idx := range master[:128] {
			if idx == 0 {
				blocks = append(blocks, make([]int, 256)...)
				continue
			}
			sub, err := indexBlocks(idx)
			if err != nil {
				return [sha256.Size]byte{}, err
			}
			blocks = append(blocks, sub...)
		}
	default:
		return [sha256.Size]byte{}, fmt.Errorf("unsupported storage type %d", storage)
	}
	h := sha256.New()
	zero := make([]byte, 512)
	for _, n := range blocks {
		if eof <= 0 {
			break
		}
		b := zero
		if n != 0 {
			var err error
			if b, err = d.block(n); err != nil {
				return [sha256.Size]byte{}, err
			}
		}
		k := min(eof, 512)
		h.Write(b[:k])
		eof -= k
	}
	if eof > 0 {
		return [sha256.Size]byte{}, fmt.Errorf("file is %d bytes short", eof)
	}
	var res [sha256.Size]byte
	copy(res[:], h.Sum(nil))
	return res, nil
}

// prodosName returns a ProDOS file name, applying GS/OS lowercase flags
// (GS/OS Technical Note #8): if bit 15 of the little-endian word at entry
// offset 0x1C is set, bits 14..0 say which of the (up to 15) characters,
// starting with the first, are lowercase.
func prodosName(b []byte, caseBits uint16) string {
	// Names are 7-bit ASCII; ignore stray high bits (as AppleCommander
	// does).
	name := make([]byte, len(b))
	for i, c := range b {
		name[i] = c & 0x7f
	}
	if caseBits&0x8000 != 0 {
		for i := range name {
			if caseBits&(0x4000>>i) != 0 && name[i] >= 'A' && name[i] <= 'Z' {
				name[i] += 'a' - 'A'
			}
		}
	}
	return string(name)
}
