package buildkit

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
var revisionPattern = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)
var refPattern = regexp.MustCompile(`^[A-Za-z0-9_./-]{1,200}$`)
var client = &http.Client{Timeout: 15 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
	if req.URL.Scheme != "https" {
		return errors.New("refusing a non-HTTPS download redirect")
	}
	if len(via) > 10 {
		return errors.New("too many redirects")
	}
	return nil
}}

func get(ctx context.Context, address, token string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "alsayf-buildkit")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download failed: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("download returned HTTP %d from %s", response.StatusCode, req.URL.Host)
	}
	return response, nil
}
func readJSON(ctx context.Context, address, token string, value any) error {
	response, err := get(ctx, address, token)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	return json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(value)
}
func download(ctx context.Context, address, token, destination, checksum string, expected int64) error {
	response, err := get(ctx, address, token)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(out, hash), io.LimitReader(response.Body, (1<<30)+1))
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if n > 1<<30 {
		return errors.New("download exceeds 1 GiB limit")
	}
	if expected > 0 && n != expected {
		return fmt.Errorf("download size mismatch: expected %d, got %d", expected, n)
	}
	if checksum != "" && hex.EncodeToString(hash.Sum(nil)) != strings.ToLower(checksum) {
		return errors.New("download SHA-256 mismatch")
	}
	return nil
}
func fetchSource(ctx context.Context, repository, ref, work string, log io.Writer) (string, string, error) {
	if !repositoryPattern.MatchString(repository) || strings.Contains(repository, "..") {
		return "", "", errors.New("repository must be a GitHub owner/name")
	}
	if !refPattern.MatchString(ref) || strings.HasPrefix(ref, "-") {
		return "", "", errors.New("invalid source ref")
	}
	token := os.Getenv("GITHUB_TOKEN")
	revision := ref
	if !revisionPattern.MatchString(ref) {
		var commit struct {
			SHA string `json:"sha"`
		}
		if err := readJSON(ctx, "https://api.github.com/repos/"+repository+"/commits/"+url.PathEscape(ref), token, &commit); err != nil {
			return "", "", err
		}
		revision = commit.SHA
	}
	if !revisionPattern.MatchString(revision) {
		return "", "", errors.New("GitHub did not return a valid commit revision")
	}
	fmt.Fprintf(log, "Downloading %s at %s\n", repository, revision)
	archive := filepath.Join(work, "source.zip")
	address := "https://codeload.github.com/" + repository + "/zip/" + revision
	if token != "" {
		address = "https://api.github.com/repos/" + repository + "/zipball/" + revision
	}
	if err := download(ctx, address, token, archive, "", 0); err != nil {
		return "", "", err
	}
	root := filepath.Join(work, "source")
	if err := extractZIP(ctx, archive, root); err != nil {
		return "", "", err
	}
	os.Remove(archive)
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", "", err
	}
	if len(entries) != 1 || !entries[0].IsDir() {
		return "", "", errors.New("source archive must contain one root directory")
	}
	return filepath.Join(root, entries[0].Name()), revision, nil
}

func archivePath(root, name string) (string, error) {
	if strings.ContainsAny(name, `\:`) || !filepath.IsLocal(filepath.FromSlash(name)) {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	return filepath.Join(root, filepath.FromSlash(name)), nil
}
func writeArchiveFile(ctx context.Context, name string, mode os.FileMode, reader io.Reader, size int64) error {
	if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		return err
	}
	permissions := os.FileMode(0600)
	if mode&0111 != 0 {
		permissions = 0700
	}
	out, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, permissions)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(out, io.LimitReader(contextReader{ctx, reader}, size+1))
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if n != size {
		return errors.New("archive file size mismatch")
	}
	return nil
}
func extractZIP(ctx context.Context, archive, root string) error {
	files, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer files.Close()
	var total uint64
	if len(files.File) > 100000 {
		return errors.New("archive has too many entries")
	}
	for _, file := range files.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		name, err := archivePath(root, file.Name)
		if err != nil {
			return err
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(name, 0700); err != nil {
				return err
			}
			continue
		}
		if !file.Mode().IsRegular() {
			return fmt.Errorf("unsupported archive entry %q", file.Name)
		}
		if file.UncompressedSize64 > 4<<30-total {
			return errors.New("archive exceeds 4 GiB extracted size limit")
		}
		total += file.UncompressedSize64
		reader, err := file.Open()
		if err != nil {
			return err
		}
		err = writeArchiveFile(ctx, name, file.Mode(), reader, int64(file.UncompressedSize64))
		reader.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
func extractTarGZ(ctx context.Context, archive, root string) error {
	file, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer file.Close()
	compressed, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer compressed.Close()
	reader := tar.NewReader(compressed)
	var total int64
	for count := 0; ; count++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if count > 100000 {
			return errors.New("archive has too many entries")
		}
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name, err := archivePath(root, header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(name, 0700); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || header.Size > 4<<30-total {
				return errors.New("archive exceeds 4 GiB extracted size limit")
			}
			total += header.Size
			if err := writeArchiveFile(ctx, name, os.FileMode(header.Mode), reader, header.Size); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported archive entry %q", header.Name)
		}
	}
}
func removeWork(root string) error {
	// Go's module cache contains read-only directories. Only touch our own tree.
	filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err == nil && entry.Type()&os.ModeSymlink == 0 {
			if entry.IsDir() {
				os.Chmod(path, 0700)
			} else {
				os.Chmod(path, 0600)
			}
		}
		return nil
	})
	return os.RemoveAll(root)
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
