package fingerprint

import (
	"path/filepath"
	"testing"
)

func TestXMPFingerprinter(t *testing.T) {
	fp := &XMPFingerprinter{}
	for _, tc := range getTestCases(t, testDataDir, largeTestDataDir) {
		t.Run(filepath.Base(tc.Name), func(t *testing.T) {
			if tc.Got.GetXmp() == nil {
				tc.Got.Xmp = &XMPTestCase{}
			}
			if tc.Got.Xmp.Skip {
				t.Skip()
			}
			fps, e := fp.Init(tc.SourceFile)
			if e != nil {
				t.Fatalf("fp.Init(%v): %v", tc.SourceFile, e)
			}
			if fps == nil {
				tc.Got.Xmp.Comment = []string{"No XMP data"}
			} else {
				xfps := fps.(*xmpFingerprinterState)
				if xfps.xmp == nil {
				} else {
					tc.Got.Xmp.WantDocumentId, _, _ = xfps.getDocumentID()
				}
			}
			maybeUpdateTestCase(t, tc)
		})
	}
}
