package fingerprint

import (
	"fmt"
	"io"
	"os"

	"github.com/trimmer-io/go-xmp/xmp"
)

type XMPFingerprinter struct {
	xmp *xmp.Document
}

func init() {
	fingerprinters = append(fingerprinters, &XMPFingerprinter{})
}

func (xfp *XMPFingerprinter) Init(filename string) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	bb, err := xmp.ScanPackets(f)
	if (err != nil && err != io.EOF) || len(bb) == 0 {
		return err
	}
	xfp.xmp = &xmp.Document{}
	if err := xmp.Unmarshal(bb[0], xfp.xmp); err != nil {
		return err
	}
	return nil
}

func (xfp *XMPFingerprinter) Get() ([]Fingerprint, error) {
	if xfp.xmp == nil {
		return nil, nil
	}

	var res []Fingerprint
	for _, f := range []func(*XMPFingerprinter) (Fingerprint, error){
		(*XMPFingerprinter).getDocumentID,
	} {
		if fp, err := f(xfp); err == nil && fp.Hash != "" {
			res = append(res, fp)
		}
	}

	return res, nil
}

func (xfp *XMPFingerprinter) getDocumentID() (Fingerprint, error) {
	//documentID := xfp.xmp.GetDocumentID()
	documentID := 2
	return Fingerprint{
		Kind:    "XMPDocumentID",
		Hash:    fmt.Sprintf("%v", documentID),
		Quality: 100,
	}, nil
}
