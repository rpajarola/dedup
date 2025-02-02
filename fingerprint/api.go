package fingerprint

import "errors"

type Fingerprint struct {
	Kind      string // Free form identifier, must be unique per type of fingerprint
	Hash      string // The actual fingerprint. Ideally unique per file
	Quality   int    // confidence that the fingerprint is useful
	Canonical bool   // set by the framework if this is the canonical fingerprint
}

var NoFingerprint = Fingerprint{}

type Fingerprinter interface {
	Init(filename string) (FingerprinterState, error)
}

type FingerprinterState interface {
	Get() ([]Fingerprint, error)
	Cleanup()
}

var fingerprinters = []Fingerprinter{}

func GetFingerprint(filename string) ([]Fingerprint, error) {
	var err error
	var res []Fingerprint
	for _, fp := range fingerprinters {
		fps, e := fp.Init(filename)
		if e != nil {
			err = errors.Join(err, e)
			continue
		}
		if fps == nil {
			continue
		}
		f, e := fps.Get()
		res = append(res, f...)
		err = errors.Join(err, e)
		fps.Cleanup()
	}
	return res, err
}
