package fingerprint

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/asticode/go-astiav"
)

// AudioFingerprinter computes a Chromaprint (AcoustID) fingerprint of the
// best audio stream of any audio or video file.
type AudioFingerprinter struct{}

type audioFingerprinterState struct {
	formatContext *astiav.FormatContext
	codecContext  *astiav.CodecContext
	stream        *astiav.Stream
}

func init() {
	fingerprinters = append(fingerprinters, &AudioFingerprinter{})
}

func (afp *AudioFingerprinter) Init(filename string) (_ FingerprinterState, err error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("open %v: %v", filename, err)
	}
	ft := getFiletype(f)
	f.Close()
	if !strings.HasPrefix(ft, "audio/") && !strings.HasPrefix(ft, "video/") {
		return nil, nil
	}

	afps := &audioFingerprinterState{}
	// GetFingerprint doesn't call Cleanup when Init fails.
	defer func() {
		if err != nil {
			afps.Cleanup()
		}
	}()
	if afps.formatContext = astiav.AllocFormatContext(); afps.formatContext == nil {
		return nil, errors.New("input format context is nil")
	}
	if err := afps.formatContext.OpenInput(filename, nil, nil); err != nil {
		return nil, fmt.Errorf("opening %v failed: %w", filename, err)
	}
	if err := afps.formatContext.FindStreamInfo(nil); err != nil {
		return nil, fmt.Errorf("finding stream info failed: %w", err)
	}
	stream, codec, err := afps.formatContext.FindBestStream(astiav.MediaTypeAudio, -1, -1)
	if err != nil {
		if errors.Is(err, astiav.ErrStreamNotFound) {
			afps.Cleanup()
			return nil, nil
		}
		return nil, fmt.Errorf("finding audio stream failed: %w", err)
	}
	if codec == nil {
		return nil, fmt.Errorf("no decoder for %v", stream.CodecParameters().CodecID())
	}
	if afps.codecContext = astiav.AllocCodecContext(codec); afps.codecContext == nil {
		return nil, errors.New("codec context is nil")
	}
	if err := stream.CodecParameters().ToCodecContext(afps.codecContext); err != nil {
		return nil, fmt.Errorf("updating codec context failed: %w", err)
	}
	// See VideoPHashFingerprinter: keep decoding reproducible.
	afps.codecContext.SetThreadCount(1)
	if err := afps.codecContext.Open(codec, nil); err != nil {
		return nil, fmt.Errorf("opening codec context failed: %w", err)
	}
	afps.stream = stream
	return afps, nil
}

func (afps *audioFingerprinterState) Get() ([]Fingerprint, error) {
	fp, err := afps.getChromaprint()
	if err != nil || len(fp) == 0 {
		return nil, err
	}
	return []Fingerprint{
		{
			Kind:    "AudioChromaprint",
			Hash:    chromaprintEncode(fp),
			Quality: 20,
		},
		{
			Kind:    "AudioChromaprintSimHash",
			Hash:    fmt.Sprintf("%08x", chromaprintSimHash(fp)),
			Quality: 20,
		},
	}, nil
}

func (afps *audioFingerprinterState) Cleanup() {
	if afps.codecContext != nil {
		afps.codecContext.Free()
	}
	if afps.formatContext != nil {
		afps.formatContext.CloseInput()
		afps.formatContext.Free()
	}
}

// getChromaprint returns the raw Chromaprint fingerprint of the first
// chromaprintMaxDuration seconds of the audio stream.
func (afps *audioFingerprinterState) getChromaprint() ([]uint32, error) {
	r, err := newAudioResampler(afps.codecContext, afps.stream)
	if err != nil {
		return nil, err
	}
	defer r.free()

	pkt := astiav.AllocPacket()
	defer pkt.Free()
	frame := astiav.AllocFrame()
	defer frame.Free()

	// Like fpcalc, this doesn't drain the decoder at EOF (it never sends a
	// flush packet), only the resampler.
	for len(r.samples) < r.maxSamples {
		if err := afps.formatContext.ReadFrame(pkt); err != nil {
			if errors.Is(err, astiav.ErrEof) {
				break
			}
			return nil, fmt.Errorf("reading frame failed: %w", err)
		}
		if pkt.StreamIndex() != afps.stream.Index() {
			pkt.Unref()
			continue
		}
		err := afps.codecContext.SendPacket(pkt)
		pkt.Unref()
		if err != nil && !errors.Is(err, astiav.ErrEagain) {
			return nil, fmt.Errorf("sending packet failed: %w", err)
		}
		for {
			if err := afps.codecContext.ReceiveFrame(frame); err != nil {
				if errors.Is(err, astiav.ErrEof) || errors.Is(err, astiav.ErrEagain) {
					break
				}
				return nil, fmt.Errorf("receiving frame failed: %w", err)
			}
			err := r.add(frame)
			frame.Unref()
			if err != nil {
				return nil, err
			}
		}
	}
	if err := r.add(nil); err != nil {
		return nil, err
	}
	return chromaprint(r.samples[:min(len(r.samples), r.maxSamples)]), nil
}

// audioResampler converts decoded audio to mono s16 at
// chromaprintSampleRate using the same libswresample settings as fpcalc
// (see chromaprint's audio/ffmpeg_audio_processor_swresample.h).
type audioResampler struct {
	graph      *astiav.FilterGraph
	src        *astiav.BuffersrcFilterContext
	sink       *astiav.BuffersinkFilterContext
	out        *astiav.Frame
	samples    []int16
	maxSamples int
}

const chromaprintResampleFilter = "aresample=" +
	"out_sample_rate=11025:out_chlayout=mono:out_sample_fmt=s16:" +
	"resampler=swr:filter_size=16:phase_shift=8:linear_interp=1:cutoff=0.8," +
	"aformat=sample_fmts=s16:channel_layouts=mono:sample_rates=11025"

func newAudioResampler(cc *astiav.CodecContext, stream *astiav.Stream) (r *audioResampler, err error) {
	r = &audioResampler{maxSamples: chromaprintMaxDuration * chromaprintSampleRate}
	defer func() {
		if err != nil {
			r.free()
			r = nil
		}
	}()
	if r.graph = astiav.AllocFilterGraph(); r.graph == nil {
		return nil, errors.New("filter graph is nil")
	}
	// Single-threaded for reproducibility, like the decoder.
	r.graph.SetThreadCount(1)
	if r.src, err = r.graph.NewBuffersrcFilterContext(astiav.FindFilterByName("abuffer"), "in"); err != nil {
		return nil, fmt.Errorf("creating abuffer failed: %w", err)
	}
	if r.sink, err = r.graph.NewBuffersinkFilterContext(astiav.FindFilterByName("abuffersink"), "out"); err != nil {
		return nil, fmt.Errorf("creating abuffersink failed: %w", err)
	}
	params := astiav.AllocBuffersrcFilterContextParameters()
	defer params.Free()
	params.SetChannelLayout(cc.ChannelLayout())
	params.SetSampleFormat(cc.SampleFormat())
	params.SetSampleRate(cc.SampleRate())
	params.SetTimeBase(stream.TimeBase())
	if err := r.src.SetParameters(params); err != nil {
		return nil, fmt.Errorf("setting abuffer parameters failed: %w", err)
	}
	if err := r.src.Initialize(nil); err != nil {
		return nil, fmt.Errorf("initializing abuffer failed: %w", err)
	}

	outputs := astiav.AllocFilterInOut()
	defer outputs.Free()
	outputs.SetName("in")
	outputs.SetFilterContext(r.src.FilterContext())
	outputs.SetPadIdx(0)
	outputs.SetNext(nil)
	inputs := astiav.AllocFilterInOut()
	defer inputs.Free()
	inputs.SetName("out")
	inputs.SetFilterContext(r.sink.FilterContext())
	inputs.SetPadIdx(0)
	inputs.SetNext(nil)
	if err := r.graph.Parse(chromaprintResampleFilter, inputs, outputs); err != nil {
		return nil, fmt.Errorf("parsing filter graph failed: %w", err)
	}
	if err := r.graph.Configure(); err != nil {
		return nil, fmt.Errorf("configuring filter graph failed: %w", err)
	}
	r.out = astiav.AllocFrame()
	return r, nil
}

// add feeds a decoded frame (or nil to flush) through the resampler and
// appends the resulting samples to r.samples.
func (r *audioResampler) add(f *astiav.Frame) error {
	if err := r.src.AddFrame(f, astiav.NewBuffersrcFlags(astiav.BuffersrcFlagKeepRef)); err != nil {
		return fmt.Errorf("adding frame to filter graph failed: %w", err)
	}
	for {
		if err := r.sink.GetFrame(r.out, astiav.NewBuffersinkFlags()); err != nil {
			if errors.Is(err, astiav.ErrEof) || errors.Is(err, astiav.ErrEagain) {
				return nil
			}
			return fmt.Errorf("getting frame from filter graph failed: %w", err)
		}
		b, err := r.out.Data().Bytes(1)
		n := r.out.NbSamples()
		r.out.Unref()
		if err != nil {
			return fmt.Errorf("getting resampled audio failed: %w", err)
		}
		if len(b) < 2*n {
			return fmt.Errorf("short resampled audio frame: %d bytes for %d samples", len(b), n)
		}
		for i := range n {
			r.samples = append(r.samples, int16(uint16(b[2*i])|uint16(b[2*i+1])<<8))
		}
	}
}

func (r *audioResampler) free() {
	if r.out != nil {
		r.out.Free()
	}
	if r.graph != nil {
		r.graph.Free()
	}
}
