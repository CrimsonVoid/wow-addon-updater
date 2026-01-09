package main

import (
	"bufio"
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
)

func fetchJson[T any](a *Addon, url string, fileNm string) (*T, error) {
	var t *T

	if err := a.cacheDownload(url, fileNm); err != nil {
		return nil, fmt.Errorf("error downloading: %w", err)
	}
	if err := json.Unmarshal(a.buf.Bytes(), &t); err != nil {
		return nil, fmt.Errorf("error unmarshalling: %w", err)
	}

	return t, nil
}

func (a *Addon) cacheDownload(url string, fileNm string) (err error) {
	wg := &sync.WaitGroup{}
	wg.Add(1)
	a.netTasks <- func() {
		defer wg.Done()
		err = a.cacheDownloadTask(url, fileNm)
	}
	wg.Wait()

	return err
}

func (a *Addon) cacheDownloadTask(url string, fileNm string) (err error) {
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
		defer func() {
			// flush cache and report any error (don't overwrite err if it is already set)
			if err2 := bufW.Flush(); err == nil && err2 != nil {
				err = fmt.Errorf("error flushing cache file %v: %w", file, err2)
			}
		}()

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

func clamp[T cmp.Ordered](lowerBound, n, upperBound T) T {
	if n < lowerBound {
		return lowerBound
	}
	if n > upperBound {
		return upperBound
	}

	return n
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
