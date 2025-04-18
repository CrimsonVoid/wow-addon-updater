package main

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
)

func (a *Addon) cacheDownload(url string, fileNm string) (err error) {
	// cacheFile exists on disk => read from disk, write to buf
	// cacheFile missing on disk => read from net, write to buf (& disk if cacheFile provided)
	a.buf.Reset()
	wr := (io.Writer)(a.buf)

	if a.cacheDir != nil {
		// optimistically try reading from cache
		if file, err := a.cacheDir.Open(fileNm); err == nil {
			defer file.Close()
			if _, err := io.Copy(a.buf, bufio.NewReader(file)); err != nil {
				return fmt.Errorf("error reading data from cache: %w", err)
			}
			return nil
		}

		// cache file not found, create it and tee writes to a.buf & cache
		file, err := a.cacheDir.Create(fileNm)
		if err != nil {
			return fmt.Errorf("error creating cache file %v: %w", file, err)
		}
		defer file.Close()
		bufW := bufio.NewWriter(file)
		defer func() { err = bufW.Flush() }()

		wr = io.MultiWriter(a.buf, bufW)
	}

	res, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("error opening connection to %v: %w", url, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("error fetching %v: %v", url, res.Status)
	}

	// copy data to buf & cache
	if _, err := io.Copy(wr, res.Body); err != nil {
		return fmt.Errorf("error copying data: %w", err)
	}

	return
}

// terminal colors & styles
const tcReset = "\033[0m"

func tcDim(s string) string {
	return "\033[2m" + s + tcReset
}

func tcMagentaDim(s string) string {
	return "\033[1;2;35m" + s + tcReset
}

func tcGreen(s string) string {
	return "\033[32m" + s + tcReset
}

func tcCyan(s string) string {
	return "\033[1;36m" + s + tcReset
}

func tcRed(s string) string {
	return "\033[1;31m" + s + tcReset
}
