package fingerprint

import (
	"fmt"
	"os"

	"github.com/h2non/filetype"
)

type FileTypeFingerprinter struct{}

type fileTypeFingerprinterState struct {
	mimeType string
}

func init() {
	filetype.AddMatcher(filetype.NewType("mp2t", "video/mp2t"), mp2tMatcher)
	// RAW camera formats that are structurally TIFF but use a
	// non-standard marker so that baseline TIFF readers won't
	// misinterpret their raw sensor data (see exiftools' tiff package).
	// h2non/filetype's own Tiff matcher only recognizes the standard
	// marker, so these need their own matchers to be recognized as
	// images at all.
	filetype.AddMatcher(filetype.NewType("orf", "image/x-olympus-orf"), orfMatcher)
	filetype.AddMatcher(filetype.NewType("rw2", "image/x-panasonic-rw2"), rw2Matcher)
	filetype.AddMatcher(filetype.NewType("raf", "image/x-fujifilm-raf"), rafMatcher)
	filetype.AddMatcher(filetype.NewType("x3f", "image/x-sigma-x3f"), x3fMatcher)
	fingerprinters = append(fingerprinters, &FileTypeFingerprinter{})
}

func (ftfp *FileTypeFingerprinter) Init(filename string) (FingerprinterState, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("Open(%v): %w", filename, err)
	}
	defer f.Close()
	ftfps := fileTypeFingerprinterState{
		mimeType: getFiletype(f),
	}
	return &ftfps, nil
}

func (ftfps *fileTypeFingerprinterState) Get() ([]Fingerprint, error) {
	fp := Fingerprint{
		Kind:    "mimetype",
		Hash:    ftfps.mimeType,
		Quality: 10,
	}

	return []Fingerprint{fp}, nil

}

func (ftfps *fileTypeFingerprinterState) Cleanup() {}

func getFiletype(f *os.File) string {
	f.Seek(0, 0)
	// We only have to pass the file header = first 261 bytes
	head := make([]byte, 261)
	f.Read(head)
	f.Seek(0, 0)
	kind, err := filetype.Match(head)
	if err != nil {
		return ""
	}
	return kind.MIME.Value
}

// orfMatcher matches Olympus ORF raw images: "II" + 'R''O' (little-endian
// variant; big-endian "MM"+'O''R' also exists in the wild but hasn't been
// observed in practice here).
func orfMatcher(buf []byte) bool {
	return len(buf) > 3 && buf[0] == 'I' && buf[1] == 'I' && buf[2] == 'R' && buf[3] == 'O'
}

// rw2Matcher matches Panasonic RW2 raw images: "II" + 0x55 0x00.
func rw2Matcher(buf []byte) bool {
	return len(buf) > 3 && buf[0] == 'I' && buf[1] == 'I' && buf[2] == 0x55 && buf[3] == 0x00
}

// rafMatcher matches Fujifilm RAF raw images, which start with the fixed
// 16-byte ASCII string "FUJIFILMCCD-RAW ".
func rafMatcher(buf []byte) bool {
	return len(buf) >= 16 && string(buf[:16]) == "FUJIFILMCCD-RAW "
}

// x3fMatcher matches Sigma X3F raw images (Foveon sensor), which start with
// the 4-byte ASCII magic "FOVb".
func x3fMatcher(buf []byte) bool {
	return len(buf) >= 4 && string(buf[:4]) == "FOVb"
}

// Match MPEG-2 Transport Stream
// 2 varieties:
// MPEG-2 TS: header is 0x4740 (bitmask 0xFF40)
// BDAV: same but extra 4 byte BDAV header with no distinguishing features
func mp2tMatcher(buf []byte) bool {
	if len(buf) < 198 {
		// too short to contain at least 2 packets
		return false
	}
	if buf[0] == 0x47 && buf[1]&0x40 == 0x40 && buf[188] == 0x47 && buf[189]&0x40 == 0x40 {
		return true
	}
	if buf[4] == 0x47 && buf[5]&0x40 == 0x40 && buf[196] == 0x47 && buf[197]&0x40 == 0x40 {
		return true
	}
	return false
}
