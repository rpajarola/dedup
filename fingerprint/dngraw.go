package fingerprint

import (
	"encoding/binary"
	"fmt"
	"image"
	"math"
)

// Rendering of DNG raw (CFA) sensor data, for DNGs that have no embedded
// preview at all (e.g. some iPhone DNGs). The result is only meant for
// perceptual hashing, not viewing: a half-size grayscale image where each
// 2x2 Bayer block becomes one pixel, black/white level and white balance
// corrected, with an sRGB-ish gamma so that its tonal distribution is
// close to that of a camera JPEG of the same shot.

// TIFF/DNG tag numbers used here.
const (
	tiffTagNewSubfileType      = 254
	tiffTagImageWidth          = 256
	tiffTagImageLength         = 257
	tiffTagBitsPerSample       = 258
	tiffTagCompression         = 259
	tiffTagPhotometric         = 262
	tiffTagStripOffsets        = 273
	tiffTagSamplesPerPixel     = 277
	tiffTagRowsPerStrip        = 278
	tiffTagStripByteCounts     = 279
	tiffTagTileWidth           = 322
	tiffTagTileLength          = 323
	tiffTagTileOffsets         = 324
	tiffTagTileByteCounts      = 325
	tiffTagSubIFDs             = 330
	tiffTagCFARepeatPatternDim = 33421
	tiffTagCFAPattern          = 33422
	dngTagLinearizationTable   = 50712
	dngTagBlackLevelRepeatDim  = 50713
	dngTagBlackLevel           = 50714
	dngTagWhiteLevel           = 50717
	dngTagDefaultCropOrigin    = 50719
	dngTagDefaultCropSize      = 50720
	dngTagAsShotNeutral        = 50728
	dngTagBaselineExposure     = 50730
	dngTagActiveArea           = 50829

	tiffPhotometricCFA         = 32803
	tiffCompressionNone        = 1
	tiffCompressionLosslessJPG = 7
)

// tiffIFD holds an IFD's numeric tag values.
type tiffIFD map[uint16][]float64

func (ifd tiffIFD) get(tag uint16, def float64) float64 {
	if v := ifd[tag]; len(v) > 0 {
		return v[0]
	}
	return def
}

// parseTIFFIFDs returns all IFDs in a TIFF file: the IFD0 chain and,
// recursively, their SubIFDs.
func parseTIFFIFDs(data []byte) (binary.ByteOrder, []tiffIFD, error) {
	if len(data) < 8 {
		return nil, nil, fmt.Errorf("tiff: file too short")
	}
	var bo binary.ByteOrder
	switch string(data[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return nil, nil, fmt.Errorf("tiff: bad byte order")
	}
	var ifds []tiffIFD
	seen := map[uint32]bool{}
	var walk func(off uint32, depth int)
	walk = func(off uint32, depth int) {
		for off != 0 && !seen[off] && depth < 4 && int64(off)+2 <= int64(len(data)) {
			seen[off] = true
			n := int(bo.Uint16(data[off:]))
			end := int64(off) + 2 + 12*int64(n)
			if end+4 > int64(len(data)) {
				return
			}
			ifd := tiffIFD{}
			for i := range n {
				e := data[int(off)+2+12*i:]
				tag, typ, count := bo.Uint16(e), bo.Uint16(e[2:]), bo.Uint32(e[4:])
				if v, ok := tiffValues(data, bo, typ, count, e[8:12]); ok {
					ifd[tag] = v
				}
			}
			ifds = append(ifds, ifd)
			for _, sub := range ifd[tiffTagSubIFDs] {
				walk(uint32(sub), depth+1)
			}
			off = bo.Uint32(data[end:])
		}
	}
	walk(bo.Uint32(data[4:]), 0)
	if len(ifds) == 0 {
		return nil, nil, fmt.Errorf("tiff: no IFDs")
	}
	return bo, ifds, nil
}

// tiffValues decodes a numeric tag value of the given type and count;
// valOrOff is the entry's 4-byte value/offset field.
func tiffValues(data []byte, bo binary.ByteOrder, typ uint16, count uint32, valOrOff []byte) ([]float64, bool) {
	sizes := map[uint16]int{1: 1, 3: 2, 4: 4, 5: 8, 6: 1, 7: 1, 8: 2, 9: 4, 10: 8, 11: 4, 12: 8, 13: 4}
	size, ok := sizes[typ]
	if !ok || count == 0 || count > 1<<20 {
		return nil, false
	}
	n := int64(size) * int64(count)
	raw := valOrOff
	if n > 4 {
		off := int64(bo.Uint32(valOrOff))
		if off+n > int64(len(data)) {
			return nil, false
		}
		raw = data[off : off+n]
	}
	res := make([]float64, count)
	for i := range res {
		b := raw[i*size:]
		switch typ {
		case 1, 7:
			res[i] = float64(b[0])
		case 6:
			res[i] = float64(int8(b[0]))
		case 3:
			res[i] = float64(bo.Uint16(b))
		case 8:
			res[i] = float64(int16(bo.Uint16(b)))
		case 4, 13:
			res[i] = float64(bo.Uint32(b))
		case 9:
			res[i] = float64(int32(bo.Uint32(b)))
		case 5:
			res[i] = float64(bo.Uint32(b)) / float64(max(1, bo.Uint32(b[4:])))
		case 10:
			d := int32(bo.Uint32(b[4:]))
			if d == 0 {
				d = 1
			}
			res[i] = float64(int32(bo.Uint32(b))) / float64(d)
		case 11:
			res[i] = float64(math.Float32frombits(bo.Uint32(b)))
		case 12:
			res[i] = math.Float64frombits(bo.Uint64(b))
		}
	}
	return res, true
}

// decodeDNGRaw renders the raw CFA image of a DNG file (see top of file).
func decodeDNGRaw(data []byte) (image.Image, error) {
	bo, ifds, err := parseTIFFIFDs(data)
	if err != nil {
		return nil, err
	}
	// Pick the largest 2x2 CFA image, preferring the main image.
	var raw tiffIFD
	for _, ifd := range ifds {
		if ifd.get(tiffTagPhotometric, 0) != tiffPhotometricCFA || ifd.get(tiffTagSamplesPerPixel, 1) != 1 {
			continue
		}
		if dim := ifd[tiffTagCFARepeatPatternDim]; len(dim) != 2 || dim[0] != 2 || dim[1] != 2 {
			continue
		}
		if raw == nil || ifd.get(tiffTagImageWidth, 0) > raw.get(tiffTagImageWidth, 0) {
			raw = ifd
		}
	}
	if raw == nil {
		return nil, fmt.Errorf("dng: no 2x2 CFA raw image found")
	}
	if !isBayerPattern(raw[tiffTagCFAPattern]) {
		return nil, fmt.Errorf("dng: unsupported CFA pattern %v", raw[tiffTagCFAPattern])
	}
	plane, w, h, err := dngRawPlane(data, bo, raw)
	if err != nil {
		return nil, err
	}
	return renderCFA(plane, w, h, raw)
}

// isBayerPattern reports whether a 2x2 CFAPattern has one red, two green
// and one blue position (0=red, 1=green, 2=blue).
func isBayerPattern(p []float64) bool {
	if len(p) != 4 {
		return false
	}
	var n [3]int
	for _, c := range p {
		if c < 0 || c > 2 {
			return false
		}
		n[int(c)]++
	}
	return n == [3]int{1, 2, 1}
}

// dngRawPlane assembles the raw sample plane from the IFD's tiles or
// strips.
func dngRawPlane(data []byte, bo binary.ByteOrder, ifd tiffIFD) ([]uint16, int, int, error) {
	w, h := int(ifd.get(tiffTagImageWidth, 0)), int(ifd.get(tiffTagImageLength, 0))
	if w <= 0 || h <= 0 || w > 1<<16 || h > 1<<16 {
		return nil, 0, 0, fmt.Errorf("dng: bad raw image size %dx%d", w, h)
	}
	compression := int(ifd.get(tiffTagCompression, 1))
	bps := int(ifd.get(tiffTagBitsPerSample, 16))
	if compression == tiffCompressionNone && bps != 8 && bps != 16 {
		return nil, 0, 0, fmt.Errorf("dng: unsupported uncompressed bit depth %d", bps)
	}
	if compression != tiffCompressionNone && compression != tiffCompressionLosslessJPG {
		return nil, 0, 0, fmt.Errorf("dng: unsupported raw compression %d", compression)
	}

	// Tiles, or strips as full-width tiles.
	tw, th := int(ifd.get(tiffTagTileWidth, 0)), int(ifd.get(tiffTagTileLength, 0))
	offsets, counts := ifd[tiffTagTileOffsets], ifd[tiffTagTileByteCounts]
	if tw == 0 || th == 0 || offsets == nil {
		tw, th = w, int(ifd.get(tiffTagRowsPerStrip, float64(h)))
		offsets, counts = ifd[tiffTagStripOffsets], ifd[tiffTagStripByteCounts]
	}
	if tw <= 0 || th <= 0 || len(offsets) == 0 || len(offsets) != len(counts) {
		return nil, 0, 0, fmt.Errorf("dng: bad tile/strip layout")
	}
	across := (w + tw - 1) / tw
	plane := make([]uint16, w*h)
	for i := range offsets {
		off, n := int64(offsets[i]), int64(counts[i])
		if off < 0 || n < 0 || off+n > int64(len(data)) {
			return nil, 0, 0, fmt.Errorf("dng: tile %d out of bounds", i)
		}
		chunk := data[off : off+n]
		var samples []uint16
		switch compression {
		case tiffCompressionLosslessJPG:
			img, err := decodeLjpeg(chunk)
			if err != nil {
				return nil, 0, 0, fmt.Errorf("dng: tile %d: %w", i, err)
			}
			samples = img.Samples
		case tiffCompressionNone:
			samples = make([]uint16, len(chunk)*8/bps)
			for j := range samples {
				if bps == 8 {
					samples[j] = uint16(chunk[j])
				} else {
					samples[j] = bo.Uint16(chunk[2*j:])
				}
			}
		}
		// Samples fill the tile row by row (a lossless JPEG's own
		// width*components may differ from the tile width, but its
		// samples are still in raster order).
		x0, y0 := (i%across)*tw, (i/across)*th
		for j, v := range samples {
			x, y := x0+j%tw, y0+j/tw
			if y >= y0+th {
				break
			}
			if x < w && y < h {
				plane[y*w+x] = v
			}
		}
	}
	return plane, w, h, nil
}

// renderCFA renders a Bayer CFA plane to a half-size grayscale image.
func renderCFA(plane []uint16, w, h int, ifd tiffIFD) (image.Image, error) {
	// ActiveArea (top, left, bottom, right) is where the CFA pattern
	// starts; DefaultCrop{Origin,Size} is relative to it.
	top, left, bottom, right := 0, 0, h, w
	if a := ifd[dngTagActiveArea]; len(a) == 4 {
		top, left, bottom, right = int(a[0]), int(a[1]), int(a[2]), int(a[3])
	}
	if top < 0 || left < 0 || bottom > h || right > w || top >= bottom || left >= right {
		return nil, fmt.Errorf("dng: bad ActiveArea %v", ifd[dngTagActiveArea])
	}
	cx, cy, cw, ch := 0, 0, right-left, bottom-top
	if o, s := ifd[dngTagDefaultCropOrigin], ifd[dngTagDefaultCropSize]; len(o) == 2 && len(s) == 2 {
		cx, cy, cw, ch = int(o[0]), int(o[1]), int(s[0]), int(s[1])
		if cx < 0 || cy < 0 || cw <= 0 || ch <= 0 || cx+cw > right-left || cy+ch > bottom-top {
			cx, cy, cw, ch = 0, 0, right-left, bottom-top
		}
	}

	lin := ifd[dngTagLinearizationTable]
	white := ifd.get(dngTagWhiteLevel, math.Exp2(ifd.get(tiffTagBitsPerSample, 16))-1)
	black := ifd[dngTagBlackLevel]
	brows, bcols := 1, 1
	if d := ifd[dngTagBlackLevelRepeatDim]; len(d) == 2 && d[0] >= 1 && d[1] >= 1 {
		brows, bcols = int(d[0]), int(d[1])
	}
	if len(black) < brows*bcols {
		brows, bcols = 1, 1
		if len(black) == 0 {
			black = []float64{0}
		}
	}
	// White balance: scale each channel by 1/AsShotNeutral.
	wb := [3]float64{1, 1, 1}
	if n := ifd[dngTagAsShotNeutral]; len(n) == 3 && n[0] > 0 && n[1] > 0 && n[2] > 0 {
		wb = [3]float64{n[1] / n[0], 1, n[1] / n[2]}
	}
	gain := math.Exp2(ifd.get(dngTagBaselineExposure, 0))
	pattern := ifd[tiffTagCFAPattern]

	value := func(x, y int) (float64, int) {
		ax, ay := x-left, y-top // position relative to the CFA origin
		v := float64(plane[y*w+x])
		if len(lin) > 0 {
			v = lin[min(int(v), len(lin)-1)]
		}
		b := black[(ay%brows)*bcols+ax%bcols]
		v = (v - b) / (white - b)
		c := int(pattern[(ay%2)*2+ax%2])
		return v * wb[c], c
	}

	ow, oh := cw/2, ch/2
	if ow == 0 || oh == 0 {
		return nil, fmt.Errorf("dng: crop area too small")
	}
	out := image.NewGray(image.Rect(0, 0, ow, oh))
	for oy := range oh {
		for ox := range ow {
			var rgb [3]float64
			for dy := range 2 {
				for dx := range 2 {
					v, c := value(left+cx+2*ox+dx, top+cy+2*oy+dy)
					rgb[c] += v
				}
			}
			rgb[1] /= 2
			lum := gain * (0.2126*rgb[0] + 0.7152*rgb[1] + 0.0722*rgb[2])
			out.Pix[oy*out.Stride+ox] = uint8(math.Round(255 * srgbGamma(lum)))
		}
	}
	return out, nil
}

// srgbGamma applies the sRGB transfer function to a linear value, clipped
// to [0, 1].
func srgbGamma(v float64) float64 {
	switch {
	case v <= 0:
		return 0
	case v >= 1:
		return 1
	case v <= 0.0031308:
		return 12.92 * v
	}
	return 1.055*math.Pow(v, 1/2.4) - 0.055
}
