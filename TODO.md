# Known issues

## Solid RAR5 archives with empty files can't be fingerprinted

`ArchiveFingerprinter` uses `github.com/nwaples/rardecode/v2`, which (as of v2.4.1) can't
read past an empty file in a solid RAR5 archive: the following `Next()` fails with
`rardecode: decoder expected more data than is in packed file`
([nwaples/rardecode#70](https://github.com/nwaples/rardecode/issues/70)). Since the
remaining files can't be reached, such archives get an error instead of (wrong) partial
fingerprints. `fingerprint/testdata/archive_proj_solid.rar.textproto` records this as an
expected error; it'll show up as a test diff once a fixed rardecode is pulled in.
