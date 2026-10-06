package fingerprint

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"io"
)

// decodeContainerPreview locates and decodes the preview image embedded in
// a RAW format whose container isn't EXIF/TIFF-based at all, so neither
// decodeImage nor decodeEmbeddedPreview applies: Fujifilm RAF, Sigma X3F
// and Canon CR3. r is read in full, since both formats need to inspect more than
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
	if jpg, err := cr3Preview(data); err == nil {
		return ipfps.decodeImage(bytes.NewReader(jpg))
	}
	// Last resort for DNGs without any embedded preview: render the raw
	// sensor data.
	if img, err := decodeDNGRaw(data); err == nil {
		b := img.Bounds()
		ipfps.cfg = image.Config{ColorModel: img.ColorModel(), Width: b.Dx(), Height: b.Dy()}
		ipfps.img = img
		return nil
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

// cr3PreviewUUID and cr3MetaUUID identify Canon's top-level preview box
// and its metadata box inside "moov" in a CR3 file.
var (
	cr3PreviewUUID = []byte{0xea, 0xf4, 0x2b, 0x5e, 0x1c, 0x98, 0x4b, 0x88, 0xb9, 0xfb, 0xb7, 0xdc, 0x40, 0x6e, 0x4d, 0x16}
	cr3MetaUUID    = []byte{0x85, 0xc0, 0xb6, 0x87, 0x82, 0x0f, 0x11, 0xe0, 0x81, 0x11, 0xf4, 0xce, 0x46, 0x2b, 0x6a, 0x48}
)

// cr3Preview returns the embedded JPEG preview from a Canon CR3 file's
// data. CR3 is an ISO-BMFF (MP4-like) container: a top-level "uuid" box
// (cr3PreviewUUID) holds, after 8 unknown bytes, a "PRVW" box with a
// medium-sized (e.g. 1620x1080) JPEG; "moov" holds another "uuid" box
// (cr3MetaUUID) whose "THMB" box has a 160x120 thumbnail, used as a
// fallback. Both PRVW and THMB start with a 16-byte header (4 unknown
// bytes, 2-byte unknown, 2-byte width, 2-byte height, 2-byte unknown,
// 4-byte JPEG length; all big-endian), followed by the JPEG itself.
// (The full-resolution JPEG in the first "trak" isn't used: the preview
// is plenty for perceptual hashing.)
func cr3Preview(data []byte) ([]byte, error) {
	if len(data) < 12 || string(data[4:12]) != "ftypcrx " {
		return nil, fmt.Errorf("not a CR3 file")
	}
	if b, ok := bmffFindUUID(data, cr3PreviewUUID); ok && len(b) > 8 {
		if jpg, err := cr3JPEGBox(b[8:], "PRVW"); err == nil {
			return jpg, nil
		}
	}
	if moov, ok := bmffFind(data, "moov"); ok {
		if meta, ok := bmffFindUUID(moov, cr3MetaUUID); ok {
			if jpg, err := cr3JPEGBox(meta, "THMB"); err == nil {
				return jpg, nil
			}
		}
	}
	return nil, fmt.Errorf("CR3: no JPEG preview found")
}

// cr3JPEGBox returns the JPEG in the PRVW/THMB box named typ in data.
func cr3JPEGBox(data []byte, typ string) ([]byte, error) {
	b, ok := bmffFind(data, typ)
	if !ok || len(b) < 16 {
		return nil, fmt.Errorf("CR3: no %v box", typ)
	}
	n := binary.BigEndian.Uint32(b[12:16])
	if uint64(n) > uint64(len(b)-16) {
		return nil, fmt.Errorf("CR3: %v JPEG length %d out of bounds", typ, n)
	}
	return b[16 : 16+n], nil
}

// bmffBoxes calls f with the type and payload of each ISO-BMFF box in
// data, until f returns false.
func bmffBoxes(data []byte, f func(typ string, payload []byte) bool) {
	for len(data) >= 8 {
		size := uint64(binary.BigEndian.Uint32(data[:4]))
		typ := string(data[4:8])
		hdr := uint64(8)
		switch size {
		case 0:
			size = uint64(len(data))
		case 1:
			if len(data) < 16 {
				return
			}
			size, hdr = binary.BigEndian.Uint64(data[8:16]), 16
		}
		if size < hdr || size > uint64(len(data)) {
			return
		}
		if !f(typ, data[hdr:size]) {
			return
		}
		data = data[size:]
	}
}

// bmffFind returns the payload of the first box of type typ in data.
func bmffFind(data []byte, typ string) (res []byte, found bool) {
	bmffBoxes(data, func(t string, payload []byte) bool {
		if t == typ {
			res, found = payload, true
		}
		return !found
	})
	return res, found
}

// bmffFindUUID returns the payload (after the 16-byte UUID) of the first
// "uuid" box with the given UUID in data.
func bmffFindUUID(data []byte, uuid []byte) (res []byte, found bool) {
	bmffBoxes(data, func(t string, payload []byte) bool {
		if t == "uuid" && len(payload) >= 16 && bytes.Equal(payload[:16], uuid) {
			res, found = payload[16:], true
		}
		return !found
	})
	return res, found
}
