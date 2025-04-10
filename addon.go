package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
)

type GhRelType int8

const (
	GhRel = iota
	GhTag
)

type Addon struct {
	// == public members

	// addon name from github, expected format PROJECT/ADDON
	Name string
	// top-level dirs to extract. empty list will extract everything except for excluded folders.
	// folders starting with '-' will be excluded, takes priority over included dirs
	Dirs []string `json:",omitempty"`
	// 0|GhRel = github release (default); 1|GhTag = tagged commit
	RelType GhRelType `json:",omitempty"`
	// skip updating this addon
	Skip bool `json:",omitempty"`

	// == private members

	// reference to AddonManager.UpdateInfo[Name]
	*AddonUpdateInfo `json:"-"`
	// top-level dirs to allow or skip extracting.  exclusions take prio over includeDirs if the
	// same folder is listed in both
	includeDirs, excludeDirs []string
	// first part of Name; Name = PROJECT/ADDON, projName = PROJECT
	projName string
	// last part of Name; Name = PROJECT/ADDON, shortName = ADDON. used mostly for disk caches
	shortName string
}

type AddonUpdateInfo struct {
	// when addon was last updated (exclusive w/ RefSha)
	UpdatedAt time.Time
	// sha hash of latest tagged reference (exclusive w/ UpdatedAt)
	RefSha string `json:",omitempty"`
	// list of folders managed by us, deleted before extracting update
	ExtractedDirs []string
}

func (a *Addon) update(buf *bytes.Buffer, cacheDir string) error {
	fmtTm := func(t time.Time) string {
		return t.Local().Format("Jan _2, 2006 15:04:05")
	}

	lastUpdateInfo := fmt.Sprintf("last update: %v", fmtTm(a.UpdatedAt))
	if a.RelType == GhTag {
		lastUpdateInfo = fmt.Sprintf("ref sha: %v", a.RefSha)
	}

	a.Logf("checking for update (%v)\n", lastUpdateInfo)
	asset, err := a.getDlAsset(buf, cacheDir)
	if err != nil {
		return fmt.Errorf("could not find update data for %v: %w", a.shortName, err)
	}

	if a.Skip {
		a.Logf("skipping\n")
		return nil
	}

	switch asset.RelType {
	case GhRel:
		if !asset.UpdatedAt.After(a.UpdatedAt) {
			a.Logf("no update found (asset update: %v)\n", fmtTm(asset.UpdatedAt))
			return nil
		}
	case GhTag:
		if a.RefSha == asset.RefSha {
			a.Logf("no update found (asset ref: %v)\n", asset.RefSha)
			return nil
		}
	default:
		return fmt.Errorf("unknown asset type for %v: found %v", a.shortName, asset.RelType)
	}

	a.Logf("downloading update %v (updated: %v)\n", asset.Name, fmtTm(asset.UpdatedAt))
	err = asset.downloadZip(a.shortName, buf, cacheDir)
	if err != nil {
		return fmt.Errorf("unable to download update for %v: %w", a.shortName, err)
	}

	a.Logf("extracting update\n")
	zipRd, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		return fmt.Errorf("addon update for %v not zip format: %w", a.shortName, err)
	}

	if err = a.extractZip(zipRd, cacheDir); err != nil {
		return fmt.Errorf("error extracting update for %v: %w", a.shortName, err)
	}
	a.Logf("extracted: %v\n", a.ExtractedDirs)

	a.UpdatedAt = asset.UpdatedAt
	a.RefSha = asset.RefSha

	return nil
}

func (a *Addon) extractZip(zipRd *zip.Reader, cacheDir string) error {
	// remove ExtractedDir from previous update
	// loop over zip files, creating all dirs first, save files to temp slice
	//   filter file ex/inclusions and update ExtractedDirs
	// extract files from temp slice

	addonsDir := "./"
	if cacheDir != "" {
		addonsDir = cacheDir + "/addons/"
	}

	// delete previously extracted dirs
	for _, dir := range a.ExtractedDirs {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("error removing previously installed addon dir %v: %w", dir, err)
		}
	}
	a.ExtractedDirs = a.ExtractedDirs[:0]

	// create all dirs before extracting files
	extractFiles := make([]*zip.File, 0, len(zipRd.File))
	topLevelDirs := map[string]bool{} // unique set of top level dirs for ExtractedDirs
	for _, file := range zipRd.File {
		if skipUnzip(file.Name, a) {
			continue
		}

		if file.Mode().IsDir() {
			parentDir := file.Name[:strings.IndexByte(file.Name, '/')]
			if _, ok := topLevelDirs[parentDir]; !ok {
				a.ExtractedDirs = append(a.ExtractedDirs, parentDir)
				topLevelDirs[parentDir] = true
			}

			subDir := addonsDir + file.Name
			if err := os.MkdirAll(addonsDir+file.Name, file.Mode()); err != nil {
				return fmt.Errorf("error creating dir %v: %w", subDir, err)
			}
		} else {
			extractFiles = append(extractFiles, file)
		}
	}

	// extract zip files
	for _, zipFile := range extractFiles {
		// use a func here to simplify defered file cleanup
		err := func() error {
			zipF, err := zipFile.Open()
			if err != nil {
				return fmt.Errorf("error opening file %v: %w", zipFile.Name, err)
			}
			defer zipF.Close()

			addonFilename := addonsDir + zipFile.Name
			file, err := os.OpenFile(addonFilename, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, zipFile.Mode())
			if err != nil {
				return fmt.Errorf("error opening file %v: %w", addonFilename, err)
			}
			defer file.Close()

			if _, err := io.Copy(file, zipF); err != nil {
				return fmt.Errorf("error extracting archive file %v to %v: %w", zipFile.Name, addonFilename, err)
			}

			return nil
		}()

		if err != nil {
			return err
		}
	}

	return nil
}

func skipUnzip(filename string, addon *Addon) bool {
	for _, exclude := range addon.excludeDirs {
		if strings.HasPrefix(filename, exclude) {
			return true
		}
	}

	for _, include := range addon.includeDirs {
		if strings.HasPrefix(filename, include) {
			return false
		}
	}

	// includeDirs empty => include all files
	// includeDirs nonempty => skip all files
	return len(addon.includeDirs) != 0
}

func (asset *DownloadAsset) downloadZip(addonShortName string, buf *bytes.Buffer, cacheDir string) error {
	cacheFilename := ""
	if cacheDir != "" {
		cacheFilename = fmt.Sprintf("%v/%v-%v", cacheDir, addonShortName, asset.Name)
	}
	buf.Reset()
	buf.Grow(int(asset.Size))

	return cacheDownload(asset.DownloadUrl, cacheFilename, buf)
}

type DownloadAsset struct {
	Name        string
	Size        int64
	DownloadUrl string    `json:"browser_download_url"`
	ContentType string    `json:"content_type"`
	UpdatedAt   time.Time `json:"updated_at"`
	RefSha      string
	RelType     GhRelType
}

func (a *Addon) getDlAsset(buf *bytes.Buffer, cacheDir string) (*DownloadAsset, error) {
	cacheFilename := ""

	switch a.RelType {
	case GhRel:
		if cacheDir != "" {
			cacheFilename = fmt.Sprintf("%v/%v-rel.json", cacheDir, a.shortName)
		}
		return a.getTaggedRelease(buf, cacheFilename)
	case GhTag:
		if cacheDir != "" {
			cacheFilename = fmt.Sprintf("%v/%v-ref.json", cacheDir, a.shortName)
		}
		return a.getTaggedRef(buf, cacheFilename)
	default:
		return nil, fmt.Errorf("unknown github release type %v", a.RelType)
	}
}

func (a *Addon) getTaggedRelease(buf *bytes.Buffer, cacheFilename string) (*DownloadAsset, error) {
	type GhTaggedRel struct {
		TagName string `json:"tag_name"`
		Assets  []*DownloadAsset
	}
	const RelEndpoint = "https://api.github.com/repos/%v/releases/latest"
	classicFlavors := regexp.MustCompile(`classic|bc|wrath|cata`)

	err := cacheDownload(fmt.Sprintf(RelEndpoint, a.Name), cacheFilename, buf)
	if err != nil {
		return nil, err
	}

	ghRelease := GhTaggedRel{}
	if err := json.Unmarshal(buf.Bytes(), &ghRelease); err != nil {
		return nil, fmt.Errorf("unmarshal error for tagged release: %w", err)
	}

	for _, asset := range ghRelease.Assets {
		if asset.ContentType != "application/zip" {
			continue
		}
		if classicFlavors.MatchString(asset.Name) {
			continue
		}
		asset.RelType = GhRel

		return asset, nil
	}

	return nil, fmt.Errorf("no valid asset found")
}

func (a *Addon) getTaggedRef(buf *bytes.Buffer, cacheFilename string) (*DownloadAsset, error) {
	type GhTaggedRef struct {
		Ref    string
		Object struct {
			Sha string
		}
	}
	const TagEndpoint = "https://api.github.com/repos/%v/git/refs/tags"

	err := cacheDownload(fmt.Sprintf(TagEndpoint, a.Name), cacheFilename, buf)
	if err != nil {
		return nil, err
	}

	ghRefs := []GhTaggedRef{}
	if err := json.Unmarshal(buf.Bytes(), &ghRefs); err != nil {
		return nil, fmt.Errorf("unmarshal error for tagged ref: %w", err)
	}
	if len(ghRefs) == 0 {
		return nil, fmt.Errorf("did not find valid ref for %v", a.Name)
	}

	// ref.Ref sample:     refs/tags/31
	// DownloadUrl sample: https://github.com/kesava-wow/kuispelllistconfig/archive/refs/tags/31.zip

	ref := ghRefs[len(ghRefs)-1]
	asset := &DownloadAsset{
		Name:        ref.Ref[strings.LastIndexByte(ref.Ref, '/')+1:] + ".zip",
		Size:        0,
		DownloadUrl: fmt.Sprintf("https://github.com/%v/archive/%v.zip", a.Name, ref.Ref),
		ContentType: "application/zip",
		UpdatedAt:   time.Time{},
		RefSha:      ref.Ref,
		RelType:     GhTag,
	}

	return asset, nil
}

func (a *Addon) Logf(format string, args ...any) {
	fmt.Printf("[\033[%vm%v/\033[0m", "2", a.projName)
	fmt.Printf("\033[%vm%v\033[0m] ", "1;36", a.shortName)
	fmt.Printf(format, args...)
}
