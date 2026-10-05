package fingerprint

// Pure Go port of Chromaprint's default fingerprinting algorithm
// (CHROMAPRINT_ALGORITHM_TEST2), as used by fpcalc and AcoustID. See
// https://github.com/acoustid/chromaprint (MIT license, (C) Lukas Lalinsky).
//
// Input is mono signed 16-bit PCM at chromaprintSampleRate. Decoding and
// resampling are done by the caller (see audiofp.go), mirroring fpcalc,
// which does the same with libswresample before handing samples to
// libchromaprint.
//
// Output is bit-compatible with libchromaprint up to floating point
// differences in the FFT (libchromaprint itself produces slightly different
// fingerprints depending on which FFT library it was built with), which
// can flip a handful of bits in a fingerprint.

import (
	"encoding/base64"
	"math"
	"math/bits"
)

const (
	chromaprintSampleRate   = 11025
	chromaprintFrameSize    = 4096
	chromaprintFrameOverlap = chromaprintFrameSize - chromaprintFrameSize/3
	chromaprintFrameStep    = chromaprintFrameSize - chromaprintFrameOverlap
	chromaprintMinFreq      = 28
	chromaprintMaxFreq      = 3520
	chromaprintNumBands     = 12
	// chromaprintAlgorithm is the algorithm ID embedded in compressed
	// fingerprints (CHROMAPRINT_ALGORITHM_TEST2 == CHROMAPRINT_ALGORITHM_DEFAULT).
	chromaprintAlgorithm = 1
	// chromaprintMaxDuration mirrors fpcalc's default -length: only the
	// first 120s of audio are fingerprinted.
	chromaprintMaxDuration = 120
)

var chromaFilterCoefficients = []float64{0.25, 0.75, 1.0, 0.75, 0.25}

type chromaprintClassifier struct {
	filterType, y, height, width int
	t0, t1, t2                   float64
}

// kClassifiersTest2 from fingerprinter_configuration.cpp.
var chromaprintClassifiers = []chromaprintClassifier{
	{0, 4, 3, 15, 1.98215, 2.35817, 2.63523},
	{4, 4, 6, 15, -1.03809, -0.651211, -0.282167},
	{1, 0, 4, 16, -0.298702, 0.119262, 0.558497},
	{3, 8, 2, 12, -0.105439, 0.0153946, 0.135898},
	{3, 4, 4, 8, -0.142891, 0.0258736, 0.200632},
	{4, 0, 3, 5, -0.826319, -0.590612, -0.368214},
	{1, 2, 2, 9, -0.557409, -0.233035, 0.0534525},
	{2, 7, 3, 4, -0.0646826, 0.00620476, 0.0784847},
	{2, 6, 2, 16, -0.192387, -0.029699, 0.215855},
	{2, 1, 3, 2, -0.0397818, -0.00568076, 0.0292026},
	{5, 10, 1, 15, -0.53823, -0.369934, -0.190235},
	{3, 6, 2, 10, -0.124877, 0.0296483, 0.139239},
	{2, 1, 1, 14, -0.101475, 0.0225617, 0.231971},
	{3, 5, 6, 4, -0.0799915, -0.00729616, 0.063262},
	{1, 9, 2, 12, -0.272556, 0.019424, 0.302559},
	{3, 4, 2, 14, -0.164292, -0.0321188, 0.0846339},
}

// chromaprintMaxFilterWidth is the widest classifier filter (in frames).
const chromaprintMaxFilterWidth = 16

// chromaprint computes the raw Chromaprint fingerprint (one 32-bit
// sub-fingerprint per ~0.124s of audio) of mono 16-bit PCM samples at
// chromaprintSampleRate.
func chromaprint(samples []int16) []uint32 {
	features := chromaFeatures(samples)
	if len(features) < chromaprintMaxFilterWidth {
		return nil
	}
	img := newIntegralImage(features)
	fp := make([]uint32, 0, len(features)-chromaprintMaxFilterWidth+1)
	for x := 0; x+chromaprintMaxFilterWidth <= len(features); x++ {
		var bits uint32
		for _, c := range chromaprintClassifiers {
			bits = bits<<2 | grayCode(c.quantize(c.apply(img, x)))
		}
		fp = append(fp, bits)
	}
	return fp
}

// chromaFeatures runs the FFT -> chroma -> chroma filter -> normalizer
// stages and returns one 12-band feature vector per output frame.
func chromaFeatures(samples []int16) [][chromaprintNumBands]float64 {
	window := hammingWindow(chromaprintFrameSize)
	fft := newRealFFT(chromaprintFrameSize)
	notes, minIdx, maxIdx := chromaNotes()

	in := make([]float64, chromaprintFrameSize)
	power := make([]float64, chromaprintFrameSize/2+1)
	var chroma [][chromaprintNumBands]float64
	for off := 0; off+chromaprintFrameSize <= len(samples); off += chromaprintFrameStep {
		for i, s := range samples[off : off+chromaprintFrameSize] {
			// libchromaprint's avtx/vdsp FFT backends work in float32.
			in[i] = float64(float32(s) * window[i])
		}
		fft.power(in, power)
		var f [chromaprintNumBands]float64
		for i := minIdx; i < maxIdx; i++ {
			f[notes[i]] += power[i]
		}
		chroma = append(chroma, f)
	}

	nc := len(chromaFilterCoefficients)
	if len(chroma) < nc {
		return nil
	}
	res := make([][chromaprintNumBands]float64, 0, len(chroma)-nc+1)
	for off := 0; off+nc <= len(chroma); off++ {
		var f [chromaprintNumBands]float64
		for b := range f {
			for j, c := range chromaFilterCoefficients {
				f[b] += chroma[off+j][b] * c
			}
		}
		normalizeChroma(&f)
		res = append(res, f)
	}
	return res
}

// hammingWindow returns a Hamming window pre-scaled to map int16 samples
// into [-1, 1], as float32 to match libchromaprint.
func hammingWindow(size int) []float32 {
	w := make([]float32, size)
	for i := range w {
		w[i] = float32(1.0 / math.MaxInt16 * (0.54 - 0.46*math.Cos(float64(i)*2.0*math.Pi/float64(size-1))))
	}
	return w
}

// chromaNotes maps each FFT bin in [minIdx, maxIdx) to one of the 12
// chroma bands (pitch classes).
func chromaNotes() (notes []int, minIdx, maxIdx int) {
	freqToIndex := func(freq float64) int {
		return int(math.Round(chromaprintFrameSize * freq / chromaprintSampleRate))
	}
	minIdx = max(1, freqToIndex(chromaprintMinFreq))
	maxIdx = min(chromaprintFrameSize/2, freqToIndex(chromaprintMaxFreq))
	notes = make([]int, chromaprintFrameSize)
	for i := minIdx; i < maxIdx; i++ {
		freq := float64(i) * chromaprintSampleRate / chromaprintFrameSize
		octave := math.Log(freq/(440.0/16.0)) / math.Log(2.0)
		notes[i] = int(chromaprintNumBands * (octave - math.Floor(octave)))
	}
	return notes, minIdx, maxIdx
}

func normalizeChroma(f *[chromaprintNumBands]float64) {
	var squares float64
	for _, v := range f {
		squares += v * v
	}
	norm := math.Sqrt(squares)
	if norm < 0.01 {
		*f = [chromaprintNumBands]float64{}
		return
	}
	for i := range f {
		f[i] /= norm
	}
}

// integralImage is a summed-area table over the chroma feature rows:
// data[r][c] is the sum of features[0..r][0..c] (inclusive).
type integralImage struct {
	data [][chromaprintNumBands]float64
}

func newIntegralImage(features [][chromaprintNumBands]float64) *integralImage {
	img := &integralImage{data: make([][chromaprintNumBands]float64, len(features))}
	for r, row := range features {
		var sum float64
		for c, v := range row {
			sum += v
			img.data[r][c] = sum
		}
		if r > 0 {
			for c := range img.data[r] {
				img.data[r][c] = img.data[r-1][c] + img.data[r][c]
			}
		}
	}
	return img
}

// area returns the sum over rows [r1, r2) and columns [c1, c2), using the
// same operation order as libchromaprint's RollingIntegralImage::Area.
func (img *integralImage) area(r1, c1, r2, c2 int) float64 {
	if r1 == r2 || c1 == c2 {
		return 0
	}
	row2 := &img.data[r2-1]
	if r1 == 0 {
		if c1 == 0 {
			return row2[c2-1]
		}
		return row2[c2-1] - row2[c1-1]
	}
	row1 := &img.data[r1-1]
	if c1 == 0 {
		return row2[c2-1] - row1[c2-1]
	}
	return row2[c2-1] - row1[c2-1] - row2[c1-1] + row1[c1-1]
}

func subtractLog(a, b float64) float64 {
	return math.Log((1.0 + a) / (1.0 + b))
}

// apply evaluates the classifier's Haar-like filter with its top-left
// corner at frame x (see filter_utils.h for the filter shapes).
func (c *chromaprintClassifier) apply(img *integralImage, x int) float64 {
	y, w, h := c.y, c.width, c.height
	switch c.filterType {
	case 0:
		return subtractLog(img.area(x, y, x+w, y+h), 0)
	case 1:
		h2 := h / 2
		return subtractLog(img.area(x, y+h2, x+w, y+h), img.area(x, y, x+w, y+h2))
	case 2:
		w2 := w / 2
		return subtractLog(img.area(x+w2, y, x+w, y+h), img.area(x, y, x+w2, y+h))
	case 3:
		w2, h2 := w/2, h/2
		a := img.area(x, y+h2, x+w2, y+h) + img.area(x+w2, y, x+w, y+h2)
		b := img.area(x, y, x+w2, y+h2) + img.area(x+w2, y+h2, x+w, y+h)
		return subtractLog(a, b)
	case 4:
		h3 := h / 3
		a := img.area(x, y+h3, x+w, y+2*h3)
		b := img.area(x, y, x+w, y+h3) + img.area(x, y+2*h3, x+w, y+h)
		return subtractLog(a, b)
	case 5:
		w3 := w / 3
		a := img.area(x+w3, y, x+2*w3, y+h)
		b := img.area(x, y, x+w3, y+h) + img.area(x+2*w3, y, x+w, y+h)
		return subtractLog(a, b)
	}
	return 0
}

func (c *chromaprintClassifier) quantize(v float64) int {
	switch {
	case v < c.t0:
		return 0
	case v < c.t1:
		return 1
	case v < c.t2:
		return 2
	}
	return 3
}

func grayCode(i int) uint32 {
	return [4]uint32{0, 1, 3, 2}[i]
}

// chromaprintSimHash is libchromaprint's chromaprint_hash_fingerprint: bit
// i is set iff it's set in more than half of the sub-fingerprints. Similar
// audio yields hashes within a small Hamming distance of each other.
func chromaprintSimHash(fp []uint32) uint32 {
	var v [32]int
	for _, x := range fp {
		for j := range v {
			v[j] += int(x>>j) & 1
		}
	}
	threshold := len(fp) / 2
	var hash uint32
	for i, n := range v {
		if n > threshold {
			hash |= 1 << i
		}
	}
	return hash
}

// chromaprintEncode compresses and base64-encodes a raw fingerprint into
// the same string format fpcalc prints and the AcoustID API accepts.
func chromaprintEncode(fp []uint32) string {
	var normal, exceptional []byte
	prev := uint32(0)
	for _, x := range fp {
		// Each sub-fingerprint is XORed with the previous one, and the
		// positions of its set bits are stored as deltas: deltas < 7 as
		// 3-bit values, larger ones as 7 + a 5-bit "exceptional" value.
		d := x ^ prev
		prev = x
		last := 0
		for d != 0 {
			bit := bits.TrailingZeros32(d) + 1
			d &= d - 1
			v := bit - last
			if v >= 7 {
				normal = append(normal, 7)
				exceptional = append(exceptional, byte(v-7))
			} else {
				normal = append(normal, byte(v))
			}
			last = bit
		}
		normal = append(normal, 0)
	}
	n := len(fp)
	out := []byte{chromaprintAlgorithm, byte(n >> 16), byte(n >> 8), byte(n)}
	out = packBits(out, normal, 3)
	out = packBits(out, exceptional, 5)
	return base64.RawURLEncoding.EncodeToString(out)
}

// packBits appends vals, each truncated to width bits, as a little-endian
// bitstream (equivalent to libchromaprint's PackInt3Array/PackInt5Array).
func packBits(out []byte, vals []byte, width uint) []byte {
	var acc uint32
	var n uint
	for _, v := range vals {
		acc |= uint32(v&(1<<width-1)) << n
		n += width
		for n >= 8 {
			out = append(out, byte(acc))
			acc >>= 8
			n -= 8
		}
	}
	if n > 0 {
		out = append(out, byte(acc))
	}
	return out
}

// realFFT is a radix-2 FFT of real input, returning the power spectrum.
type realFFT struct {
	n       int
	twiddle []complex128
	rev     []int
	buf     []complex128
}

func newRealFFT(n int) *realFFT {
	f := &realFFT{n: n, twiddle: make([]complex128, n/2), rev: make([]int, n), buf: make([]complex128, n)}
	for i := range f.twiddle {
		s, c := math.Sincos(-2 * math.Pi * float64(i) / float64(n))
		f.twiddle[i] = complex(c, s)
	}
	logN := bits.TrailingZeros(uint(n))
	for i := range f.rev {
		f.rev[i] = int(bits.Reverse(uint(i)) >> (bits.UintSize - logN))
	}
	return f
}

// power writes |X[k]|^2 for k in [0, n/2] into out.
func (f *realFFT) power(in []float64, out []float64) {
	for i, v := range in {
		f.buf[f.rev[i]] = complex(v, 0)
	}
	for size := 2; size <= f.n; size <<= 1 {
		half, step := size/2, f.n/size
		for start := 0; start < f.n; start += size {
			for k := 0; k < half; k++ {
				t := f.twiddle[k*step] * f.buf[start+k+half]
				u := f.buf[start+k]
				f.buf[start+k] = u + t
				f.buf[start+k+half] = u - t
			}
		}
	}
	for k := range out {
		re, im := real(f.buf[k]), imag(f.buf[k])
		out[k] = re*re + im*im
	}
}
