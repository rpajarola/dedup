package fingerprint

import (
	"errors"
	"fmt"
	"image"
	"math"
	"os"
	"strings"

	"github.com/asticode/go-astiav"
	azr "github.com/azr/phash"
	"github.com/h2non/filetype"
)

type VideoPHashFingerprinter struct{}

type videoPHashFingerprinterState struct {
	formatContext *astiav.FormatContext
	codec         *astiav.Codec
	codecContext  *astiav.CodecContext
	stream        *astiav.Stream
	width         int
	height        int
	pixelFormat   astiav.PixelFormat
}

func init() {
	fingerprinters = append(fingerprinters, &VideoPHashFingerprinter{})

	// Handle ffmpeg logs
	astiav.SetLogLevel(astiav.LogLevelWarning)
	astiav.SetLogCallback(func(c astiav.Classer, l astiav.LogLevel, f, msg string) {
		var cs string
		if c != nil {
			if cl := c.Class(); cl != nil {
				cs = cl.String()
			}
		}
		fmt.Printf("ffmpeg %v %v: %v\n", l, cs, strings.TrimSpace(msg))
	})
}

func (vpfp *VideoPHashFingerprinter) Init(filename string) (FingerprinterState, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("open %v: %v", filename, err)
	}
	// We only have to pass the file header = first 261 bytes
	head := make([]byte, 261)
	f.Read(head)
	kind, err := filetype.Match(head)
	if err != nil {
		return nil, err
	}
	fmt.Printf("XXX %v %v %v\n", kind.MIME.Value, filetype.IsVideo(head), kind.Extension)
	if kind.MIME.Value != "image/gif" && !filetype.IsVideo(head) {
		return nil, nil
	}

	vpfps := &videoPHashFingerprinterState{}
	vpfps.formatContext = astiav.AllocFormatContext()
	if vpfps.formatContext == nil {
		return nil, errors.New("input format context is nil")
	}
	if err := vpfps.formatContext.OpenInput(filename, nil, nil); err != nil {
		return vpfps, fmt.Errorf("opening %v failed: %w", filename, err)
	}
	if err := vpfps.formatContext.FindStreamInfo(nil); err != nil {
		return vpfps, fmt.Errorf("finding stream info failed: %w", err)
	}

	for _, is := range vpfps.formatContext.Streams() {
		if is.CodecParameters().MediaType() != astiav.MediaTypeVideo {
			continue
		}
		if is.CodecParameters().Width() == 0 || is.CodecParameters().Height() == 0 {
			continue
		}
		if vpfps.codec = astiav.FindDecoder(is.CodecParameters().CodecID()); vpfps.codec == nil {
			return vpfps, errors.New("codec is nil")
		}
		if vpfps.codecContext = astiav.AllocCodecContext(vpfps.codec); vpfps.codecContext == nil {
			return vpfps, errors.New("codec context is nil")
		}
		if err := is.CodecParameters().ToCodecContext(vpfps.codecContext); err != nil {
			return vpfps, fmt.Errorf("updating codec context failed: %w", err)
		}
		if err := vpfps.codecContext.Open(vpfps.codec, nil); err != nil {
			return vpfps, fmt.Errorf("opening codec context failed: %w", err)
		}
		vpfps.width = is.CodecParameters().Width()
		vpfps.height = is.CodecParameters().Height()
		vpfps.pixelFormat = is.CodecParameters().PixelFormat()
		vpfps.stream = is
	}
	if vpfps.stream == nil {
		return nil, nil
	}
	return vpfps, nil
}

func (vpfps *videoPHashFingerprinterState) Get() ([]Fingerprint, error) {
	h, err := vpfps.GetRicop()
	if h == NoFingerprint {
		return nil, err
	}
	return []Fingerprint{h}, err
}

func (vpfps *videoPHashFingerprinterState) Cleanup() {
	if vpfps.formatContext != nil {
		vpfps.formatContext.CloseInput()
	}
	if vpfps.formatContext != nil {
		vpfps.formatContext.Free()
	}
	if vpfps.codecContext != nil {
		vpfps.codecContext.Free()
	}
}

func (vpfps *videoPHashFingerprinterState) readFrames(images chan *image.Image) {
	pkt := astiav.AllocPacket()
	defer pkt.Free()
	frame := astiav.AllocFrame()
	defer frame.Free()
	defer close(images)

	// Create software scale context
	swsCtx, err := astiav.CreateSoftwareScaleContext(
		vpfps.width, vpfps.height, vpfps.pixelFormat,
		32 /*dst width*/, 32, /*dst height*/
		astiav.PixelFormatGray8,
		astiav.NewSoftwareScaleContextFlags(astiav.SoftwareScaleContextFlagBilinear))
	if err != nil {
		// TODO: return error
		fmt.Printf("%v\n", fmt.Errorf("main: creating software scale context failed: %w", err))
	}
	defer swsCtx.Free()
	dstFrame := astiav.AllocFrame()
	defer dstFrame.Free()

	for {
		stop, err := func() (bool, error) {
			if err := vpfps.formatContext.ReadFrame(pkt); err != nil {
				if errors.Is(err, astiav.ErrEof) {
					return true, nil
				}
				return true, fmt.Errorf("reading frame failed: %w", err)
			}
			defer pkt.Unref()
			if pkt.StreamIndex() != vpfps.stream.Index() {
				return false, nil
			}
			if err := vpfps.codecContext.SendPacket(pkt); err != nil {
				return true, fmt.Errorf("main: sending packet failed: %w", err)
			}

			for {
				stop, err := func() (bool, error) {
					if err := vpfps.codecContext.ReceiveFrame(frame); err != nil {
						if errors.Is(err, astiav.ErrEof) || errors.Is(err, astiav.ErrEagain) {
							return true, nil
						}
						return true, fmt.Errorf("main: receiving frame failed: %w", err)
					}
					defer frame.Unref()
					if err := swsCtx.ScaleFrame(frame, dstFrame); err != nil {
						return true, err
					}
					i, err := dstFrame.Data().GuessImageFormat()
					if err != nil {
						return true, fmt.Errorf("guessing image format failed: %w", err)
					}
					if err := dstFrame.Data().ToImage(i); err != nil {
						return true, fmt.Errorf("getting frame's data as Go image failed: %w", err)
					}
					images <- &i
					return false, nil
				}()
				if err != nil {
					return true, err
				}
				if stop {
					break
				}
			}
			return false, nil
		}()
		if err != nil {
			// TODO: return error
			fmt.Printf("readFrame: %v", err)
			break
		}
		if stop {
			return
		}
	}
}

// algorithm:
// detect scene transitions (azr.DTC hash dist >=10)
// hash #1 (hashsum):
//
//	calculate azr.DTC for first image of each scene and xor all
//
// hash #2 (sceneHash):
//
//	plot number of scene transitions into a 32x32 image and run azr.DTC on it
//
// Hash format is {hashsum}.{scenehash} (128bit)
func (vpfps *videoPHashFingerprinterState) GetRicop() (Fingerprint, error) {
	images := make(chan *image.Image, 20)
	go vpfps.readFrames(images)
	var prevH uint64
	var l int
	var scenes []int
	var scenefps []uint64
	var nframes int
	for i := range images {
		nframes++
		h := azr.DTC(*i)
		if prevH == 0 {
			prevH = h
			scenefps = append(scenefps, h)
		}
		d := azr.Distance(prevH, h)
		if d < 10 {
			l++
		} else {
			scenes = append(scenes, l)
			scenefps = append(scenefps, h)
			l = 0
		}
		prevH = h
	}
	if nframes == 1 {
		return NoFingerprint, nil
	}
	scenes = append(scenes, l)
	pixels := make([]byte, 32*32)
	var frame float64
	framescale := float64(nframes) / 1024.0
	for _, n := range scenes {
		frame += float64(n)
		pos := uint(math.Floor(frame / framescale))
		if pos >= 1024 {
			pos = 1023
		}
		pixels[pos]++
	}
	sceneImg := &image.Gray{
		Rect:   image.Rect(0, 0, 32, 32),
		Pix:    pixels,
		Stride: 32,
	}
	sceneHash := azr.DTC(sceneImg)
	var hashsum uint64
	for _, h1 := range scenefps {
		hashsum ^= h1
	}
	return Fingerprint{
		Kind:    "VideoPHashRicop",
		Hash:    fmt.Sprintf("%08x.%08x", hashsum, sceneHash),
		Quality: 20,
	}, nil
}
