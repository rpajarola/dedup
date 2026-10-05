# FAQ

## Dependencies

### Protobuf

```
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install github.com/protocolbuffers/txtpbfmt/cmd/txtpbfmt@latest
export PATH="$PATH:$(go env GOPATH)/bin"
```

(easier and more reliable than using the OS package even if it is
available).

### FFMPEG

This requires ffmpeg >= 7.x

#### Ubuntu

```
sudo add-apt-repository ppa:ubuntuhandbook1/ffmpeg7
sudo apt install libavcodec-dev libavdevice-dev libavfilter-dev libavformat-dev libswresample-dev libswscale-dev libavutil-dev
```

#### MacOS X

```
brew install ffmpeg
```

## Tests


### Platforms

I am testing on

* Ubuntu 24.04 LTS on x64.
* Mac OS X Sonoma 14.4 on ARM

### Testdata

Media files used by `fingerprint`'s tests live in the separate
[rpajarola/dedup-testdata](https://github.com/rpajarola/dedup-testdata) repo, distributed as
tarballs attached to its GitHub Releases (split by media type: `testdata-images.tar.gz`,
`testdata-videos.tar.gz`, `testdata-audio.tar.gz`, `testdata-archives.tar.gz`, and
`testdata-noimage.tar.gz` for the EXIF-only placeholder fixtures).
There's a single `fingerprint/testdata/` directory; only `.textproto` files are committed here,
everything else is fetched on demand.

### Fetch testdata

Nothing to do manually: `go test` downloads and extracts the release tarballs into
`fingerprint/testdata/` automatically whenever a file referenced by a `.textproto` is
missing.

### Add testdata

1. Add the new source file plus its `.textproto` (with a `source_file` field pointing at it) to
   `fingerprint/testdata/` locally.
2. Add the file to the appropriate tarball (images/videos/audio/archives/etc.) and upload it as a new release
   asset on `rpajarola/dedup-testdata`, e.g.:
   ```
   tar -czf testdata-images.tar.gz -C fingerprint/testdata <new_file>
   gh release upload <version> testdata-images.tar.gz --clobber --repo rpajarola/dedup-testdata
   ```
3. Commit the `.textproto` in the main repo (the binary file itself is gitignored and fetched on
   demand, so don't commit it here).

### Audio fingerprints

`AudioChromaprint` is a pure Go port of Chromaprint (fingerprint/chromaprint.go)
and is meant to be bit-identical to the reference implementation. To cross-check
a file against it:

```
brew install chromaprint   # or: sudo apt install libchromaprint-tools
fpcalc <file>              # FINGERPRINT= should equal AudioChromaprint
```

### Unstable tests

Decoding of video is fickly, and some of the hashes need fixing to be
stable between platforms. See [TODO.md](TODO.md) for the current list
of known-flaky fixtures and other open issues.
