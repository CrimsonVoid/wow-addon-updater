package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"
)

type DownloadAsset struct {
	Name        string
	Size        int64
	DownloadUrl string    `json:"browser_download_url"`
	ContentType string    `json:"content_type"`
	UpdatedAt   time.Time `json:"updated_at"`
	RefSha      string
	RelType     GhRelType
}

type GhRelType uint8

const (
	GhAuto GhRelType = iota
	GhRel
	GhTag
)

type Addon struct {
	// == public members

	// addon name from github, expected format PROJECT/ADDON
	Name string
	// top-level dirs to extract. empty list will extract everything except for excluded folders.
	// folders starting with '-' will be excluded, takes priority over included dirs
	Dirs []string `json:",omitempty"`
	// 0|GhAuto = auto (default if omitted); 1|GhRel = github release; 2|GhTag = tagged commit ref
	RelType GhRelType
	// skip updating this addon
	Skip bool `json:",omitempty"`

	// == private members

	// reference to AddonManager.UpdateInfo[Name]
	*AddonUpdateInfo `json:"-"`
	// top-level dirs to allow or skip extracting.  exclusions take prio over includeDirs if the
	// same folder is listed in both
	includeDirs, excludeDirs []string
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

type AddonManager struct {
	Addons          []*Addon
	UnmanagedAddons map[string]string
	UpdateInfo      map[string]*AddonUpdateInfo
	// cache data on disk for dev, omit or set to "" to skip caching
	CacheDir string

	buf *bytes.Buffer
}

func (am *AddonManager) debugPrint() {
	fmt.Println("CacheDir:", am.CacheDir)

	for _, addon := range am.Addons {
		fmt.Println("Name:", addon.Name)
		fmt.Println("Dirs:", addon.Dirs)
		fmt.Println("RelType:", addon.RelType)
		fmt.Println("includeDirs:", addon.includeDirs)
		fmt.Println("excludeDirs:", addon.excludeDirs)
		fmt.Println("addonUpdateInfo:", addon.AddonUpdateInfo)
		fmt.Println("")
	}

	for addon, url := range am.UnmanagedAddons {
		fmt.Printf("%v: %v\n", addon, url)
	}
	fmt.Println("")
}

func main() {
	// slog.SetLogLoggerLevel(slog.LevelDebug)
	const addonsCfg = "addons.json"

	defer func() {
		fmt.Println("\npress any key to exit...")
		fmt.Scanf("h")
	}()

	am, err := loadAddonCfg(addonsCfg)
	if err != nil {
		fmt.Printf("error loading addons config from %v: %v\n", addonsCfg, err)
		return
	}
	// am.debugPrint()
	fmt.Println("")

	for _, addon := range am.Addons {
		if err := am.updateAddon(addon); err != nil {
			fmt.Printf("[%v] error updating addon: %v\n", addon.Name, err)
		}
		fmt.Println("")
	}

	for addon, url := range am.UnmanagedAddons {
		fmt.Printf("%v: %v\n", addon, url)
	}
	fmt.Println("")

	err = am.saveAddonCfg(addonsCfg)
	if err != nil {
		fmt.Printf("error saving addon confing to %v: %v\n", addonsCfg, err)
	}
}

func loadAddonCfg(filename string) (*AddonManager, error) {
	slog.Debug("loading addon config", "configFile", filename)
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("error reading config %v: %w", filename, err)
	}

	am := &AddonManager{
		Addons:          []*Addon{},
		UnmanagedAddons: map[string]string{},
		UpdateInfo:      map[string]*AddonUpdateInfo{},
		buf:             &bytes.Buffer{},
	}

	if err = json.Unmarshal(data, &am); err != nil {
		return nil, fmt.Errorf("error decoding json data: %w", err)
	}

	for _, addon := range am.Addons {
		// update shortname; addon.Name = "PROJECT/ADDON"; addon.shortName = "ADDON"
		idx := strings.LastIndexByte(addon.Name, '/')
		if idx == -1 {
			return nil, fmt.Errorf("addon name not formatted correctly: expected PROJECT/ADDON, found %v", addon.Name)
		}
		addon.shortName = addon.Name[idx+1:]

		// set addon.AddonUpdateInfo from addonManager.UpdateInfo
		if _, ok := am.UpdateInfo[addon.Name]; !ok {
			am.UpdateInfo[addon.Name] = &AddonUpdateInfo{}
		}
		addon.AddonUpdateInfo = am.UpdateInfo[addon.Name]

		// populate addon.{include,exclude}Dirs from Dirs
		// dirs starting with '-' are excluded
		for i, dir := range addon.Dirs {
			// ensure dir names have a trailing '/'
			if dir[len(dir)-1] != '/' {
				dir += "/"
				addon.Dirs[i] = dir
			}

			if dir[0] == '-' {
				addon.excludeDirs = append(addon.excludeDirs, dir[1:])
			} else {
				addon.includeDirs = append(addon.includeDirs, dir)
			}
		}
	}

	// create CacheDir
	if am.CacheDir != "" {
		if err := os.MkdirAll(am.CacheDir, 0755); err != nil {
			return nil, fmt.Errorf("could not create cache dir: %w", err)
		}
	}

	return am, nil
}

func (am *AddonManager) saveAddonCfg(filename string) error {
	// delete addons in UpdateInfo that are not in Addons
	addonsDeleted := []string{}
	for name := range am.UpdateInfo {
		if !slices.ContainsFunc(am.Addons, func(a *Addon) bool { return a.Name == name }) {
			delete(am.UpdateInfo, name)
			addonsDeleted = append(addonsDeleted, name)
		}
	}
	slog.Debug("removed untracked addons from update list", "addons removed", addonsDeleted)

	data, err := json.MarshalIndent(am, "", "    ")
	if err != nil {
		return fmt.Errorf("error marshalling addons: %w", err)
	}

	err = os.WriteFile(filename, data, 0644)
	if err != nil {
		return fmt.Errorf("error saving addons to file %v: %w", filename, err)
	}

	return nil
}

func (am *AddonManager) updateAddon(addon *Addon) error {
	fmtTm := func(t time.Time) string {
		return t.Local().Format("Jan _2, 2006 15:04:05")
	}

	lastUpdateInfo := fmt.Sprintf("last update: %v", fmtTm(addon.UpdatedAt))
	if addon.RelType == GhTag {
		lastUpdateInfo = fmt.Sprintf("ref sha: %v", addon.RefSha)
	} else if addon.RelType == GhAuto {
		lastUpdateInfo += fmt.Sprintf(", ref sha: %v", addon.RefSha)
	}

	fmt.Printf("[%v] checking for update (%v)\n", addon.Name, lastUpdateInfo)
	asset, err := am.getDlAsset(addon)
	if err != nil {
		return fmt.Errorf("could not find update data for %v: %w", addon.shortName, err)
	}

	if addon.Skip {
		fmt.Printf("[%v] skipping\n", addon.Name)
		return nil
	}

	switch asset.RelType {
	case GhRel:
		if !asset.UpdatedAt.After(addon.UpdatedAt) {
			fmt.Printf("[%v] no update found (asset update: %v)\n", addon.Name, fmtTm(asset.UpdatedAt))
			return nil
		}
	case GhTag:
		if addon.RefSha == asset.RefSha {
			fmt.Printf("[%v] no update found (asset ref: %v)\n", addon.Name, asset.RefSha)
			return nil
		}
	default:
		return fmt.Errorf("unknown asset type for %v: found %v", addon.shortName, asset.RelType)
	}

	fmt.Printf("[%v] downloading update %v (updated: %v)\n", addon.Name, asset.Name, fmtTm(asset.UpdatedAt))
	err = am.downloadZip(asset, addon.shortName)
	if err != nil {
		return fmt.Errorf("unable to download update for %v: %w", addon.shortName, err)
	}

	fmt.Printf("[%v] extracting update\n", addon.Name)
	zipRd, err := zip.NewReader(bytes.NewReader(am.buf.Bytes()), int64(am.buf.Len()))
	if err != nil {
		return fmt.Errorf("addon update for %v not zip format: %w", addon.shortName, err)
	}

	if err = am.extractZip(addon, zipRd); err != nil {
		return fmt.Errorf("error extracting update for %v: %w", addon.shortName, err)
	}
	fmt.Printf("[%v] extracted: %v\n", addon.Name, addon.ExtractedDirs)

	addon.UpdatedAt = asset.UpdatedAt
	addon.RefSha = asset.RefSha

	return nil
}

func (am *AddonManager) extractZip(addon *Addon, zipRd *zip.Reader) error {
	// remove ExtractedDir from previous update
	// loop over zip files, creating all dirs first, save files to temp slice
	//   filter file ex/inclusions and update ExtractedDirs
	// extract files from temp slice

	addonsDir := "./"
	if am.CacheDir != "" {
		addonsDir = am.CacheDir + "/addons/"
	}

	// delete previously extracted dirs
	for _, dir := range addon.ExtractedDirs {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("error removing previously installed addon dir %v: %w", dir, err)
		}
	}
	addon.ExtractedDirs = addon.ExtractedDirs[:0]

	// create all dirs before extracting files
	extractFiles := make([]*zip.File, 0, len(zipRd.File))
	topLevelDirs := map[string]bool{} // unique set of top level dirs for ExtractedDirs
	for _, file := range zipRd.File {
		if skipUnzip(file.Name, addon) {
			continue
		}

		if file.Mode().IsDir() {
			parentDir := file.Name[:strings.IndexByte(file.Name, '/')]
			if _, ok := topLevelDirs[parentDir]; !ok {
				addon.ExtractedDirs = append(addon.ExtractedDirs, parentDir)
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

func (am *AddonManager) downloadZip(asset *DownloadAsset, addonShortName string) error {
	cacheFilename := ""
	if am.CacheDir != "" {
		cacheFilename = fmt.Sprintf("%v/%v-%v", am.CacheDir, addonShortName, asset.Name)
	}
	am.buf.Reset()
	am.buf.Grow(int(asset.Size))

	return cacheDownload(asset.DownloadUrl, cacheFilename, am.buf)
}

var errNoTaggedRel = fmt.Errorf("no tagged release found")

func (am *AddonManager) getDlAsset(addon *Addon) (*DownloadAsset, error) {
	cacheFilename := ""

	if addon.RelType == GhAuto || addon.RelType == GhRel {
		if am.CacheDir != "" {
			cacheFilename = fmt.Sprintf("%v/%v-rel.json", am.CacheDir, addon.shortName)
		}

		asset, err := am.getTaggedRelease(addon, cacheFilename)
		if err == nil {
			return asset, nil
		} else if !(addon.RelType == GhAuto && err == errNoTaggedRel) {
			return nil, fmt.Errorf("no valid asset found for %v: %w", addon.Name, err)
		}
	}

	if am.CacheDir != "" {
		cacheFilename = fmt.Sprintf("%v/%v-ref.json", am.CacheDir, addon.shortName)
	}
	asset, err := am.getTaggedRef(addon, cacheFilename)

	return asset, err
}

func (am *AddonManager) getTaggedRelease(addon *Addon, cacheFilename string) (*DownloadAsset, error) {
	type GhTaggedRel struct {
		TagName string `json:"tag_name"`
		Assets  []*DownloadAsset
		Status  string
	}
	const RelEndpoint = "https://api.github.com/repos/%v/releases/latest"

	err := cacheDownload(fmt.Sprintf(RelEndpoint, addon.Name), cacheFilename, am.buf)
	if err != nil {
		return nil, err
	}

	ghRelease := GhTaggedRel{}
	if err := json.Unmarshal(am.buf.Bytes(), &ghRelease); err != nil {
		return nil, fmt.Errorf("unmarshal error for tagged release: %w", err)
	}
	if ghRelease.Status == "404" {
		return nil, errNoTaggedRel
	}

	for _, asset := range ghRelease.Assets {
		if asset.ContentType != "application/zip" {
			continue
		}
		if strings.Contains(asset.Name, "cata") || strings.Contains(asset.Name, "classic") {
			continue
		}
		asset.RelType = GhRel

		return asset, nil
	}

	return nil, fmt.Errorf("no valid asset found")
}

func (am *AddonManager) getTaggedRef(addon *Addon, cacheFilename string) (*DownloadAsset, error) {
	type GhTaggedRef struct {
		Ref    string
		Object struct {
			Sha string
		}
	}
	const TagEndpoint = "https://api.github.com/repos/%v/git/refs/tags"

	err := cacheDownload(fmt.Sprintf(TagEndpoint, addon.Name), cacheFilename, am.buf)
	if err != nil {
		return nil, err
	}

	ghRefs := []GhTaggedRef{}
	if err := json.Unmarshal(am.buf.Bytes(), &ghRefs); err != nil {
		return nil, fmt.Errorf("unmarshal error for tagged ref: %w", err)
	}
	if len(ghRefs) == 0 {
		return nil, fmt.Errorf("did not find valid ref for %v", addon.Name)
	}

	// ref.Ref sample:     refs/tags/31
	// DownloadUrl sample: https://github.com/kesava-wow/kuispelllistconfig/archive/refs/tags/31.zip

	ref := ghRefs[len(ghRefs)-1]
	asset := &DownloadAsset{
		Name:        ref.Ref[strings.LastIndexByte(ref.Ref, '/')+1:] + ".zip",
		Size:        0,
		DownloadUrl: fmt.Sprintf("https://github.com/%v/archive/%v.zip", addon.Name, ref.Ref),
		ContentType: "application/zip",
		UpdatedAt:   time.Time{},
		RefSha:      ref.Ref,
		RelType:     GhTag,
	}

	return asset, nil
}

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
