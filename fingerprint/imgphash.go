package fingerprint

import (
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"os"

	"github.com/azr/phash"
)

var (
	extensions = map[string]func(io.Reader) (image.Image, error){
		"jpg":  jpeg.Decode,
		"jpeg": jpeg.Decode,
		"png":  png.Decode,
		"gif":  gif.Decode,
	}
)

type ImgPHashFingerprinter struct {
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
	_, format, err := image.DecodeConfig(f)
	if err != nil {
		return fmt.Errorf("image.DecodeConfig(%v): %w", filename, err)
	}

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

	var res []Fingerprint
	for _, f := range []func(*ImgPHashFingerprinter) (Fingerprint, error){
		(*ImgPHashFingerprinter).getAzr,
	} {
		if fp, err := f(d); err == nil && fp.Hash != "" {
			res = append(res, fp)
		}
	}

	return res, nil
}

func (d *ImgPHashFingerprinter) getAzr() (Fingerprint, error) {
	h := phash.DTC(d.img)
	return Fingerprint{
		Kind:    "ImgPHashAzr",
		Hash:    fmt.Sprintf("%08x", h),
		Quality: 20,
	}, nil
}
