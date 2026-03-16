package utils

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func BuildJobDir(storageRoot, jobID string, now time.Time) (string, error) {
	dir := filepath.Join(
		storageRoot,
		fmt.Sprintf("%04d", now.Year()),
		fmt.Sprintf("%02d", now.Month()),
		fmt.Sprintf("%02d", now.Day()),
		jobID,
	)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func DownloadOrCreateFile(ctx context.Context, sourceURL, dstPath string, retryCount int, retryDelay time.Duration) error {
	if strings.HasPrefix(sourceURL, "mock://") {
		return writeMockFile(dstPath)
	}

	if sourceURL == "" {
		return errors.New("empty source url")
	}

	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return err
	}

	var lastErr error
	for i := 1; i <= retryCount; i++ {
		err := downloadFile(ctx, sourceURL, dstPath)
		if err == nil {
			return nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(retryDelay):
		}
	}
	return fmt.Errorf("download failed after retries: %w", lastErr)
}

func DownloadVideoFile(ctx context.Context, sourceURL, dstPath string, retryCount int, retryDelay time.Duration) error {
	if strings.HasPrefix(sourceURL, "mock://") {
		return writeMockMP4File(dstPath)
	}
	if strings.TrimSpace(sourceURL) == "" {
		return errors.New("empty video url")
	}
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return err
	}

	var lastErr error
	for i := 1; i <= retryCount; i++ {
		if err := downloadVideoValidated(ctx, sourceURL, dstPath); err == nil {
			return nil
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(retryDelay):
		}
	}
	return fmt.Errorf("video download failed after retries: %w", lastErr)
}

func downloadFile(ctx context.Context, sourceURL, dstPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("download status=%d", resp.StatusCode)
	}

	f, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}

func downloadVideoValidated(ctx context.Context, sourceURL, dstPath string) error {
	tmpPath := dstPath + ".part"
	_ = os.Remove(tmpPath)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "video/*,application/octet-stream,*/*")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("download status=%d", resp.StatusCode)
	}

	ct := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	if strings.Contains(ct, "text/html") || strings.Contains(ct, "application/json") {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("video url returned non-binary content-type=%s body=%s", ct, strings.TrimSpace(string(b)))
	}

	f, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	if err := validateMP4File(tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, dstPath)
}

func validateMP4File(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() < 2048 {
		return fmt.Errorf("invalid mp4: too small (%d bytes)", info.Size())
	}

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	head := make([]byte, 64)
	n, err := f.Read(head)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	head = head[:n]

	if len(head) < 12 {
		return errors.New("invalid mp4 header length")
	}
	// ISO BMFF: bytes 4..8 should typically be "ftyp".
	if !bytes.Equal(head[4:8], []byte("ftyp")) {
		return errors.New("invalid mp4 signature (ftyp not found)")
	}
	brand := string(head[8:12])
	allowed := map[string]struct{}{
		"isom": {}, "iso2": {}, "mp41": {}, "mp42": {}, "avc1": {}, "dash": {},
	}
	if _, ok := allowed[brand]; !ok {
		// Keep tolerant, but still warn through error to prevent corrupt uploads.
		return fmt.Errorf("unsupported mp4 major brand: %s", brand)
	}
	return nil
}

func writeMockFile(dstPath string) error {
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return err
	}

	f, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer f.Close()

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	buf := make([]byte, 4*1024)
	if _, err := r.Read(buf); err != nil {
		return err
	}
	_, err = f.Write(buf)
	return err
}

func writeMockMP4File(dstPath string) error {
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return err
	}
	f, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer f.Close()

	// Minimal ISO BMFF-like header for mock only.
	header := []byte{
		0x00, 0x00, 0x00, 0x18,
		'f', 't', 'y', 'p',
		'i', 's', 'o', 'm',
		0x00, 0x00, 0x00, 0x01,
		'i', 's', 'o', 'm', 'm', 'p', '4', '2',
	}
	if _, err := f.Write(header); err != nil {
		return err
	}
	padding := make([]byte, 4096)
	_, err = f.Write(padding)
	return err
}

func FindZipByJobID(storageRoot, jobID string) (string, error) {
	var found string

	err := filepath.Walk(storageRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(info.Name()), ".zip") && strings.Contains(path, string(filepath.Separator)+jobID+string(filepath.Separator)) {
			found = path
			return io.EOF
		}
		return nil
	})

	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("job %s zip not found", jobID)
	}
	return found, nil
}
