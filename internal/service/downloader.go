package service

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lixianmin/logo"
)

var downloadClient = &http.Client{
	Timeout: 30 * time.Minute,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return nil
	},
	Transport: &http.Transport{
		ResponseHeaderTimeout: 30 * time.Second,
	},
}

func DownloadModel(targetPath string, urls ...string) error {
	if _, err := os.Stat(targetPath); err == nil {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return err
	}

	for _, url := range urls {
		logo.Info("DownloadModel: downloading %s -> %s", url, targetPath)
		fmt.Fprintf(os.Stderr, "  Downloading model from %s\n", shortURL(url))
		if err := downloadFile(targetPath, url); err != nil {
			logo.Warn("DownloadModel: %s failed: %s", url, err)
			fmt.Fprintf(os.Stderr, "  Download failed: %s\n", err)
			continue
		}
		logo.Info("DownloadModel: success %s", targetPath)
		fmt.Fprintf(os.Stderr, "  Download complete: %s\n", filepath.Base(targetPath))
		return nil
	}

	fmt.Fprintf(os.Stderr, "  All download attempts failed for %s\n", filepath.Base(targetPath))
	return fmt.Errorf("all download attempts failed for %s", targetPath)
}

func shortURL(url string) string {
	parts := strings.Split(url, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}
	return url
}

func downloadFile(targetPath, url string) error {
	fmt.Fprintf(os.Stderr, "  Connecting...\n")
	resp, err := downloadClient.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	total := resp.ContentLength
	fmt.Fprintf(os.Stderr, "  Downloading %s\n", formatBytes(total))

	tmpPath := targetPath + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	defer f.Close()

	var written int64
	buf := make([]byte, 32*1024)
	start := time.Now()
	lastPrint := time.Time{}

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			wn, writeErr := f.Write(buf[:n])
			if writeErr != nil {
				os.Remove(tmpPath)
				return writeErr
			}
			written += int64(wn)
		}

		now := time.Now()
		if now.Sub(lastPrint) >= 500*time.Millisecond || readErr != nil {
			lastPrint = now
			printDownloadProgress(written, total, start)
		}

		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			os.Remove(tmpPath)
			return readErr
		}
	}

	fmt.Fprintf(os.Stderr, "\n")

	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}

	return os.Rename(tmpPath, targetPath)
}

func printDownloadProgress(written, total int64, start time.Time) {
	elapsed := time.Since(start).Seconds()
	if elapsed < 0.001 {
		elapsed = 0.001
	}
	speed := float64(written) / elapsed

	if total > 0 {
		pct := float64(written) / float64(total)
		remaining := time.Duration((float64(total-written) / speed) * float64(time.Second)).Truncate(time.Second)
		bar := progressBar(pct, 30)
		fmt.Fprintf(os.Stderr, "\r  %s %5.1f%% %s/%s %s/s ETA %s    ",
			bar, pct*100, formatBytes(written), formatBytes(total), formatSpeed(speed), remaining)
	} else {
		fmt.Fprintf(os.Stderr, "\r  %s  %s/s    ", formatBytes(written), formatSpeed(speed))
	}
}

func progressBar(pct float64, width int) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 1 {
		pct = 1
	}
	filled := int(pct * float64(width))
	if filled > width {
		filled = width
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	return "[" + bar + "]"
}

func formatBytes(b int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)
	switch {
	case b >= GB:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(GB))
	case b >= MB:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(MB))
	case b >= KB:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(KB))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func formatSpeed(bytesPerSec float64) string {
	const MB = 1024 * 1024
	if bytesPerSec >= MB {
		return fmt.Sprintf("%.1f MB", bytesPerSec/MB)
	}
	const KB = 1024
	if bytesPerSec >= KB {
		return fmt.Sprintf("%.0f KB", bytesPerSec/KB)
	}
	return fmt.Sprintf("%.0f B", bytesPerSec)
}
