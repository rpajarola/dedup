package fingerprint

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/bzip2"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/bodgit/sevenzip"
	"github.com/klauspost/compress/zstd"
	"github.com/nwaples/rardecode/v2"
	"github.com/ulikunitz/xz"
	"golang.org/x/text/unicode/norm"
)

// ArchiveFingerprinter fingerprints the contents of archives (zip, tar,
// 7z, rar) and compressed files (gzip, bzip2, xz, zstd, including
// compressed tarballs), independent of the container format and
// compression used, so that repacked copies of the same files match.
//
// It emits two fingerprints:
//
//   - ArchiveContentTree: a hash of every file's (path, content hash).
//   - ArchiveContentSet: a hash of just the multiset of file content hashes,
//     so it also matches when files were renamed or moved around.
//
// Both are computed after canonicalizing the file list (see
// canonicalizeArchiveEntries) to ignore differences commonly introduced by
// repacking: directory and symlink entries, file metadata, a common leading
// directory ("./", "archive-name/"), OS clutter like __MACOSX/ and
// .DS_Store, and top-level release noise like READMEs, file_id.diz, .nfo
// files and checksum lists.
type ArchiveFingerprinter struct{}

type archiveFingerprinterState struct {
	filename string
	mimeType string
}

type archiveEntry struct {
	name string // as stored in the archive
	hash [sha256.Size]byte
}

func init() {
	fingerprinters = append(fingerprinters, &ArchiveFingerprinter{})
}

func (afp *ArchiveFingerprinter) Init(filename string) (FingerprinterState, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("open %v: %v", filename, err)
	}
	ft := getFiletype(f)
	f.Close()
	switch ft {
	case "application/zip", "application/x-tar", "application/x-7z-compressed", "application/vnd.rar",
		"application/gzip", "application/x-bzip2", "application/x-xz", "application/zstd":
		return &archiveFingerprinterState{filename: filename, mimeType: ft}, nil
	}
	return nil, nil
}

func (afps *archiveFingerprinterState) Get() ([]Fingerprint, error) {
	entries, err := readArchive(afps.filename, afps.mimeType)
	if err != nil {
		return nil, fmt.Errorf("reading archive %v: %w", afps.filename, err)
	}
	return archiveFingerprints(entries), nil
}

// archiveFingerprints returns the ArchiveContentTree and ArchiveContentSet
// fingerprints of a list of files (from an archive or disk image), or nil
// if there are no files left after canonicalization.
func archiveFingerprints(entries []archiveEntry) []Fingerprint {
	entries = canonicalizeArchiveEntries(entries)
	if len(entries) == 0 {
		return nil
	}
	tree, set := archiveHashes(entries)
	return []Fingerprint{
		{Kind: "ArchiveContentTree", Hash: tree, Quality: 20},
		{Kind: "ArchiveContentSet", Hash: set, Quality: 20},
	}
}

func (afps *archiveFingerprinterState) Cleanup() {}

// archiveHashes returns the ArchiveContentTree and ArchiveContentSet hashes
// of canonicalized entries.
func archiveHashes(entries []archiveEntry) (tree, set string) {
	slices.SortFunc(entries, func(a, b archiveEntry) int { return strings.Compare(a.name, b.name) })
	th := sha256.New()
	for _, e := range entries {
		fmt.Fprintf(th, "%s\x00%x\n", e.name, e.hash)
	}
	hashes := make([]string, len(entries))
	for i, e := range entries {
		hashes[i] = hex.EncodeToString(e.hash[:])
	}
	slices.Sort(hashes)
	sh := sha256.New()
	for _, h := range hashes {
		fmt.Fprintf(sh, "%s\n", h)
	}
	return hex.EncodeToString(th.Sum(nil)), hex.EncodeToString(sh.Sum(nil))
}

// readArchive returns the regular files in the archive with their content
// hashes, in archive order. Directories, symlinks and other special
// entries are skipped.
func readArchive(filename, mimeType string) ([]archiveEntry, error) {
	switch mimeType {
	case "application/zip":
		return readZip(filename)
	case "application/x-7z-compressed":
		return read7z(filename)
	case "application/vnd.rar":
		return readRar(filename)
	}

	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if mimeType == "application/x-tar" {
		return readTar(f)
	}

	// A single compressed stream: either a compressed tarball, or a
	// compressed single file, which is treated as an archive containing
	// just that file, named like gunzip/unxz/etc. would (by dropping the
	// extension; gzip's optional original-name header is ignored so that
	// all compressors behave the same).
	name := strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))
	var r io.Reader
	switch mimeType {
	case "application/gzip":
		zr, err := gzip.NewReader(f)
		if err != nil {
			return nil, err
		}
		defer zr.Close()
		r = zr
	case "application/x-bzip2":
		r = bzip2.NewReader(f)
	case "application/x-xz":
		if r, err = xz.NewReader(f); err != nil {
			return nil, err
		}
	case "application/zstd":
		zr, err := zstd.NewReader(f)
		if err != nil {
			return nil, err
		}
		defer zr.Close()
		r = zr
	default:
		return nil, fmt.Errorf("unsupported archive type %v", mimeType)
	}
	br := bufio.NewReader(r)
	if head, _ := br.Peek(512); isTarHeader(head) {
		return readTar(br)
	}
	h, err := hashReader(br)
	if err != nil {
		return nil, err
	}
	return []archiveEntry{{name: name, hash: h}}, nil
}

func hashReader(r io.Reader) ([sha256.Size]byte, error) {
	var res [sha256.Size]byte
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return res, err
	}
	copy(res[:], h.Sum(nil))
	return res, nil
}

// isTarHeader reports whether b starts with a tar header block with a
// valid checksum.
func isTarHeader(b []byte) bool {
	if len(b) < 512 {
		return false
	}
	want, err := strconv.ParseUint(strings.Trim(string(b[148:156]), " \x00"), 8, 64)
	if err != nil {
		return false
	}
	var sum uint64
	for i, c := range b[:512] {
		if i >= 148 && i < 156 {
			c = ' ' // the checksum field itself counts as spaces
		}
		sum += uint64(c)
	}
	return sum == want
}

func readTar(r io.Reader) ([]archiveEntry, error) {
	var res []archiveEntry
	byName := map[string][sha256.Size]byte{}
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return res, nil
		}
		if err != nil {
			return nil, err
		}
		switch hdr.Typeflag {
		case tar.TypeReg:
			h, err := hashReader(tr)
			if err != nil {
				return nil, fmt.Errorf("%v: %w", hdr.Name, err)
			}
			byName[path.Clean(hdr.Name)] = h
			res = append(res, archiveEntry{name: hdr.Name, hash: h})
		case tar.TypeLink:
			// Hard links have no content of their own; repacking
			// to most other formats turns them into regular copies.
			if h, ok := byName[path.Clean(hdr.Linkname)]; ok {
				res = append(res, archiveEntry{name: hdr.Name, hash: h})
			}
		}
	}
}

func readZip(filename string) ([]archiveEntry, error) {
	zr, err := zip.OpenReader(filename)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	var res []archiveEntry
	for _, f := range zr.File {
		if !f.Mode().IsRegular() {
			continue
		}
		h, err := hashZipFile(f)
		if err != nil {
			return nil, fmt.Errorf("%v: %w", f.Name, err)
		}
		res = append(res, archiveEntry{name: f.Name, hash: h})
	}
	return res, nil
}

func hashZipFile(f *zip.File) ([sha256.Size]byte, error) {
	r, err := f.Open()
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	defer r.Close()
	return hashReader(r)
}

func read7z(filename string) ([]archiveEntry, error) {
	zr, err := sevenzip.OpenReader(filename)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	var res []archiveEntry
	for _, f := range zr.File {
		if !f.FileInfo().Mode().IsRegular() {
			continue
		}
		h, err := hash7zFile(f)
		if err != nil {
			return nil, fmt.Errorf("%v: %w", f.Name, err)
		}
		res = append(res, archiveEntry{name: f.Name, hash: h})
	}
	return res, nil
}

func hash7zFile(f *sevenzip.File) ([sha256.Size]byte, error) {
	r, err := f.Open()
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	defer r.Close()
	return hashReader(r)
}

func readRar(filename string) ([]archiveEntry, error) {
	rr, err := rardecode.OpenReader(filename)
	if err != nil {
		return nil, err
	}
	defer rr.Close()
	var res []archiveEntry
	for {
		hdr, err := rr.Next()
		if err == io.EOF {
			return res, nil
		}
		if err != nil {
			return nil, err
		}
		if hdr.IsDir || hdr.LinkType != 0 {
			continue
		}
		h, err := hashReader(rr)
		if err != nil {
			return nil, fmt.Errorf("%v: %w", hdr.Name, err)
		}
		res = append(res, archiveEntry{name: hdr.Name, hash: h})
	}
}

// canonicalizeArchiveEntries normalizes entry names and drops entries that
// are commonly added, removed or renamed when an archive is repacked:
//
//  1. Names are normalized: "\" becomes "/", Unicode is NFC (macOS
//     produces NFD), and leading "/", "./" and ".." are removed. If a name
//     occurs more than once, the last entry wins (as when extracting).
//  2. OS clutter (__MACOSX/, .DS_Store, ._*, Thumbs.db, desktop.ini) is
//     dropped anywhere.
//  3. Release noise (READMEs, file_id.diz, .nfo, checksum lists, ...; see
//     isArchiveReleaseNoise) is dropped if it's at the top level, i.e. in
//     the common directory of all other files or one of its ancestors.
//     Deeper down (e.g. "src/README") it's considered content. If the
//     archive consists of nothing but release noise, it's all kept.
//  4. The common leading directory of all remaining files is removed.
func canonicalizeArchiveEntries(entries []archiveEntry) []archiveEntry {
	var files []archiveEntry
	seen := map[string]int{}
	for _, e := range entries {
		e.name = normalizeArchivePath(e.name)
		if e.name == "" || isArchiveOSClutter(e.name) {
			continue
		}
		if i, ok := seen[e.name]; ok {
			files[i] = e
			continue
		}
		seen[e.name] = len(files)
		files = append(files, e)
	}

	var content, noise []archiveEntry
	for _, e := range files {
		if isArchiveReleaseNoise(path.Base(e.name)) {
			noise = append(noise, e)
		} else {
			content = append(content, e)
		}
	}
	if len(content) == 0 {
		content, noise = noise, nil
	}
	top := archiveCommonDir(content)
	for _, e := range noise {
		dir := path.Dir(e.name)
		if dir == "." || dir == top || strings.HasPrefix(top, dir+"/") {
			continue
		}
		content = append(content, e)
	}

	if prefix := archiveCommonDir(content); prefix != "" {
		for i := range content {
			content[i].name = strings.TrimPrefix(content[i].name, prefix+"/")
		}
	}
	return content
}

func normalizeArchivePath(name string) string {
	name = strings.ReplaceAll(name, `\`, "/")
	name = norm.NFC.String(name)
	return strings.TrimPrefix(path.Clean("/"+name), "/")
}

// archiveCommonDir returns the longest directory path containing all
// entries, or "" if there is none.
func archiveCommonDir(entries []archiveEntry) string {
	var common []string
	for i, e := range entries {
		dirs := strings.Split(e.name, "/")
		dirs = dirs[:len(dirs)-1]
		if i == 0 {
			common = dirs
			continue
		}
		n := 0
		for n < len(common) && n < len(dirs) && common[n] == dirs[n] {
			n++
		}
		common = common[:n]
	}
	return strings.Join(common, "/")
}

func isArchiveOSClutter(name string) bool {
	for _, c := range strings.Split(name, "/") {
		if strings.EqualFold(c, "__MACOSX") {
			return true
		}
	}
	base := strings.ToLower(path.Base(name))
	switch base {
	case ".ds_store", "thumbs.db", "desktop.ini":
		return true
	}
	return strings.HasPrefix(base, "._")
}

// isArchiveReleaseNoise reports whether a file name looks like something
// that's commonly added to (or removed from) an archive when it's
// redistributed, rather than part of its actual content: READMEs, BBS/scene
// descriptions, file listings, checksum lists and download links.
func isArchiveReleaseNoise(base string) bool {
	b := strings.ToLower(base)
	switch b {
	case "file_id.diz", "descript.ion", "files.bbs", "00index.txt", "index", "index.txt",
		"md5sums", "sha1sums", "sha256sums", "sha512sums", "checksums", "checksums.txt":
		return true
	}
	for _, p := range []string{"readme", "read.me", "read_me", "read-me"} {
		if strings.HasPrefix(b, p) {
			return true
		}
	}
	switch path.Ext(b) {
	case ".nfo", ".diz", ".sfv", ".md5", ".sha1", ".sha256", ".sha512", ".par2", ".url":
		return true
	}
	return false
}
