package fingerprint

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

// decodeContainerPreview locates and decodes the preview image embedded in
// a RAW format whose container isn't EXIF/TIFF-based at all, so neither
// decodeImage nor decodeEmbeddedPreview applies: Fujifilm RAF and Sigma
// X3F. r is read in full, since both formats need to inspect more than
// just a small header (RAF's preview is sized relative to the rest of the
// file; X3F's directory lives at the very end of the file).
func (ipfps *imgPHashFingerprinterState) decodeContainerPreview(r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if jpg, err := rafPreview(data); err == nil {
		return ipfps.decodeImage(bytes.NewReader(jpg))
	}
	if jpg, err := x3fPreview(data); err == nil {
		return ipfps.decodeImage(bytes.NewReader(jpg))
	}
	return fmt.Errorf("no known RAW container preview found")
}

// rafMagic is the fixed 16-byte ASCII header every Fujifilm RAF file
// starts with.
var rafMagic = []byte("FUJIFILMCCD-RAW ")

// rafPreview returns the embedded JPEG preview from a Fujifilm RAF file's
// data. RAF wraps a complete, independent JPEG (with its own EXIF data)
// directly in its header region, ahead of the raw sensor data; across RAF
// format versions this has consistently been found 148 bytes in, but
// rather than trust a fixed offset across all versions/cameras, this scans
// for the embedded JPEG's own SOI+APP1 marker within the header region.
// The returned slice runs to the end of the file (into the trailing raw
// sensor data) because RAF's own header fields that claim to give the
// preview's exact length have been observed not to line up with where the
// JPEG's EOI actually falls; decodeImage only reads up to the JPEG's own
// EOI marker and ignores trailing bytes, so this is harmless.
func rafPreview(data []byte) ([]byte, error) {
	if len(data) < len(rafMagic) || !bytes.Equal(data[:len(rafMagic)], rafMagic) {
		return nil, fmt.Errorf("not a RAF file")
	}
	head := data
	if len(head) > 4096 {
		head = head[:4096]
	}
	idx := bytes.Index(head, []byte{0xff, 0xd8, 0xff, 0xe1})
	if idx < 0 {
		return nil, fmt.Errorf("RAF: no embedded JPEG preview found")
	}
	return data[idx:], nil
}

// x3fMagic is the 4-byte ASCII magic every Sigma X3F file starts with.
var x3fMagic = []byte("FOVb")

// x3fPreview returns the embedded JPEG preview from a Sigma X3F file's
// data. X3F stores a directory of data blocks at the very end of the
// file, located via a 4-byte little-endian offset in the file's last 4
// bytes; directory entries are 12 bytes each (4-byte offset, 4-byte
// length, 4-byte ASCII type). An "IMA2" entry is an image block with its
// own 28-byte sub-header (4-byte "SECi" magic, 4-byte version, 4-byte
// image type, then width/height/row-size), where image type 2 means the
// block's remaining bytes (after that sub-header) are a plain JPEG.
func x3fPreview(data []byte) ([]byte, error) {
	if len(data) < len(x3fMagic) || !bytes.Equal(data[:len(x3fMagic)], x3fMagic) {
		return nil, fmt.Errorf("not an X3F file")
	}
	const (
		dirEntrySize  = 12
		ima2SubHeader = 28
	)
	if len(data) < 4 {
		return nil, fmt.Errorf("X3F: file too short")
	}
	dirOff := binary.LittleEndian.Uint32(data[len(data)-4:])
	if int64(dirOff)+12 > int64(len(data)) {
		return nil, fmt.Errorf("X3F: directory offset out of bounds")
	}
	if !bytes.Equal(data[dirOff:dirOff+4], []byte("SECd")) {
		return nil, fmt.Errorf("X3F: no directory section found")
	}
	numEntries := binary.LittleEndian.Uint32(data[dirOff+8 : dirOff+12])
	pos := int64(dirOff) + 12
	var best []byte
	for i := uint32(0); i < numEntries; i++ {
		if pos+dirEntrySize > int64(len(data)) {
			break
		}
		entry := data[pos : pos+dirEntrySize]
		off := binary.LittleEndian.Uint32(entry[0:4])
		length := binary.LittleEndian.Uint32(entry[4:8])
		typ := entry[8:12]
		pos += dirEntrySize
		if !bytes.Equal(typ, []byte("IMA2")) {
			continue
		}
		if int64(off)+ima2SubHeader > int64(len(data)) || int64(off)+int64(length) > int64(len(data)) {
			continue
		}
		imgType := binary.LittleEndian.Uint32(data[off+8 : off+12])
		if imgType != 2 {
			continue
		}
		jpg := data[off+ima2SubHeader : off+length]
		if len(jpg) > len(best) {
			best = jpg
		}
	}
	if best == nil {
		return nil, fmt.Errorf("X3F: no JPEG preview block found")
	}
	return best, nil
}
