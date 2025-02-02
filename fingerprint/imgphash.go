package fingerprint

import (
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

	nr90 "github.com/Nr90/imgsim"
	ajdnik "github.com/ajdnik/imghash"
	azr "github.com/azr/phash"
	heif "github.com/jdeng/goheif"
	tiff "golang.org/x/image/tiff"
)

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
	cfg, format, err := image.DecodeConfig(f)
	if err != nil {
		return nil, fmt.Errorf("image.DecodeConfig(%v): %w", filename, err)
	}
	ipfps.cfg = cfg
	decodeFunc, ok := extensions[format]
	if !ok {
		return nil, fmt.Errorf("%v: unknown file format: %v", filename, format)
	}
	f.Seek(0, 0)
	if ipfps.img, err = decodeFunc(f); err != nil {
		return nil, fmt.Errorf("decode(%v): %w", filename, err)
	}

	return &ipfps, nil
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
