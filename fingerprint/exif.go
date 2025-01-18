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

type EXIFFingerprinter struct {
	xf *exif.Exif
}

func init() {
	exif.RegisterParsers(mknote.All...)
	//tiff.TagLengthCutoff = 8 * 1024 * 1024
	fingerprinters = append(fingerprinters, &EXIFFingerprinter{})
}

func (xfp *EXIFFingerprinter) Init(filename string) error {
	xfp.xf = nil
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	xfp.xf, err = exif.Decode(f)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}

	return nil
}

func (xfp *EXIFFingerprinter) Get() ([]Fingerprint, error) {
	if xfp.xf == nil {
		return nil, nil
	}

	var res []Fingerprint
	for _, f := range []func(*EXIFFingerprinter) (Fingerprint, error){
		(*EXIFFingerprinter).getModelSerialPhotoIDFP,
	} {
		if fp, err := f(xfp); err == nil && fp.Hash != "" {
			res = append(res, fp)
		}
	}

	return res, nil
}

func (xfp *EXIFFingerprinter) getModelSerialPhotoIDFP() (Fingerprint, error) {
	cameraModel := xfp.getCameraModel()
	cameraSerial := xfp.getCameraSerial()
	photoID, photoIDIsUnique, photoIDQuality := xfp.getPhotoID()
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

func trim(s string) string {
	return strings.Trim(s, "\" 	")
}

func (xfp *EXIFFingerprinter) getCameraModel() string {
	make, err := xfp.xf.Get(exif.Make)
	if err != nil {
		return ""
	}
	model, err := xfp.xf.Get(exif.Model)
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

func (xfp *EXIFFingerprinter) getCameraSerial() string {
	if v, err := xfp.xf.Get(mknote.SerialNumber); err == nil {
		return trim(v.String())
	}
	if v, err := xfp.xf.Get(mknote.InternalSerialNumber); err == nil {
		return trim(v.String())
	}
	if v, err := xfp.xf.Get(mknote.NikonSerialNO); err == nil {
		return trim(v.String())
	}
	if v, err := xfp.xf.Get(mknote.SonyInternalSerialNumber); err == nil {
		return trim(v.String())
	}
	if v, err := xfp.xf.Get(mknote.SonyInternalSerialNumber2); err == nil {
		return trim(v.String())
	}
	return ""
}

func (xfp *EXIFFingerprinter) getPhotoID() (string, bool, int) {
	quality := ""
	if v, err := xfp.xf.Get(mknote.Quality); err == nil {
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
		if v, err := xfp.xf.Get(t.field); err == nil {
			if t.hexify {
				return hex.EncodeToString(v.Val) + quality, t.unique, t.quality
			}
			return trim(v.String()) + quality, t.unique, t.quality
		}
	}

	if v, err := xfp.xf.DateTime(exif.DateTimeOriginal); err == nil {
		return fmt.Sprintf("%v", v) + quality, false, 80
	}

	return "", false, 0
}
