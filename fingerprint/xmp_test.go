package fingerprint

import (
	"path/filepath"
	"testing"
)

func TestXMPFingerprinter(t *testing.T) {
	fp := &XMPFingerprinter{}
	for _, tc := range getTestCases(t) {
		t.Run(filepath.Base(tc.Name), func(t *testing.T) {
			if tc.GetXmp() == nil || tc.Xmp.Skip {
				t.Skip()
			}
			if e := fp.Init(tc.SourceFile); e != nil {
				t.Fatalf("fp.Init(%v): %v", tc.SourceFile, e)
			}
			documentID, _, _ := fp.getDocumentID()
			if documentID != tc.Xmp.WantDocumentId {
				t.Errorf("unexpected Document ID, got %q, want %q", documentID, tc.Xmp.WantDocumentId)
			}
		})
	}
}
