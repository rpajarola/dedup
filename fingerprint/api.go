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
	Init(filename string) error
	Get() ([]Fingerprint, error)
}

var fingerprinters = []Fingerprinter{}

// XMPFingerprinter,
// PerceptionHashFingerprinter,

func GetFingerprint(filename string) ([]Fingerprint, error) {
	var err error
	var res []Fingerprint
	for _, fp := range fingerprinters {
		if e := fp.Init(filename); e != nil {
			err = errors.Join(err, e)
			continue
		}
		f, e := fp.Get()
		res = append(res, f...)
		err = errors.Join(err, e)
	}
	return res, err
}
