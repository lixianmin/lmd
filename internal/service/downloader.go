package service

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/lixianmin/logo"
)

func DownloadModel(targetPath string, urls ...string) error {
	if _, err := os.Stat(targetPath); err == nil {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return err
	}

	for _, url := range urls {
		logo.Info("DownloadModel: downloading %s -> %s", url, targetPath)
		if err := downloadFile(targetPath, url); err != nil {
			logo.Warn("DownloadModel: %s failed: %s", url, err)
			continue
		}
		logo.Info("DownloadModel: success %s", targetPath)
		return nil
	}

	return fmt.Errorf("all download attempts failed for %s", targetPath)
}

func downloadFile(targetPath, url string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	tmpPath := targetPath + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		os.Remove(tmpPath)
		return err
	}

	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}

	return os.Rename(tmpPath, targetPath)
}
