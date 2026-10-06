package fingerprint

import (
	"errors"
	"fmt"
	"image"
	"os"
	"strings"
	"sync"

	"github.com/asticode/go-astiav"
	azr "github.com/azr/phash"
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

	// readErr is set by readFrames if frame decoding stops early due to an
	// error rather than reaching EOF. It's written once, before readFrames
	// returns (which signals wg.Done()), and only read after wg.Wait()
	// returns in frameHashes, so the sync.WaitGroup provides the necessary
	// happens-before edge without an explicit mutex.
	readErr error
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
	ft := getFiletype(f)
	if !strings.HasPrefix(ft, "video/") && ft != "image/gif" {
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
		// Force single-threaded decoding so the frame sequence (and thus
		// the resulting hash) is reproducible across machines/runs,
		// rather than depending on however many threads ffmpeg's default
		// (CPU-count-based) thread count happens to pick.
		vpfps.codecContext.SetThreadCount(1)
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
	hs, err := vpfps.frameHashes()
	if err != nil {
		return nil, err
	}
	if len(hs) <= 1 {
		return nil, nil
	}
	return []Fingerprint{videoSimHash(hs)}, nil
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

func (vpfps *videoPHashFingerprinterState) readFrames(images chan *image.Image, wg *sync.WaitGroup) {
	defer wg.Done()
	pkt := astiav.AllocPacket()
	defer pkt.Free()
	frame := astiav.AllocFrame()
	defer frame.Free()

	// Create software scale context
	swsCtx, err := astiav.CreateSoftwareScaleContext(
		vpfps.width, vpfps.height, vpfps.pixelFormat,
		32 /*dst width*/, 32, /*dst height*/
		astiav.PixelFormatGray8,
		astiav.NewSoftwareScaleContextFlags(astiav.SoftwareScaleContextFlagBilinear))
	if err != nil {
		vpfps.readErr = fmt.Errorf("creating software scale context failed: %w", err)
		return
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
			vpfps.readErr = err
			return
		}
		if stop {
			return
		}
	}
}

// frameHashes decodes every frame of the video and returns its azr.DTC
// perceptual hash.
func (vpfps *videoPHashFingerprinterState) frameHashes() ([]uint64, error) {
	images := make(chan *image.Image, 20)
	var wg sync.WaitGroup
	wg.Add(1)
	go vpfps.readFrames(images, &wg)
	go func() {
		wg.Wait()
		close(images)
	}()
	var hs []uint64
	for i := range images {
		hs = append(hs, azr.DTC(*i))
	}
	if err := vpfps.readErr; err != nil {
		return nil, fmt.Errorf("reading video frames: %w", err)
	}
	return hs, nil
}

// videoSimHash returns the bitwise majority of the frames' perceptual
// hashes: bit i is set iff it's set in more than half of the frames (like
// Chromaprint's SimHash for audio). It ignores frame order and count, so
// it's robust to re-encoding, resolution and frame rate changes, and
// dropped or duplicated frames; similar videos get hashes within a small
// Hamming distance of each other (re-encodes of the same clip are
// typically within 0-6 bits, unrelated videos 24+ bits apart).
func videoSimHash(hs []uint64) Fingerprint {
	var v [64]int
	for _, h := range hs {
		for j := range v {
			v[j] += int(h >> j & 1)
		}
	}
	var hash uint64
	for j, n := range v {
		if 2*n > len(hs) {
			hash |= 1 << j
		}
	}
	return Fingerprint{
		Kind:    "VideoPHashSimHash",
		Hash:    fmt.Sprintf("%016x", hash),
		Quality: 20,
	}
}
