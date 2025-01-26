package fingerprint

import (
	"encoding/binary"
	"encoding/base64"
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
)

var (
	extensions = map[string]func(io.Reader) (image.Image, error){
		"gif":  gif.Decode,
		"heic": heif.Decode,
		"heif": heif.Decode,
		"jpeg": jpeg.Decode,
		"jpg":  jpeg.Decode,
		"png":  png.Decode,
	}
)

type ImgPHashFingerprinter struct {
	cfg image.Config
	img image.Image
}

func init() {
	fingerprinters = append(fingerprinters, &ImgPHashFingerprinter{})
}

func (d *ImgPHashFingerprinter) Init(filename string) error {
	d.img = nil
	f, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("Open(%v): %w", filename, err)
	}
	defer f.Close()
	cfg, format, err := image.DecodeConfig(f)
	if err != nil {
		return fmt.Errorf("image.DecodeConfig(%v): %w", filename, err)
	}
	d.cfg = cfg
	decodeFunc, ok := extensions[format]
	if !ok {
		return fmt.Errorf("%v: unknown file format: %v", filename, format)
	}
	f.Seek(0, 0)
	if d.img, err = decodeFunc(f); err != nil {
		return fmt.Errorf("decode(%v): %w", filename, err)
	}

	return nil
}

func (d *ImgPHashFingerprinter) Get() ([]Fingerprint, error) {
	if d.img == nil {
		return nil, nil
	}
	if d.cfg.Height < 10 || d.cfg.Width < 10 {
		return nil, nil
	}

	var res []Fingerprint
	for _, f := range []func(*ImgPHashFingerprinter) (Fingerprint, error){
		(*ImgPHashFingerprinter).getAzr,
		(*ImgPHashFingerprinter).getNr90,
		// TODO: unstable between platforms (arm/x86) (*ImgPHashFingerprinter).getAjdnikCM,
		(*ImgPHashFingerprinter).getAjdnikMH,
	} {
		if fp, err := f(d); err == nil && fp.Hash != "" {
			res = append(res, fp)
		}
	}

	return res, nil
}

func (d *ImgPHashFingerprinter) getAzr() (Fingerprint, error) {
	h := azr.DTC(d.img)
	return Fingerprint{
		Kind:    "ImgPHashAzr",
		Hash:    fmt.Sprintf("%08x", h),
		Quality: 20,
	}, nil
}

func (d *ImgPHashFingerprinter) getNr90() (Fingerprint, error) {
	avg := nr90.AverageHash(d.img)
	dif := nr90.DifferenceHash(d.img)
	return Fingerprint{
		Kind:    "ImgPHashNr90",
		Hash:    fmt.Sprintf("%08x.%08x", uint64(avg), uint64(dif)),
		Quality: 20,
	}, nil
}

func (d *ImgPHashFingerprinter) getAjdnikCM() (Fingerprint, error) {
	cmhash := ajdnik.NewColorMoment()
	h := cmhash.Calculate(d.img)
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

func (d *ImgPHashFingerprinter) getAjdnikMH() (Fingerprint, error) {
	mhhash := ajdnik.NewMarrHildreth()
	h := mhhash.Calculate(d.img)
	return Fingerprint{
		Kind:    "ImgPHashAjdnikMH",
		Hash:    base64.StdEncoding.EncodeToString(h),
		Quality: 20,
	}, nil
}
