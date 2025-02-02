package fingerprint

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/rpajarola/exiftools/exif"
	"github.com/rpajarola/exiftools/mknote"
)

type EXIFFingerprinter struct{}

type exifFingerprinterState struct {
	xf *exif.Exif
}

func init() {
	exif.RegisterParsers(mknote.All...)
	//tiff.TagLengthCutoff = 8 * 1024 * 1024
	fingerprinters = append(fingerprinters, &EXIFFingerprinter{})
}

func (xfp *EXIFFingerprinter) Init(filename string) (FingerprinterState, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	xfps := exifFingerprinterState{}
	xfps.xf, err = exif.Decode(f)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if xfps.xf == nil {
		return nil, nil
	}
	return &xfps, nil
}

func (xfps *exifFingerprinterState) Get() ([]Fingerprint, error) {
	var res []Fingerprint
	for _, f := range []func(*exifFingerprinterState) (Fingerprint, error){
		(*exifFingerprinterState).getModelSerialPhotoIDFP,
	} {
		if fp, err := f(xfps); err == nil && fp.Hash != "" {
			res = append(res, fp)
		}
	}

	return res, nil
}

func (xfps *exifFingerprinterState) getModelSerialPhotoIDFP() (Fingerprint, error) {
	cameraModel := xfps.getCameraModel()
	cameraSerial := xfps.getCameraSerial()
	photoID, photoIDIsUnique, photoIDQuality := xfps.getPhotoID()
	if cameraModel == "" {
		// There isn't even basic EXIF information
		return NoFingerprint, nil
	}
	if cameraSerial == "" && !photoIDIsUnique {
		// Need at least a unique camera ID or a unique photo ID
		return NoFingerprint, nil
	}
	return Fingerprint{
		Kind:    "EXIFModelSerialPhotoID",
		Hash:    cameraModel + " " + cameraSerial + " " + photoID,
		Quality: photoIDQuality,
	}, nil
}

func (xfps *exifFingerprinterState) Cleanup() {}

func trim(s string) string {
	return strings.Trim(s, "\" 	")
}

func (xfps *exifFingerprinterState) getCameraModel() string {
	make, err := xfps.xf.Get(exif.Make)
	if err != nil {
		return ""
	}
	model, err := xfps.xf.Get(exif.Model)
	if err != nil {
		return ""
	}
	makestr := trim(make.String())
	modelstr := trim(model.String())
	switch makestr {
	case "NIKON CORPORATION":
		makestr = "NIKON"
	case "OLYMPUS IMAGING CORP.":
		makestr = "OLYMPUS"
	}
	if strings.HasPrefix(modelstr, makestr) {
		return modelstr
	}
	return makestr + " " + modelstr
}

func (xfps *exifFingerprinterState) getCameraSerial() string {
	if v, err := xfps.xf.Get(mknote.SerialNumber); err == nil {
		return trim(v.String())
	}
	if v, err := xfps.xf.Get(mknote.InternalSerialNumber); err == nil {
		return trim(v.String())
	}
	if v, err := xfps.xf.Get(mknote.NikonSerialNO); err == nil {
		return trim(v.String())
	}
	if v, err := xfps.xf.Get(mknote.SonyInternalSerialNumber); err == nil {
		return trim(v.String())
	}
	if v, err := xfps.xf.Get(mknote.SonyInternalSerialNumber2); err == nil {
		return trim(v.String())
	}
	return ""
}

func (xfps *exifFingerprinterState) getPhotoID() (string, bool, int) {
	quality := ""
	if v, err := xfps.xf.Get(mknote.Quality); err == nil {
		quality = " " + trim(v.String())
	}
	for _, t := range []struct {
		field   exif.FieldName
		unique  bool
		quality int
		hexify  bool
	}{
		{exif.ImageUniqueID, true, 100, false},
		{mknote.CanonImageUniqueID, true, 100, true},
		{mknote.ApplePhotoIdentifier, true, 100, false},
		{mknote.ShutterCount, false, 90, false},
		{mknote.SonyShutterCount, false, 90, false},
		{mknote.SonyShutterCount2, false, 90, false},
		{mknote.SonyShutterCount3, false, 90, false},
		{mknote.FileNumber, false, 90, false},
	} {
		if v, err := xfps.xf.Get(t.field); err == nil {
			if t.hexify {
				return hex.EncodeToString(v.Val) + quality, t.unique, t.quality
			}
			return trim(v.String()) + quality, t.unique, t.quality
		}
	}

	if v, err := xfps.xf.DateTime(exif.DateTimeOriginal); err == nil {
		return fmt.Sprintf("%v", v) + quality, false, 80
	}

	return "", false, 0
}
