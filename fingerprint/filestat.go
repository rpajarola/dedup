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

	// file date
	// name+extension

	return res, nil
}

func (fsfps *filestatFingerprinterState) Cleanup() {}
