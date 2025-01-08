package fingerprint

import (
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
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

			gotTc := proto.Clone(tc).(*FingerprintTestCase)

			if e := fp.Init(tc.SourceFile); e != nil {
				t.Fatalf("fp.Init(%v): %v", tc.SourceFile, e)
			}
			documentID, _, _ := fp.getDocumentID()
			gotTc.Xmp = &XMPTestCase{
				WantDocumentId: documentID,
			}
			got := prototext.Format(gotTc)
			want := prototext.Format(tc)
			if diff := cmp.Diff(got, want, protocmp.Transform()); diff != "" {
				t.Errorf("Unexpected test result, +=want, -=got:\n\n%v", diff)
				updateTestCase(t, gotTc)
			}
		})
	}
}
