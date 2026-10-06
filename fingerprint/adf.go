package fingerprint

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"path"
	"slices"

	"golang.org/x/text/encoding/charmap"
)

// Reader for Amiga ADF disk images with an AmigaDOS filesystem (OFS or
// FFS, with or without international mode/directory cache), see e.g. the
// "ADF FAQ" (Laurent Clévy). All blocks are 512 bytes; integers are
// big-endian 32-bit "longs".

const (
	adfBlockSize = 512

	adfTypeHeader = 2  // T_HEADER: root, directory and file header blocks
	adfTypeList   = 16 // T_LIST: file extension blocks

	adfSecTypeRoot    = 1
	adfSecTypeUserDir = 2
	adfSecTypeFile    = -3

	adfHashTableSize = 72 // entries in a hash table / data block table
)

// adfDiskSizes are the sizes of double and high density floppy images.
var adfDiskSizes = []int64{80 * 2 * 11 * adfBlockSize, 80 * 2 * 22 * adfBlockSize}

// isADF reports whether r (of the given size) looks like an ADF floppy
// image with an AmigaDOS boot block.
func isADF(r io.ReaderAt, size int64) bool {
	if !slices.Contains(adfDiskSizes, size) {
		return false
	}
	head := make([]byte, 4)
	if _, err := r.ReadAt(head, 0); err != nil {
		return false
	}
	return string(head[:3]) == "DOS" && head[3] <= 7
}

type adfReader struct {
	data []byte
	ffs  bool
	seen map[uint32]bool
}

func (r *adfReader) block(n uint32) ([]byte, error) {
	off := int64(n) * adfBlockSize
	if n < 2 || off+adfBlockSize > int64(len(r.data)) {
		return nil, fmt.Errorf("ADF: block %d out of range", n)
	}
	return r.data[off : off+adfBlockSize], nil
}

func adfLong(b []byte, off int) uint32 { return binary.BigEndian.Uint32(b[off:]) }

// readADF returns the files in an ADF image.
func readADF(data []byte) ([]archiveEntry, error) {
	r := &adfReader{data: data, ffs: data[3]&1 == 1, seen: map[uint32]bool{}}
	rootNum := uint32(len(data) / adfBlockSize / 2)
	root, err := r.block(rootNum)
	if err != nil {
		return nil, err
	}
	if adfLong(root, 0) != adfTypeHeader || int32(adfLong(root, 508)) != adfSecTypeRoot {
		return nil, fmt.Errorf("ADF: no AmigaDOS root block at block %d", rootNum)
	}
	var res []archiveEntry
	err = r.readDir(root, "", 0, &res)
	return res, err
}

// readDir reads the directory whose header (or root) block is dir.
func (r *adfReader) readDir(dir []byte, prefix string, depth int, res *[]archiveEntry) error {
	if depth > 32 {
		return fmt.Errorf("ADF: directories nested too deeply at %q", prefix)
	}
	for i := range adfHashTableSize {
		for n := adfLong(dir, 24+4*i); n != 0; {
			if r.seen[n] {
				return fmt.Errorf("ADF: block %d referenced twice", n)
			}
			r.seen[n] = true
			hdr, err := r.block(n)
			if err != nil {
				return err
			}
			if adfLong(hdr, 0) != adfTypeHeader {
				return fmt.Errorf("ADF: block %d isn't a header block", n)
			}
			name := path.Join(prefix, adfName(hdr))
			switch int32(adfLong(hdr, 508)) {
			case adfSecTypeUserDir:
				if err := r.readDir(hdr, name, depth+1, res); err != nil {
					return err
				}
			case adfSecTypeFile:
				h, err := r.fileHash(hdr)
				if err != nil {
					return fmt.Errorf("ADF: %v: %w", name, err)
				}
				*res = append(*res, archiveEntry{name: name, hash: h})
			}
			// Hard/soft links are skipped, like symlinks elsewhere.
			n = adfLong(hdr, 496) // next entry with the same hash
		}
	}
	return nil
}

// adfName returns the name of a header block (a BCPL string of up to 30
// ISO-8859-1 characters).
func adfName(hdr []byte) string {
	n := min(int(hdr[432]), 30)
	name, err := charmap.ISO8859_1.NewDecoder().Bytes(hdr[433 : 433+n])
	if err != nil {
		return string(hdr[433 : 433+n])
	}
	return string(name)
}

// fileHash returns the content hash of the file whose header block is hdr.
func (r *adfReader) fileHash(hdr []byte) ([sha256.Size]byte, error) {
	size := int64(adfLong(hdr, 324))
	h := sha256.New()
	remaining := size
	// The header and each extension block list up to 72 data blocks,
	// stored in reverse order from the end of the table.
	for blk, ext := hdr, 0; remaining > 0; ext++ {
		if ext > len(r.data)/adfBlockSize {
			return [sha256.Size]byte{}, fmt.Errorf("too many extension blocks")
		}
		count := min(int(adfLong(blk, 8)), adfHashTableSize)
		for i := 0; i < count && remaining > 0; i++ {
			db, err := r.block(adfLong(blk, 24+4*(adfHashTableSize-1-i)))
			if err != nil {
				return [sha256.Size]byte{}, err
			}
			chunk := db
			if !r.ffs {
				// OFS data blocks have a 24-byte header; the
				// payload size is at offset 12.
				chunk = db[24 : 24+min(int(adfLong(db, 12)), adfBlockSize-24)]
			}
			if int64(len(chunk)) > remaining {
				chunk = chunk[:remaining]
			}
			h.Write(chunk)
			remaining -= int64(len(chunk))
		}
		if remaining == 0 {
			break
		}
		next := adfLong(blk, 504)
		if next == 0 {
			return [sha256.Size]byte{}, fmt.Errorf("file is %d bytes short", remaining)
		}
		var err error
		if blk, err = r.block(next); err != nil {
			return [sha256.Size]byte{}, err
		}
		if adfLong(blk, 0) != adfTypeList {
			return [sha256.Size]byte{}, fmt.Errorf("block %d isn't an extension block", next)
		}
	}
	var res [sha256.Size]byte
	copy(res[:], h.Sum(nil))
	return res, nil
}
