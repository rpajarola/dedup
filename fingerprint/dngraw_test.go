package fingerprint

import (
	"image"
	"math/bits"
	"os"
	"path/filepath"
	"testing"

	azr "github.com/azr/phash"
)

// TestDNGRawRenderMatchesJPEG checks that rendering a preview-less DNG's
// raw data gives a perceptual hash close to that of the camera's own JPEG
// of the same shot, and far from an unrelated image.
func TestDNGRawRenderMatchesJPEG(t *testing.T) {
	t.Parallel()
	decode := func(name string) image.Image {
		t.Helper()
		fn := filepath.Join(testDataDir, name)
		data, err := os.ReadFile(fn)
		if err != nil {
			t.Skipf("missing testdata: %v", err)
		}
		if filepath.Ext(name) == ".dng" {
			img, err := decodeDNGRaw(data)
			if err != nil {
				t.Fatalf("decodeDNGRaw(%v): %v", name, err)
			}
			return img
		}
		f, err := os.Open(fn)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		img, _, err := image.Decode(f)
		if err != nil {
			t.Fatalf("image.Decode(%v): %v", name, err)
		}
		return img
	}
	unrelated := azr.DTC(decode("imgphash_cat_sky.jpg"))
	for _, base := range []string{"IPHON8PLUShSLI0020NRD-IMG_0218", "IPHON8PLUShSLI0040NRD-IMG_0219"} {
		raw := azr.DTC(decode(base + ".dng"))
		jpg := azr.DTC(decode(base + ".jpeg"))
		if d := bits.OnesCount64(raw ^ jpg); d > 6 {
			t.Errorf("%v: Hamming distance between raw render and camera JPEG = %d, want <= 6", base, d)
		}
		if d := bits.OnesCount64(raw ^ unrelated); d < 20 {
			t.Errorf("%v: Hamming distance between raw render and an unrelated image = %d, want >= 20", base, d)
		}
	}
}
