package fingerprint

import (
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"os"
)

type ChecksumFingerprinter struct{}

type fphash struct {
	kind    string
	hash    hash.Hash
	quality int
}

type checksumFingerprinterState struct {
	filename string

	fphashes []fphash
}

func init() {
	fingerprinters = append(fingerprinters, &ChecksumFingerprinter{})
}

func (csfp *ChecksumFingerprinter) Init(filename string) (FingerprinterState, error) {
	csfps := checksumFingerprinterState{
		filename: filename,
		fphashes: []fphash{
			{"CRC32", crc32.NewIEEE(), 50},
			{"MD5", md5.New(), 80},
			{"SHA1", sha1.New(), 99},
		},
	}
	return &csfps, nil
}

func (csfps *checksumFingerprinterState) Get() ([]Fingerprint, error) {
	var res []Fingerprint
	f, err := os.Open(csfps.filename)
	if err != nil {
		return nil, fmt.Errorf("Open(%v): %w", csfps.filename, err)
	}

	buf := make([]byte, 32768)
	for {
		n, err := f.Read(buf)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("Read(%v): %w", csfps.filename, err)
		}
		for _, h := range csfps.fphashes {
			h.hash.Write(buf[0:n])
		}
	}

	for _, h := range csfps.fphashes {
		res = append(res, Fingerprint{
			Kind:    h.kind,
			Hash:    hex.EncodeToString(h.hash.Sum(nil)),
			Quality: h.quality,
		})
	}

	return res, nil
}

func (csfps *checksumFingerprinterState) Cleanup() {}
