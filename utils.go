package main

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

func (a *Addon) cacheDownload(url string, cacheFile string) error {
	// cacheFile exists on disk => read from disk, write to buf
	// cacheFile missing on disk => read from net, write to buf (& disk if cacheFile provided)
	a.buf.Reset()

	var rd io.Reader
	switch {
	case a.cacheDir != nil:
		// optimistically try reading from cache
		if file, err := a.cacheDir.Open(cacheFile); err == nil {
			slog.Debug("reading from cache", "filename", cacheFile)
			defer file.Close()
			rd = file
			break
		}

		fallthrough
	default:
		res, err := http.Get(url)
		if err != nil {
			return fmt.Errorf("error opening connection to %v: %w", url, err)
		}
		defer res.Body.Close()
		rd = res.Body

		// copy data to cacheFile while reading if provided and not found on disk
		if a.cacheDir != nil {
			file, err := a.cacheDir.Create(cacheFile)
			if err != nil {
				return fmt.Errorf("could not create file %v: %w", file, err)
			}
			defer file.Close()

			rd = io.TeeReader(res.Body, file)
		}
	}

	if _, err := io.Copy(a.buf, rd); err != nil {
		return fmt.Errorf("error copying data: %w", err)
	}

	return nil
}

// terminal colors & styles

func tcDim(s string) string {
	return "\033[2m" + s + "\033[0m"
}

func tcMagentaDim(s string) string {
	return "\033[1;2;35m" + s + "\033[0m"
}

func tcGreen(s string) string {
	return "\033[32m" + s + "\033[0m"
}

func tcBlue(s string) string {
	return "\033[1;36m" + s + "\033[0m"
}

func tcRed(s string) string {
	return "\033[1;31m" + s + "\033[0m"
}
