package fingerprint

import (
	"encoding/binary"
	"fmt"
)

// Decoder for lossless JPEG (ITU T.81 process 14, SOF3, Huffman-coded), as
// used for raw sensor data in DNG files. Only what DNG uses is supported:
// all components sampled 1x1, no restart intervals.

// ljpegImage is a decoded lossless JPEG: Height rows of Width*Components
// samples each, components interleaved per pixel.
type ljpegImage struct {
	Width, Height, Components int
	Precision                 int
	Samples                   []uint16
}

type ljpegHuffman struct {
	// lookup maps the next 16 bits (MSB first) to the symbol and code
	// length of the code they start with; length 0 means invalid.
	lookup []ljpegHuffEntry
}

type ljpegHuffEntry struct {
	symbol, length uint8
}

func newLjpegHuffman(counts []byte, symbols []byte) (*ljpegHuffman, error) {
	h := &ljpegHuffman{lookup: make([]ljpegHuffEntry, 1<<16)}
	code, k := 0, 0
	for l := 1; l <= 16; l++ {
		for range int(counts[l-1]) {
			if k >= len(symbols) {
				return nil, fmt.Errorf("ljpeg: DHT symbol table too short")
			}
			if code >= 1<<l {
				return nil, fmt.Errorf("ljpeg: invalid Huffman table")
			}
			// Every 16-bit value starting with this code maps to it.
			lo := code << (16 - l)
			hi := (code + 1) << (16 - l)
			for i := lo; i < hi; i++ {
				h.lookup[i] = ljpegHuffEntry{symbol: symbols[k], length: uint8(l)}
			}
			code++
			k++
		}
		code <<= 1
	}
	return h, nil
}

// ljpegBitReader reads MSB-first bits from JPEG entropy-coded data,
// undoing 0xFF00 byte stuffing. Past the end of the data (or at a
// marker), it returns 0 bits.
type ljpegBitReader struct {
	data  []byte
	pos   int
	acc   uint64
	nbits uint
}

func (br *ljpegBitReader) fill() {
	for br.nbits <= 56 {
		var b byte
		if br.pos < len(br.data) {
			b = br.data[br.pos]
			if b == 0xff {
				if br.pos+1 < len(br.data) && br.data[br.pos+1] == 0x00 {
					br.pos += 2
				} else {
					// A marker: stop consuming, pad with zeros.
					b = 0
				}
			} else {
				br.pos++
			}
		}
		br.acc |= uint64(b) << (56 - br.nbits)
		br.nbits += 8
	}
}

func (br *ljpegBitReader) peek16() uint32 {
	if br.nbits < 16 {
		br.fill()
	}
	return uint32(br.acc >> 48)
}

func (br *ljpegBitReader) skip(n uint) {
	br.acc <<= n
	br.nbits -= n
}

func (br *ljpegBitReader) bits(n uint) uint32 {
	if n == 0 {
		return 0
	}
	if br.nbits < n {
		br.fill()
	}
	v := uint32(br.acc >> (64 - n))
	br.skip(n)
	return v
}

// decodeDiff decodes one Huffman-coded difference value.
func (br *ljpegBitReader) decodeDiff(h *ljpegHuffman) (int32, error) {
	e := h.lookup[br.peek16()]
	if e.length == 0 {
		return 0, fmt.Errorf("ljpeg: invalid Huffman code")
	}
	br.skip(uint(e.length))
	s := uint(e.symbol)
	switch {
	case s == 0:
		return 0, nil
	case s == 16:
		return 32768, nil
	case s > 16:
		return 0, fmt.Errorf("ljpeg: invalid difference category %d", s)
	}
	v := int32(br.bits(s))
	if v < 1<<(s-1) {
		v -= 1<<s - 1
	}
	return v, nil
}

// decodeLjpeg decodes a lossless JPEG image.
func decodeLjpeg(data []byte) (*ljpegImage, error) {
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 {
		return nil, fmt.Errorf("ljpeg: missing SOI marker")
	}
	var (
		img       *ljpegImage
		huff      [4]*ljpegHuffman
		compIDs   []byte
		pos       = 2
		restartIv int
	)
	for {
		if pos+4 > len(data) || data[pos] != 0xff {
			return nil, fmt.Errorf("ljpeg: expected marker at %d", pos)
		}
		marker := data[pos+1]
		if marker == 0xff {
			pos++ // fill byte
			continue
		}
		length := int(binary.BigEndian.Uint16(data[pos+2:]))
		if length < 2 || pos+2+length > len(data) {
			return nil, fmt.Errorf("ljpeg: segment %#x out of bounds", marker)
		}
		seg := data[pos+4 : pos+2+length]
		pos += 2 + length
		switch marker {
		case 0xc4: // DHT
			for len(seg) > 0 {
				if len(seg) < 17 {
					return nil, fmt.Errorf("ljpeg: short DHT")
				}
				class, id := seg[0]>>4, seg[0]&0x0f
				counts := seg[1:17]
				n := 0
				for _, c := range counts {
					n += int(c)
				}
				if class != 0 || id > 3 || len(seg) < 17+n {
					return nil, fmt.Errorf("ljpeg: bad DHT")
				}
				h, err := newLjpegHuffman(counts, seg[17:17+n])
				if err != nil {
					return nil, err
				}
				huff[id] = h
				seg = seg[17+n:]
			}
		case 0xc3: // SOF3: lossless, Huffman
			if len(seg) < 6 {
				return nil, fmt.Errorf("ljpeg: short SOF3")
			}
			img = &ljpegImage{
				Precision:  int(seg[0]),
				Height:     int(binary.BigEndian.Uint16(seg[1:])),
				Width:      int(binary.BigEndian.Uint16(seg[3:])),
				Components: int(seg[5]),
			}
			if len(seg) < 6+3*img.Components || img.Components < 1 || img.Components > 4 {
				return nil, fmt.Errorf("ljpeg: bad SOF3")
			}
			for i := range img.Components {
				c := seg[6+3*i:]
				if c[1] != 0x11 {
					return nil, fmt.Errorf("ljpeg: unsupported sampling factors %#x", c[1])
				}
				compIDs = append(compIDs, c[0])
			}
		case 0xc0, 0xc1, 0xc2, 0xc5, 0xc6, 0xc7, 0xc9, 0xca, 0xcb, 0xcd, 0xce, 0xcf:
			return nil, fmt.Errorf("ljpeg: unsupported JPEG process (SOF%d)", marker-0xc0)
		case 0xdd: // DRI
			if len(seg) >= 2 {
				restartIv = int(binary.BigEndian.Uint16(seg))
			}
		case 0xda: // SOS
			if img == nil {
				return nil, fmt.Errorf("ljpeg: SOS before SOF3")
			}
			if restartIv != 0 {
				return nil, fmt.Errorf("ljpeg: restart intervals are not supported")
			}
			if len(seg) < 1 || int(seg[0]) != img.Components || len(seg) < 1+2*img.Components+3 {
				return nil, fmt.Errorf("ljpeg: unsupported SOS")
			}
			tables := make([]*ljpegHuffman, img.Components)
			for i := range img.Components {
				if seg[1+2*i] != compIDs[i] {
					return nil, fmt.Errorf("ljpeg: SOS component order differs from SOF3")
				}
				td := seg[2+2*i] >> 4
				if td > 3 || huff[td] == nil {
					return nil, fmt.Errorf("ljpeg: missing Huffman table %d", td)
				}
				tables[i] = huff[td]
			}
			p := seg[1+2*img.Components:]
			predictor, pointTransform := int(p[0]), int(p[2]&0x0f)
			if err := img.decodeScan(data[pos:], tables, predictor, pointTransform); err != nil {
				return nil, err
			}
			return img, nil
		}
	}
}

func (img *ljpegImage) decodeScan(data []byte, tables []*ljpegHuffman, predictor, pt int) error {
	if predictor < 1 || predictor > 7 {
		return fmt.Errorf("ljpeg: unsupported predictor %d", predictor)
	}
	nc := img.Components
	stride := img.Width * nc
	img.Samples = make([]uint16, stride*img.Height)
	br := &ljpegBitReader{data: data}
	initial := int32(1) << (img.Precision - pt - 1)
	s := img.Samples
	for y := range img.Height {
		row := y * stride
		for x := range img.Width {
			for c := range nc {
				i := row + x*nc + c
				var pred int32
				switch {
				case x == 0 && y == 0:
					pred = initial
				case y == 0:
					pred = int32(s[i-nc])
				case x == 0:
					pred = int32(s[i-stride])
				default:
					ra, rb, rc := int32(s[i-nc]), int32(s[i-stride]), int32(s[i-stride-nc])
					switch predictor {
					case 1:
						pred = ra
					case 2:
						pred = rb
					case 3:
						pred = rc
					case 4:
						pred = ra + rb - rc
					case 5:
						pred = ra + (rb-rc)>>1
					case 6:
						pred = rb + (ra-rc)>>1
					case 7:
						pred = (ra + rb) >> 1
					}
				}
				diff, err := br.decodeDiff(tables[c])
				if err != nil {
					return fmt.Errorf("%w at row %d, column %d", err, y, x)
				}
				s[i] = uint16(pred + diff)
			}
		}
	}
	if pt != 0 {
		for i := range s {
			s[i] <<= pt
		}
	}
	return nil
}
