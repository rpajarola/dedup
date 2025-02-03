package fingerprint

import (
	"os"

	"github.com/h2non/filetype"
)

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
