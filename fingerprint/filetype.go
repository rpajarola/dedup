package fingerprint

import (
	"os"

	"github.com/h2non/filetype"
)

func init() {
	filetype.AddMatcher(filetype.NewType("mp2t", "video/mp2t"), mp2tMatcher)
}

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
