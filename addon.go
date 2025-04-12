package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"
)

type GhRelType uint8

const (
	GhRel = iota
	GhTag
	GhEnd // this should always be the last variant
)

type Addon struct {
	// addon name from github, expected format PROJECT/ADDON
	Name string
	// top-level dirs to extract. empty list will extract everything except for excluded folders.
	// folders starting with '-' will be excluded, takes priority over included dirs
	Dirs []string `json:",omitempty"`
	// 0|GhRel = github release (default); 1|GhTag = tagged commit
	RelType GhRelType `json:",omitempty"`
	// skip updating this addon
	Skip bool `json:",omitempty"`

	// reference to AddonManager.UpdateInfo[Name]
	*AddonUpdateInfo `json:"-"`
	// top-level dirs to allow or skip extracting.  exclusions take prio over includeDirs if the
	// same folder is listed in both
	includeDirs, excludeDirs []string
	// Name, projName, shortName = PROJECT/ADDON, PROJECT/, ADDON
	projName, shortName string
	// internal buffer for io
	buf *bytes.Buffer
	// AddonManager.CacheDir
	cacheDir string
}

type AddonUpdateInfo struct {
	// addon version from release.json if found or filename
	Version string `json:",omitempty"`
	// when addon was last updated (exclusive w/ RefSha)
	UpdatedOn time.Time
	// sha hash of latest tagged reference (exclusive w/ UpdatedAt)
	RefSha string `json:",omitempty"`
	// list of folders managed by us, deleted before extracting update
	ExtractedDirs []string
}

func (a *Addon) update() error {
	getUpdateInfo := func(t time.Time, ref string) string {
		if a.RelType == GhRel {
			return tcDim(t.Local().Format("Jan 2, 2006"))
		}
		return tcDim(ref)
	}

	a.Logf("checking for update (last update: %v on %v)\n", tcGreen(a.Version), getUpdateInfo(a.UpdatedOn, a.RefSha))
	asset, err := a.checkUpdate()
	if err != nil {
		return fmt.Errorf("could not find update data for %v: %w", a.shortName, err)
	}

	updateInfo := getUpdateInfo(asset.UpdatedAt, asset.RefSha)
	if !a.hasUpdate(asset) {
		a.Logf("no update found (%v on %v)\n", tcGreen(asset.Version), updateInfo)
		return nil
	} else if a.Skip {
		a.Logf("skipping update (%v on %v)\n", tcGreen(asset.Version), updateInfo)
		return nil
	}

	a.Logf("downloading update %v (%v on %v)\n", asset.Name, tcGreen(asset.Version), updateInfo)
	if err = a.downloadZip(asset); err != nil {
		return fmt.Errorf("unable to download update for %v: %w", a.shortName, err)
	}

	a.Logf("unzipping\n")
	if err = a.extractZip(); err != nil {
		return fmt.Errorf("error extracting update for %v: %w", a.shortName, err)
	}
	a.Logf("extracted %v\n", tcMagentaDim(fmt.Sprint(a.ExtractedDirs)))

	a.Version = asset.Version
	a.UpdatedOn = asset.UpdatedAt
	a.RefSha = asset.RefSha

	return nil
}

func (a *Addon) hasUpdate(asset *DownloadAsset) bool {
	return (asset.RelType == GhRel && a.UpdatedOn.Before(asset.UpdatedAt)) ||
		(asset.RelType == GhTag && a.RefSha != asset.RefSha)
}

func (a *Addon) extractZip() error {
	// remove ExtractedDir from previous update
	// loop over zip files, creating all dirs first, save files to temp slice
	//   filter file ex/inclusions and update ExtractedDirs
	// extract files from temp slice
	zipRd, err := zip.NewReader(bytes.NewReader(a.buf.Bytes()), int64(a.buf.Len()))
	if err != nil {
		return fmt.Errorf("addon update for %v not zip format: %w", a.shortName, err)
	}
	addonsDir := a.mkCacheFile("addons/")

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
		if skipUnzip(a, file.Name) {
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

func skipUnzip(addon *Addon, filename string) bool {
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

func (a *Addon) downloadZip(asset *DownloadAsset) error {
	cacheFilename := a.mkCacheFile("%v-%v", a.shortName, asset.Name)
	a.buf.Reset()
	a.buf.Grow(int(asset.Size))

	return cacheDownload(asset.DownloadUrl, a.buf, cacheFilename)
}

type DownloadAsset struct {
	Name        string
	Size        int64
	DownloadUrl string    `json:"browser_download_url"`
	ContentType string    `json:"content_type"`
	UpdatedAt   time.Time `json:"updated_at"`
	RefSha      string
	Version     string
	RelType     GhRelType
}

func (a *Addon) checkUpdate() (*DownloadAsset, error) {
	switch a.RelType {
	case GhRel:
		return a.getTaggedRelease()
	case GhTag:
		return a.getTaggedRef()
	default:
		return nil, fmt.Errorf("unknown github release type %v", a.RelType)
	}
}

func (a *Addon) getTaggedRelease() (*DownloadAsset, error) {
	type ReleaseMetadata struct {
		Flavor    string
		Interface int
	}
	type ReleaseInfo struct {
		Releases []struct {
			Version  string
			Filename string
			Metadata []*ReleaseMetadata
		}
	}
	type GhTaggedRel struct {
		TagName string `json:"tag_name"`
		Assets  []*DownloadAsset
	}

	const RelEndpoint = "https://api.github.com/repos/%v/releases/latest"
	classicFlavors := regexp.MustCompile(`classic|bc|wrath|cata`)
	isMainline := func(m *ReleaseMetadata) bool { return m.Flavor == "mainline" }
	releaseManifest := func(a *DownloadAsset) bool { return a.Name == "release.json" && a.ContentType == "application/json" }
	cacheFilename := a.mkCacheFile("%v-rel.json", a.shortName)

	ghRelease := GhTaggedRel{}
	if err := cacheDownload(fmt.Sprintf(RelEndpoint, a.Name), a.buf, cacheFilename); err != nil {
		return nil, fmt.Errorf("error fetching update info: %w", err)
	} else if err := json.Unmarshal(a.buf.Bytes(), &ghRelease); err != nil {
		return nil, fmt.Errorf("unmarshal error for tagged release: %w", err)
	}

	// check for release.json in assets
	switch idx := slices.IndexFunc(ghRelease.Assets, releaseManifest); idx {
	case -1:
		// release.json not found, fallback to any zip in assets
		for _, asset := range ghRelease.Assets {
			if asset.ContentType != "application/zip" || classicFlavors.MatchString(asset.Name) {
				continue
			}
			asset.Version = ghRelease.TagName
			asset.RelType = GhRel

			return asset, nil
		}

		return nil, fmt.Errorf("no valid asset found")
	default:
		relAsset := ghRelease.Assets[idx]
		cacheRelManifest := a.mkCacheFile("%v-addonRel.json", a.shortName)

		addonReleases := ReleaseInfo{}
		if err := cacheDownload(relAsset.DownloadUrl, a.buf, cacheRelManifest); err != nil {
			return nil, fmt.Errorf("error fetching release manifest: %w", err)
		} else if err := json.Unmarshal(a.buf.Bytes(), &addonReleases); err != nil {
			return nil, fmt.Errorf("unmarshal error for release manifest: %w", err)
		}

		// find mainline addon releaseInfo
		// get asset from github assets where Filenames match (releaseInfo.Filename == asset.Name)
		for _, rel := range addonReleases.Releases {
			// todo: check interface version as well?
			if !slices.ContainsFunc(rel.Metadata, isMainline) {
				continue
			}

			isRelAsset := func(a *DownloadAsset) bool { return a.Name == rel.Filename }
			if idx := slices.IndexFunc(ghRelease.Assets, isRelAsset); idx != -1 {
				asset := ghRelease.Assets[idx]
				asset.RelType = GhRel
				asset.Version = rel.Version
				return asset, nil
			}

			return nil, fmt.Errorf("no matching asset found from release manifest")
		}

		return nil, fmt.Errorf("no valid release found")
	}
}

func (a *Addon) getTaggedRef() (*DownloadAsset, error) {
	type GhTaggedRef struct {
		Ref    string
		Object struct {
			Sha string
		}
	}
	const TagEndpoint = "https://api.github.com/repos/%v/git/refs/tags"
	cacheFilename := a.mkCacheFile("%v-ref.json", a.shortName)
	ghRefs := []GhTaggedRef{}

	if err := cacheDownload(fmt.Sprintf(TagEndpoint, a.Name), a.buf, cacheFilename); err != nil {
		return nil, err
	} else if err := json.Unmarshal(a.buf.Bytes(), &ghRefs); err != nil {
		return nil, fmt.Errorf("unmarshal error for tagged ref: %w", err)
	} else if len(ghRefs) == 0 {
		return nil, fmt.Errorf("did not find valid ref for %v", a.Name)
	}

	// ref.Ref sample:     refs/tags/31
	// DownloadUrl sample: https://github.com/kesava-wow/kuispelllistconfig/archive/refs/tags/31.zip

	ref := ghRefs[len(ghRefs)-1]
	name := ref.Ref[strings.LastIndexByte(ref.Ref, '/')+1:] + ".zip"
	asset := &DownloadAsset{
		Name:        name,
		Size:        0,
		DownloadUrl: fmt.Sprintf("https://github.com/%v/archive/%v.zip", a.Name, ref.Ref),
		ContentType: "application/zip",
		UpdatedAt:   time.Time{},
		RefSha:      ref.Ref,
		Version:     name,
		RelType:     GhTag,
	}

	return asset, nil
}

func (a *Addon) Logf(format string, args ...any) {
	fmt.Printf("[%v%v] ", tcDim(a.projName), tcBlue(a.shortName))
	fmt.Printf(format, args...)
}
