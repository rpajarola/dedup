package fingerprint

//go:generate protoc --go_out=. --go_opt=paths=source_relative fingerprint_test.proto

import (
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
)

func TestEXIFFingerprinter(t *testing.T) {
	fp := &EXIFFingerprinter{}
	for _, tc := range getTestCases(t) {
		t.Run(filepath.Base(tc.Name), func(t *testing.T) {
			if tc.Exif.Skip {
				t.Skip()
			}
			gotTc := proto.Clone(tc).(*FingerprintTestCase)

			if e := fp.Init(tc.SourceFile); e != nil {
				t.Fatalf("fp.Init(%v): %v", tc.SourceFile, e)
			}
			photoID, isUnique, _ := fp.getPhotoID()
			gotTc.Exif = &EXIFTestCase{
				WantCameraModel:   fp.getCameraModel(),
				WantCameraSerial:  fp.getCameraSerial(),
				WantPhotoId:       photoID,
				WantUniquePhotoId: isUnique,
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
