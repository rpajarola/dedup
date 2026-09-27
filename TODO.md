# Known issues

## Flaky test fixtures (non-deterministic across runs)

These produce different hash values on different `go test` runs, unrelated to any
recent change. Cause not yet investigated — possibly race conditions in the
underlying phash/decode libraries under `t.Parallel()`, or genuine algorithm
non-determinism.

- `fingerprint/large_testdata/BigBuckBunny.avi.textproto`,
  `BigBuckBunny.gif.textproto`, `BigBuckBunny.ts.textproto`,
  `computerchess5.flv.textproto`, `druid_peak_trailer_2014.mp4.textproto` —
  `VideoPhash.WantRicopHash` varies between runs. Already called out in
  `FAQ.md`'s "Unstable tests" section.
- `fingerprint/large_testdata/Nikon D300S 2918764486.jpg.textproto` —
  `ImgPHash.WantAzrHash` / the `ImgPHashAzr` fingerprint varies between runs.
- `fingerprint/large_testdata/heic.digital iPhone 12 Pro 2.heic.textproto` —
  the `EXIFModelSerialPhotoID` fingerprint is intermittently missing from
  `GetFingerprint`'s output.

## RAW preview/thumbnail extraction gaps

`ImgPHashFingerprinter` (fingerprint/imgphash.go) now falls back to the embedded
EXIF preview/thumbnail (via `exiftools`' `Exif.PreviewImage()`) when a RAW file's
primary image data can't be decoded directly. This works for most ARW/CR2/DNG
fixtures, but not all:

- `fingerprint/large_testdata/Canon_EOS_R10_IMG_0683.CR3.textproto` — CR3 uses an
  ISO-BMFF (MP4-like) container, not TIFF, so `exif.Decode`'s TIFF-based parsing
  never even runs. Would need a CR3-specific preview extractor.
- `fingerprint/large_testdata/IPHON8PLUShSLI0020NRD-IMG_0218.dng.textproto` and
  `IPHON8PLUShSLI0040NRD-IMG_0219.dng.textproto` — `PreviewImage()` finds nothing
  for these two specific DNGs even though it works for the other DNG fixtures.
  Not investigated further; possibly a different preview tag/IFD layout for this
  capture pipeline (iPhone 8 Plus DNG-from-jpeg workflow).

## Checksum verification quirk

`fingerprint/large_testdata/PXL_20240818_015751859.RAW-01.COVER.dng.textproto`'s
`checksum.verified_crc32` reads `"be9154ef\tBAD be9154ef != 20240818"`. The
`crc32` CLI tool that `checksum_test.go`'s `runCmd` shells out to appears to be
parsing a digit sequence out of the filename itself (`..._20240818_...`) as an
"expected" checksum to compare against, rather than treating it as opaque. Not
a bug in this codebase per se, but the reliance on an external `crc32` binary
with undocumented filename-parsing behavior is fragile — worth understanding or
replacing with a pure-Go CRC32 comparison.
