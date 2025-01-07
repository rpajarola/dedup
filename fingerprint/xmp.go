package fingerprint

import (
	"fmt"
	"io"
	"os"
	"strings"

	_ "github.com/trimmer-io/go-xmp/models"
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
	if err != nil && err != io.EOF {
		return fmt.Errorf("xmp.ScanPackets: %v", err)
	}
	if len(bb) == 0 {
		return nil
	}
	xfp.xmp = &xmp.Document{}
	if err := xmp.Unmarshal(bb[0], xfp.xmp); err != nil {
		return fmt.Errorf("xmp.Unmarshal: %v", err)
	}
	return nil
}

func (xfp *XMPFingerprinter) Get() ([]Fingerprint, error) {
	if xfp.xmp == nil {
		return nil, nil
	}

	var res []Fingerprint
	for _, f := range []func(*XMPFingerprinter) (Fingerprint, error){
		(*XMPFingerprinter).getDocumentIDFP,
	} {
		if fp, err := f(xfp); err == nil && fp.Hash != "" {
			res = append(res, fp)
		}
	}

	return res, nil
}

func (xfp *XMPFingerprinter) getDocumentIDFP() (Fingerprint, error) {
	if xfp.xmp == nil {
		return NoFingerprint, nil
	}
	documentID, isUnique, quality := xfp.getDocumentID()
	if documentID != "" && isUnique && quality == 100 {
		return Fingerprint{
			Kind:    "XMPDocumentID",
			Hash:    documentID,
			Quality: quality,
		}, nil
	}
	return NoFingerprint, nil
}

func (xfp *XMPFingerprinter) getDocumentID() (string, bool, int) {
	if xfp.xmp == nil {
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
		if v, err := xfp.xmp.GetPath(xmp.Path(path)); err == nil {
			v = strings.TrimPrefix(v, "xmp.did:")
			return v, true, 100
		}
	}

	return "", false, 0
}
