package fingerprint

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"os"
	"sort"
	"strings"

	nr90 "github.com/Nr90/imgsim"
	ajdnik "github.com/ajdnik/imghash"
	azr "github.com/azr/phash"
	heif "github.com/jdeng/goheif"
	"github.com/rpajarola/exiftools/exif"
	"github.com/rpajarola/exiftools/mknote"
	"github.com/rpajarola/exiftools/models"
	tiff "golang.org/x/image/tiff"
)

// vendorPreviewImageTags lists maker-note-specific preview-image tags that
// the generic IFD0/thumbnail tags exif.PreviewCandidates always checks
// don't cover, for RAW formats whose real preview lives elsewhere.
var vendorPreviewImageTags = []exif.PreviewImageTag{
	mknote.OlympusPreviewImageTag,
}

// vendorPreviewBlobTags lists "blob" tags that hold a preview image
// directly as their own value, rather than as a start/length pair into the
// rest of the file.
var vendorPreviewBlobTags = []models.FieldName{
	models.PanasonicJpgFromRaw,
}

var (
	extensions = map[string]func(io.Reader) (image.Image, error){
		"gif":  gif.Decode,
		"heic": heif.Decode,
		"heif": heif.Decode,
		"jpeg": jpeg.Decode,
		"jpg":  jpeg.Decode,
		"png":  png.Decode,
		"tiff": tiff.Decode,
	}
)

type ImgPHashFingerprinter struct{}

type imgPHashFingerprinterState struct {
	cfg image.Config
	img image.Image
}

func init() {
	fingerprinters = append(fingerprinters, &ImgPHashFingerprinter{})
}

func (ipfp *ImgPHashFingerprinter) Init(filename string) (FingerprinterState, error) {
	ipfps := imgPHashFingerprinterState{}
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("Open(%v): %w", filename, err)
	}
	defer f.Close()
	if !strings.HasPrefix(getFiletype(f), "image/") {
		return nil, nil
	}
	if err := ipfps.decodeImage(f); err == nil {
		return &ipfps, nil
	}
	// RAW formats (ARW, CR2, CR3, DNG, ...) wrap sensor data the stdlib
	// image package can't decode directly. Fall back to the JPEG/PNG
	// preview or thumbnail embedded in the file's EXIF data.
	if _, err := f.Seek(0, 0); err != nil {
		return nil, nil
	}
	if err := ipfps.decodeEmbeddedPreview(f); err == nil {
		return &ipfps, nil
	}
	// A few RAW formats (Fujifilm RAF, Sigma X3F) aren't EXIF/TIFF-based
	// containers at all, so neither path above applies. Fall back to
	// their own, format-specific container layout.
	if _, err := f.Seek(0, 0); err != nil {
		return nil, nil
	}
	if err := ipfps.decodeContainerPreview(f); err == nil {
		return &ipfps, nil
	}
	return nil, nil
}

// decodeImage decodes r directly as one of the supported image formats.
func (ipfps *imgPHashFingerprinterState) decodeImage(r io.ReadSeeker) error {
	cfg, format, err := image.DecodeConfig(r)
	if err != nil {
		return err
	}
	decodeFunc, ok := extensions[format]
	if !ok {
		return fmt.Errorf("unknown file format: %v", format)
	}
	if _, err := r.Seek(0, 0); err != nil {
		return err
	}
	img, err := decodeFunc(r)
	if err != nil {
		return err
	}
	ipfps.cfg = cfg
	ipfps.img = img
	return nil
}

// decodeEmbeddedPreview locates and decodes the preview/thumbnail image
// embedded in r's EXIF data, for files whose primary image data isn't
// directly decodable (e.g. RAW camera formats).
//
// A file can have several preview-image tag candidates (e.g. a small EXIF
// thumbnail in IFD1 and a much larger vendor-specific preview elsewhere),
// and the largest one by claimed length isn't always a real, decodable
// image: some RAW formats have a StripOffsets/StripByteCounts tag pair that
// looks exactly like a preview-image tag but actually points at raw sensor
// data. So candidates are tried largest-first, falling back to the next one
// on decode failure, instead of trusting only the single biggest candidate.
func (ipfps *imgPHashFingerprinterState) decodeEmbeddedPreview(r io.Reader) error {
	x, err := exif.Decode(r)
	if err != nil {
		return err
	}

	// Gather every candidate's bytes up front so start/length pairs and
	// self-contained "blob" tags (e.g. Panasonic RW2's JpgFromRaw, which
	// holds its preview directly as the tag's own value rather than as an
	// offset/length pair into the rest of the file) can be tried together
	// in a single largest-first order.
	var blobs [][]byte
	for _, c := range x.PreviewCandidates(vendorPreviewImageTags...) {
		start, length := int64(c.Start), int64(c.Length)
		if length <= 0 || start < 0 || int(start+length) > len(x.Raw) {
			continue
		}
		blobs = append(blobs, x.Raw[start:start+length])
	}
	blobs = append(blobs, x.PreviewBlobCandidates(vendorPreviewBlobTags...)...)
	sort.Slice(blobs, func(i, j int) bool { return len(blobs[i]) > len(blobs[j]) })

	if len(blobs) == 0 {
		return fmt.Errorf("no embedded preview image found")
	}
	var lastErr error
	for _, b := range blobs {
		if err := ipfps.decodeImage(bytes.NewReader(b)); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return lastErr
}

func (ipfps *imgPHashFingerprinterState) Get() ([]Fingerprint, error) {
	if ipfps.img == nil {
		return nil, nil
	}
	if ipfps.cfg.Height < 10 || ipfps.cfg.Width < 10 {
		return nil, nil
	}

	var res []Fingerprint
	for _, f := range []func(*imgPHashFingerprinterState) (Fingerprint, error){
		(*imgPHashFingerprinterState).getAzr,
		(*imgPHashFingerprinterState).getNr90,
		// TODO: unstable between platforms (arm/x86) (*imgPHashFingerprinterState).getAjdnikCM,
		(*imgPHashFingerprinterState).getAjdnikMH,
	} {
		if fp, err := f(ipfps); err == nil && fp.Hash != "" {
			res = append(res, fp)
		}
	}

	return res, nil
}

func (ipfps *imgPHashFingerprinterState) Cleanup() {}

func (ipfps *imgPHashFingerprinterState) getAzr() (Fingerprint, error) {
	h := azr.DTC(ipfps.img)
	return Fingerprint{
		Kind:    "ImgPHashAzr",
		Hash:    fmt.Sprintf("%08x", h),
		Quality: 20,
	}, nil
}

func (ipfps *imgPHashFingerprinterState) getNr90() (Fingerprint, error) {
	avg := nr90.AverageHash(ipfps.img)
	dif := nr90.DifferenceHash(ipfps.img)
	return Fingerprint{
		Kind:    "ImgPHashNr90",
		Hash:    fmt.Sprintf("%08x.%08x", uint64(avg), uint64(dif)),
		Quality: 20,
	}, nil
}

func (ipfps *imgPHashFingerprinterState) getAjdnikCM() (Fingerprint, error) {
	cmhash := ajdnik.NewColorMoment()
	h := cmhash.Calculate(ipfps.img)
	buf := make([]byte, 8*len(h))
	for i, f := range h {
		binary.LittleEndian.PutUint64(buf[8*i:], math.Float64bits(f))
	}
	res := base64.StdEncoding.EncodeToString(buf)
	return Fingerprint{
		Kind:    "ImgPHashAjdnikCM",
		Hash:    res,
		Quality: 20,
	}, nil
}

func (ipfps *imgPHashFingerprinterState) getAjdnikMH() (Fingerprint, error) {
	mhhash := ajdnik.NewMarrHildreth()
	h := mhhash.Calculate(ipfps.img)
	return Fingerprint{
		Kind:    "ImgPHashAjdnikMH",
		Hash:    base64.StdEncoding.EncodeToString(h),
		Quality: 20,
	}, nil
}
