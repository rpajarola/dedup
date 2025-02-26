package fingerprint

import (
	"fmt"
	"os"
)

type FilestatFingerprinter struct{}

type filestatFingerprinterState struct {
	filename string
}

func init() {
	fingerprinters = append(fingerprinters, &FilestatFingerprinter{})
}

func (fsfp *FilestatFingerprinter) Init(filename string) (FingerprinterState, error) {
	fsfps := filestatFingerprinterState{
		filename: filename,
	}
	return &fsfps, nil
}

func (fsfps *filestatFingerprinterState) Get() ([]Fingerprint, error) {
	var res []Fingerprint
	s, err := os.Stat(fsfps.filename)
	if err != nil {
		return nil, fmt.Errorf("Open(%v): %w", fsfps.filename, err)
	}
	res = append(res, Fingerprint{
		Kind:    "filesize",
		Hash:    fmt.Sprintf("%v", s.Size()),
		Quality: 20,
	})
	res = append(res, Fingerprint{
		Kind:    "filedate",
		Hash:    fmt.Sprintf("%v", s.ModTime().Unix()),
		Quality: 20,
	})
	res = append(res, Fingerprint{
		Kind:    "filename",
		Hash:    fsfps.filename,
		Quality: 10,
	})
	ext := ""
	if dot := len(fsfps.filename) - 1; dot >= 0 {
		for i := dot; i >= 0; i-- {
			if fsfps.filename[i] == '.' {
				ext = fsfps.filename[i:]
				break
			}
		}
	}
	res = append(res, Fingerprint{
		Kind:    "fileextension",
		Hash:    ext,
		Quality: 10,
	})

	return res, nil
}

func (fsfps *filestatFingerprinterState) Cleanup() {}
