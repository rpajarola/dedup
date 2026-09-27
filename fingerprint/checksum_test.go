package fingerprint

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestChecksumFingerprinter(t *testing.T) {
	t.Parallel()
	fp := &ChecksumFingerprinter{}
	for _, tc := range getTestCases(t, testDataDir) {
		t.Run(filepath.Base(tc.Name), func(t *testing.T) {
			t.Parallel()
			if tc.Got.GetChecksum() == nil {
				tc.Got.Checksum = &ChecksumTestCase{}
			}
			if tc.Got.ImgPhash.Skip {
				t.Skip()
			}
			if tc.Got.Checksum.VerifiedCrc32 != "" &&
				tc.Got.Checksum.VerifiedMd5 != "" &&
				tc.Got.Checksum.VerifiedSha1 != "" {
				t.Skip()
			}
			fps, e := fp.Init(tc.SourceFile)
			if e != nil {
				t.Fatalf("fp.Init(%v): %v", tc.SourceFile, e)
			}

			tc.Got.Checksum.VerifiedCrc32 = runCmd(t, "crc32", tc.SourceFile)
			tc.Got.Checksum.VerifiedMd5 = runCmd(t, "md5sum", tc.SourceFile)
			tc.Got.Checksum.VerifiedSha1 = runCmd(t, "sha1sum", tc.SourceFile)
			csfps, e := fps.Get()
			if e != nil {
				t.Fatalf("fps.Get(%v): %v", tc.SourceFile, e)
			}
			for _, csfp := range csfps {
				switch csfp.Kind {
				case "CRC32":
					tc.Got.Checksum.Crc32 = csfp.Hash
				case "MD5":
					tc.Got.Checksum.Md5 = csfp.Hash
				case "SHA1":
					tc.Got.Checksum.Sha1 = csfp.Hash
				}
			}
			c := tc.Got.Checksum
			if c.Crc32 != c.VerifiedCrc32 {
				t.Errorf("CRC32 mismatch: got %q want verified %q", c.Crc32, c.VerifiedCrc32)
			}
			if c.Md5 != c.VerifiedMd5 {
				t.Errorf("MD5 mismatch: got %q want verified %q", c.Md5, c.VerifiedMd5)
			}
			if c.Sha1 != c.VerifiedSha1 {
				t.Errorf("SHA1 mismatch: got %q want verified %q", c.Sha1, c.VerifiedSha1)
			}
			maybeUpdateTestCase(t, tc)
		})
	}
}

func runCmd(t *testing.T, algo string, filename string) string {
	t.Helper()
	cmd := exec.Command(algo, filename)
	stdout, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run %v: %v", algo, err)
	}
	// The hash is always the first whitespace-delimited token: md5sum/sha1sum
	// follow it with "  filename", and crc32 (see /usr/bin/crc32) may append
	// "\tOK" or "\tBAD <crc> != <hex-run-from-filename>" if the filename
	// happens to contain an 8-hex-digit substring (a scene-release CRC32
	// convention), which for a single file isn't preceded by the filename
	// itself and would otherwise get kept as part of the checksum.
	fields := strings.Fields(string(stdout))
	if len(fields) == 0 {
		t.Fatalf("run %v: no output", algo)
	}
	return fields[0]
}
