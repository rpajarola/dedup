# Known issues

## VideoPhash is experimental, not production-ready

`fingerprint/testdata/BigBuckBunny.avi.textproto`, `BigBuckBunny.gif.textproto`,
`BigBuckBunny.ts.textproto`, `computerchess5.flv.textproto`, and
`druid_peak_trailer_2014.mp4.textproto` all show a different `VideoPhash.WantRicopHash`
on every `go test` run. `VideoPhash` is experimental and not ready for prime time — this
is expected for now, not a bug to chase. Already called out in `FAQ.md`'s "Unstable
tests" section.

(Two other fixtures that looked flaky the same way — `Nikon D300S 2918764486.jpg.textproto`'s
`ImgPHashAzr` and `heic.digital iPhone 12 Pro 2.heic.textproto`'s missing
`EXIFModelSerialPhotoID` — turned out not to be flaky at all: both were deterministic bugs
that happened to look intermittent because leftover `.new` files from a prior failing run
were getting picked up by `readTestCase` as the new "want" baseline in later runs, e.g. across
runs of a naive `for i in 1 2 3; do go test ...; done` loop. The Nikon one needed an updated
hash; the heic one was actually a real HEIF EXIF-decode bug in `exiftools` — see below. Both
are now fixed and no longer produce diffs.)

## exiftools' TestDecode assumes every fixture has EXIF

`exiftools/exif/exif_test.go`'s `TestDecode` iterates every `.jpg` in the shared testdata
directory and fails if `Decode` doesn't return usable EXIF. It now fails on
`imgphash_cat_sky.jpg`, `imgphash_cat_medium.jpg`, and `imgphash_cat_smiling.jpg` — real
photos with no EXIF at all, added to the shared corpus when `dedup`'s `testdata/` and
`large_testdata/` were merged. Pre-existing fallout from that merge, not from anything in
this repo; needs a fix in `exiftools` itself (either skip files with no EXIF, or special-case
these three).

## RAW preview/thumbnail extraction gaps

`ImgPHashFingerprinter` (fingerprint/imgphash.go) now falls back to the embedded
EXIF preview/thumbnail (via `exiftools`' `Exif.PreviewImage()`) when a RAW file's
primary image data can't be decoded directly. This works for most ARW/CR2/DNG
fixtures, but not all:

- `fingerprint/testdata/Canon_EOS_R10_IMG_0683.CR3.textproto` — CR3 uses an
  ISO-BMFF (MP4-like) container, not TIFF, so `exif.Decode`'s TIFF-based parsing
  never even runs. Would need a CR3-specific preview extractor.
- `fingerprint/testdata/IPHON8PLUShSLI0020NRD-IMG_0218.dng.textproto` and
  `IPHON8PLUShSLI0040NRD-IMG_0219.dng.textproto` — `PreviewImage()` finds nothing
  for these two specific DNGs even though it works for the other DNG fixtures.
  Not investigated further; possibly a different preview tag/IFD layout for this
  capture pipeline (iPhone 8 Plus DNG-from-jpeg workflow).


## Solid RAR5 archives with empty files can't be fingerprinted

`ArchiveFingerprinter` uses `github.com/nwaples/rardecode/v2`, which (as of v2.4.1) can't
read past an empty file in a solid RAR5 archive: the following `Next()` fails with
`rardecode: decoder expected more data than is in packed file`
([nwaples/rardecode#70](https://github.com/nwaples/rardecode/issues/70)). Since the
remaining files can't be reached, such archives get an error instead of (wrong) partial
fingerprints. `fingerprint/testdata/archive_proj_solid.rar.textproto` records this as an
expected error; it'll show up as a test diff once a fixed rardecode is pulled in.
