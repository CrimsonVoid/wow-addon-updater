package main

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
)

func cacheDownload(url, fileCache string, buf *bytes.Buffer) error {
	// fileCache exists => read from disk, write to buf
	// fileCache missing => read from net, write to buf (& disk if fileCache provided)
	buf.Reset()

	var rd io.Reader
	if file, err := os.Open(fileCache); err == nil {
		// optimistically try reading from fileCache
		slog.Debug("reading from cache", "filename", fileCache)
		defer file.Close()
		rd = file
	} else {
		res, err := http.Get(url)
		if err != nil {
			return fmt.Errorf("error opening connection to %v: %w", url, err)
		}
		defer res.Body.Close()
		rd = res.Body

		// copy data to fileCache while reading if provided and not found on disk
		if fileCache != "" {
			file, err := os.Create(fileCache)
			if err != nil {
				return fmt.Errorf("could not create file %v: %w", file, err)
			}
			defer file.Close()

			rd = io.TeeReader(res.Body, file)
		}
	}

	_, err := io.Copy(buf, rd)
	if err != nil {
		return fmt.Errorf("error copying data: %w", err)
	}

	return nil
}
