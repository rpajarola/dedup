# Known issues

## Solid RAR5 archives with empty files can't be fingerprinted

`ArchiveFingerprinter` uses `github.com/nwaples/rardecode/v2`, which (as of v2.4.1) can't
read past an empty file in a solid RAR5 archive: the following `Next()` fails with
`rardecode: decoder expected more data than is in packed file`
([nwaples/rardecode#70](https://github.com/nwaples/rardecode/issues/70)). Since the
remaining files can't be reached, such archives get an error instead of (wrong) partial
fingerprints. `fingerprint/testdata/archive_proj_solid.rar.textproto` records this as an
expected error; it'll show up as a test diff once a fixed rardecode is pulled in.

## go-diskfs: FAT short names with non-ASCII characters

`DiskImageFingerprinter` reads FAT images with `github.com/diskfs/go-diskfs` (v1.9.4). A name
that fits 8.3 and is all lowercase (e.g. `café.txt`) is stored by Windows and mtools as a short
name in the DOS code page (é = 0x82) with the "lowercase" flag set, and no long name. go-diskfs
applies `strings.ToLower` to the raw code page bytes, which turns 0x82 into U+FFFD, so the
original character is lost before `decodeFATShortNames` can decode it as CP437. Such files still
count towards `ArchiveContentSet`, but `ArchiveContentTree` won't match other copies of the
same files (e.g. `fingerprint/testdata/disk_proj_{floppy,fat16,fat32}.img` vs
`archive_proj.zip`). Needs a fix upstream (lowercase only ASCII A-Z, as DOS/Windows do).

Also worked around in `diskimage.go`, to be removed once fixed in a go-diskfs release:
`GetFilesystem` passing the 512-byte sector size as the squashfs block size, FAT12/16 failing to
`Stat` the root directory, and FAT failing to open empty files (diskfs/go-diskfs#417, closed
but not yet released).
