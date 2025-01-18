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
			if e := fp.Init(tc.SourceFile); e != nil {
				t.Fatalf("fp.Init(%v): %v", tc.SourceFile, e)
			}
			if fp.xmp == nil {
				tc.Got.Xmp.Comment = []string{"No XMP data"}
			} else {
				tc.Got.Xmp.WantDocumentId, _, _ = fp.getDocumentID()
			}
			maybeUpdateTestCase(t, tc)
		})
	}
}
