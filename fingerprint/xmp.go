package fingerprint

import (
	"fmt"
	"io"
	"os"
	"strings"

	_ "github.com/trimmer-io/go-xmp/models"
	"github.com/trimmer-io/go-xmp/xmp"
)

type XMPFingerprinter struct{}

type xmpFingerprinterState struct {
	xmp *xmp.Document
}

func init() {
	fingerprinters = append(fingerprinters, &XMPFingerprinter{})
}

func (xfp *XMPFingerprinter) Init(filename string) (FingerprinterState, error) {
	xfps := xmpFingerprinterState{}
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	bb, err := xmp.ScanPackets(f)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("xmp.ScanPackets: %v", err)
	}
	if len(bb) == 0 {
		return nil, nil
	}
	xfps.xmp = &xmp.Document{}
	if err := xmp.Unmarshal(bb[0], xfps.xmp); err != nil {
		return nil, fmt.Errorf("xmp.Unmarshal: %v", err)
	}
	return &xfps, nil
}

func (xfps *xmpFingerprinterState) Get() ([]Fingerprint, error) {
	if xfps.xmp == nil {
		return nil, nil
	}

	var res []Fingerprint
	for _, f := range []func(*xmpFingerprinterState) (Fingerprint, error){
		(*xmpFingerprinterState).getDocumentIDFP,
	} {
		if fp, err := f(xfps); err == nil && fp.Hash != "" {
			res = append(res, fp)
		}
	}

	return res, nil
}

func (xfps *xmpFingerprinterState) Cleanup() {}

func (xfps *xmpFingerprinterState) getDocumentIDFP() (Fingerprint, error) {
	if xfps.xmp == nil {
		return NoFingerprint, nil
	}
	documentID, isUnique, quality := xfps.getDocumentID()
	if documentID != "" && isUnique && quality == 100 {
		return Fingerprint{
			Kind:    "XMPDocumentID",
			Hash:    documentID,
			Quality: quality,
		}, nil
	}
	return NoFingerprint, nil
}

func (xfps *xmpFingerprinterState) getDocumentID() (string, bool, int) {
	if xfps.xmp == nil {
		return "", false, 0
	}
	for _, path := range []string{
		"xmpMM:OriginalDocumentID",
		"xmpMM:DocumentID",
		"xmpMM:InstanceID",
		"exif:ImageUniqueID",
		"digiKam:ImageUniqueID",
		"iXML:fileUid",
		"qt:ClipID",
		"qt:ContentID",
		"qt:GUID",
	} {
		if v, err := xfps.xmp.GetPath(xmp.Path(path)); err == nil {
			v = strings.TrimPrefix(v, "xmp.did:")
			return v, true, 100
		}
	}

	return "", false, 0
}
