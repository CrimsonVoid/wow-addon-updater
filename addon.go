package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Addon struct {
	*AddonCfg
	*AddonUpdateInfo

	// top-level dirs to allow or skip extracting.  exclusions take prio over includeDirs if the
	// same folder is listed in both
	excludeDirs, includeDirs []string
	// skip updating this addon
	skip bool
	// Name, projName, shortName = Project/Addon, Project/, Addon
	projName, shortName string

	// shared/externally managed state
	*addonSharedState
}

type addonSharedState struct {
	// internal buffer for io
	buf *bytes.Buffer
	// AddonManager.CacheDir
	cacheDir *os.Root
	// net and disk workers
	netTasks, diskTasks chan<- func()
	logs                chan<- string
}

type addonUpdateStatus struct {
	addon    *Addon
	err      error
	execTime time.Duration
}

func (a *Addon) update() *addonUpdateStatus {
	status := &addonUpdateStatus{addon: a}

	getUpdateInfo := func(t time.Time, ref string) string {
		if a.RelType == GhRelease {
			return tcDim(t.Local().Format("Jan 2, 2006"))
		}
		return tcDim(ref)
	}

	a.Logf("checking for update (%v on %v)\n", tcGreen(a.Version), getUpdateInfo(a.UpdatedOn, a.RefSha))
	asset, err := a.checkUpdate()
	if err != nil {
		status.err = a.Errorf("could not find update data for %v: %w", a.shortName, err)
		return status
	}

	updateInfo := getUpdateInfo(asset.UpdatedAt, asset.RefSha)
	if !a.hasUpdate(asset) {
		a.Logf("no update found     (%v on %v)\n", tcGreen(asset.Version), updateInfo)
		return status
	} else if a.skip {
		// check if an update is available even if skipping
		a.Logf("skipping update     (%v on %v)\n", tcGreen(asset.Version), updateInfo)
		return status
	}

	a.Logf("downloading update  (%v on %v) %v\n", tcGreen(asset.Version), updateInfo, asset.Name)
	if err = a.downloadZip(asset); err != nil {
		status.err = a.Errorf("unable to download update for %v: %w", a.shortName, err)
		return status
	}

	a.Logf("unzipping\n")
	if err = a.extractZip(); err != nil {
		status.err = a.Errorf("error extracting update for %v: %w", a.shortName, err)
		return status
	}
	a.Logf("extracted %v\n", tcMagentaDim(fmt.Sprint(a.ExtractedDirs)))

	a.Version = asset.Version
	a.UpdatedOn = asset.UpdatedAt
	a.RefSha = asset.RefSha

	return status
}

func (a *Addon) hasUpdate(asset *downloadAsset) bool {
	return (asset.RelType == GhRelease && a.UpdatedOn.Before(asset.UpdatedAt)) ||
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

	addonsDir := "./"
	if a.cacheDir != nil {
		addonsDir = a.cacheDir.Name() + "/addons/"
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

	unzipErr := &atomic.Bool{}
	wg := &sync.WaitGroup{}
	wg.Add(len(extractFiles))

	// extract zip files
	// todo: check zip is thread safe
	for _, zipFile := range extractFiles {
		if unzipErr.Load() {
			wg.Done()
			continue
		}

		unzipFile := func() bool {
			zipF, err := zipFile.Open()
			if err != nil {
				return false
			}
			defer zipF.Close()

			addonFilename := addonsDir + zipFile.Name
			file, err := os.OpenFile(addonFilename, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, zipFile.Mode())
			if err != nil {
				return false
			}
			defer file.Close()

			writer := bufio.NewWriter(file)
			if _, err := io.Copy(writer, zipF); err != nil {
				return false
			}
			if err := writer.Flush(); err != nil {
				return false
			}

			return true
		}
		a.diskTasks <- func() {
			defer wg.Done()
			if !unzipFile() {
				unzipErr.Store(true)
			}
		}
	}
	wg.Wait()

	if unzipErr.Load() {
		return fmt.Errorf("error unzipping archive")
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

func (a *Addon) downloadZip(asset *downloadAsset) error {
	cacheFilename := fmt.Sprintf("%v-%v", a.shortName, asset.Name)
	a.buf.Reset()
	a.buf.Grow(int(asset.Size))

	return a.cacheDownload(asset.DownloadUrl, cacheFilename)
}

type downloadAsset struct {
	Name        string
	Size        int64
	DownloadUrl string    `json:"browser_download_url"`
	ContentType string    `json:"content_type"`
	UpdatedAt   time.Time `json:"updated_at"`
	RefSha      string
	Version     string
	RelType     GhAssetType
}

func (a *Addon) checkUpdate() (*downloadAsset, error) {
	switch a.RelType {
	case GhRelease:
		return a.getTaggedRelease()
	case GhTag:
		return a.getTaggedRef()
	default:
		return nil, fmt.Errorf("unknown github release type %v", a.RelType)
	}
}

type ghTaggedRel struct {
	TagName string `json:"tag_name"`
	Assets  []*downloadAsset
}
type releaseInfo struct {
	Releases []release
}
type release struct {
	Version  string
	Filename string
	Metadata []*releaseMetadata
}
type releaseMetadata struct {
	Flavor    string
	Interface int
}

func (a *Addon) getTaggedRelease() (*downloadAsset, error) {
	const RelEndpoint = "https://api.github.com/repos/%v/releases/latest"
	releaseManifest := func(a *downloadAsset) bool { return a.ContentType == "application/json" && a.Name == "release.json" }

	cacheFilename := fmt.Sprintf("%v-rel.json", a.shortName)

	ghRelease, err := fetchJson[ghTaggedRel](a, fmt.Sprintf(RelEndpoint, a.Name), cacheFilename)
	if err != nil {
		return nil, fmt.Errorf("error fetching update info: %w", err)
	}

	addonReleases := &releaseInfo{}
	if idx := slices.IndexFunc(ghRelease.Assets, releaseManifest); idx != -1 {
		relAsset := ghRelease.Assets[idx]
		cacheRelManifest := fmt.Sprintf("%v-addonRel.json", a.shortName)

		addonReleases, err = fetchJson[releaseInfo](a, relAsset.DownloadUrl, cacheRelManifest)
		if err != nil {
			return nil, fmt.Errorf("error fetching release manifest: %w", err)
		}
	}

	return a.findTaggedRel(ghRelease, addonReleases)
}

func (a *Addon) findTaggedRel(ghRelease *ghTaggedRel, addonReleases *releaseInfo) (*downloadAsset, error) {
	// invariant: ghRelease and addonReleases will not be nil when called from getTaggedRelease
	classicFlavors := regexp.MustCompile(`classic|bc|wrath|cata`)
	isMainline := func(m *releaseMetadata) bool { return m.Flavor == "mainline" }

	isReleaseAsset := func(a *downloadAsset) bool {
		return a.ContentType == "application/zip" && !classicFlavors.MatchString(a.Name)
	}
	version := ghRelease.TagName

	for _, addonRelInfo := range addonReleases.Releases {
		if slices.ContainsFunc(addonRelInfo.Metadata, isMainline) {
			isReleaseAsset = func(a *downloadAsset) bool {
				return a.ContentType == "application/zip" && a.Name == addonRelInfo.Filename
			}
			version = addonRelInfo.Version

			break
		}
	}

	idx := slices.IndexFunc(ghRelease.Assets, isReleaseAsset)
	if idx == -1 {
		return nil, fmt.Errorf("no matching asset found from release manifest")
	}

	asset := ghRelease.Assets[idx]
	asset.RelType = GhRelease
	asset.Version = version

	return asset, nil
}

type ghTaggedRef struct {
	Ref    string
	Object struct {
		Sha string
	}
}

func (a *Addon) getTaggedRef() (*downloadAsset, error) {
	const TagEndpoint = "https://api.github.com/repos/%v/git/refs/tags"
	cacheFilename := fmt.Sprintf("%v-ref.json", a.shortName)

	ghRefs, err := fetchJson[[]ghTaggedRef](a, fmt.Sprintf(TagEndpoint, a.Name), cacheFilename)
	if err != nil {
		return nil, fmt.Errorf("error fetching tagged ref: %w", err)
	}

	return a.findTaggedRef(*ghRefs)
}

func (a *Addon) findTaggedRef(ghRefs []ghTaggedRef) (*downloadAsset, error) {
	if len(ghRefs) == 0 {
		return nil, fmt.Errorf("did not find valid ref for %v", a.Name)
	}

	// ref.Ref sample:     refs/tags/31
	// DownloadUrl sample: https://github.com/kesava-wow/kuispelllistconfig/archive/refs/tags/31.zip

	ref := ghRefs[len(ghRefs)-1]
	name := ref.Ref[strings.LastIndexByte(ref.Ref, '/')+1:] + ".zip"
	asset := &downloadAsset{
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
	args = append([]any{tcDim(a.projName), tcCyan(a.shortName)}, args...)
	msg := fmt.Sprintf("[%v%v] "+format, args...)
	a.logs <- msg
}

func (a *Addon) Errorf(format string, args ...any) error {
	err := fmt.Errorf(format, args...)
	a.Logf("%v %v\n", tcRed("error updating addon"), err)

	return err
}
