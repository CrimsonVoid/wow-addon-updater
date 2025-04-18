package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type AddonManager struct {
	Addons []*Addon
	// addons that are not managed by us, typically map of urls
	UnmanagedAddons map[string]string
	// map of addon name to update info, only used when (de)serializing. most likely should use
	// Addon.AddonUpdateInfo instead
	UpdateInfo map[string]*AddonUpdateInfo
	// cache data on disk for dev, omit or set to "" to skip caching
	CacheDir  string `json:",omitempty"`
	cacheRoot *os.Root
}

func newAddonManager() *AddonManager {
	return &AddonManager{
		Addons:          []*Addon{},
		UnmanagedAddons: map[string]string{},
		UpdateInfo:      map[string]*AddonUpdateInfo{},
	}
}

func LoadAddonCfg(filename string) (*AddonManager, error) {
	am := newAddonManager()

	// read and unmarshal json data
	if data, err := os.ReadFile(filename); err != nil {
		return nil, fmt.Errorf("error reading config %v: %w", filename, err)
	} else if err = json.Unmarshal(data, &am); err != nil {
		return nil, fmt.Errorf("error decoding json config: %w", err)
	}

	if err := am.initialize(); err != nil {
		return nil, fmt.Errorf("error loading addon manager: %w", err)
	}

	return am, nil
}

func (am *AddonManager) initialize() error {
	// rebuild updateInfo with only currently tracked addons
	prevUpdateInfo := am.UpdateInfo
	am.UpdateInfo = make(map[string]*AddonUpdateInfo, len(am.Addons))

	for _, addon := range am.Addons {
		if err := am.initializeAddon(addon, prevUpdateInfo[addon.Name]); err != nil {
			return fmt.Errorf("error loading addon %v: %w", addon.Name, err)
		}
		am.UpdateInfo[addon.Name] = addon.AddonUpdateInfo
	}

	// create cache dir if provided
	if am.CacheDir != "" {
		if err := os.MkdirAll(am.CacheDir, 0755); err != nil {
			return fmt.Errorf("could not create cache dir: %w", err)
		} else if am.cacheRoot, err = os.OpenRoot(am.CacheDir); err != nil {
			return fmt.Errorf("could not open cache dir: %w", err)
		}
	}

	return nil
}

func (am *AddonManager) initializeAddon(addon *Addon, lastUpdateInfo *AddonUpdateInfo) error {
	if addon.RelType >= GhEnd {
		return fmt.Errorf("unknown release type for addon %v: %v", addon.Name, addon.RelType)
	}

	// update projName and shortname
	// addon.Name = "PROJECT/ADDON"; projName, shortName = "PROJECT/", "ADDON"
	idx := strings.LastIndexByte(addon.Name, '/')
	if idx <= 0 || idx == len(addon.Name)-1 {
		return fmt.Errorf("addon name not formatted correctly: expected PROJECT/ADDON, found %v", addon.Name)
	}
	addon.projName = addon.Name[:idx+1]
	addon.shortName = addon.Name[idx+1:]

	// set AddonUpdateInfo, creating it if not found
	if lastUpdateInfo == nil {
		lastUpdateInfo = &AddonUpdateInfo{}
	}
	addon.AddonUpdateInfo = lastUpdateInfo

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

	return nil
}

func (am *AddonManager) UpdateAddons() {
	// range helper to cleanup pre/post conditions when looping over addons
	addons := func(yield func(*Addon) bool) {
		buf := &bytes.Buffer{}

		for _, addon := range am.Addons {
			buf.Reset()
			addon.buf, addon.cacheDir = buf, am.cacheRoot

			ok := yield(addon)
			addon.buf, addon.cacheDir = nil, nil
			am.UpdateInfo[addon.Name] = addon.AddonUpdateInfo

			if !ok {
				break
			}
		}
	}

	for addon := range addons {
		if err := addon.update(); err != nil {
			addon.Logf("%v %v\n", tcRed("error updating addon"), err)
		}
		fmt.Println()
	}

	for addon, url := range am.UnmanagedAddons {
		fmt.Printf("%v: %v\n", tcCyan(addon), url)
	}
}

func (am *AddonManager) SaveAddonCfg(filename string) error {
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

func (am *AddonManager) String() string {
	buf := &strings.Builder{}

	fmt.Fprintln(buf, "CacheDir:", am.CacheDir)

	for _, addon := range am.Addons {
		fmt.Fprintf(buf, "%v%v\n", tcDim(addon.projName), tcCyan(addon.shortName))
		fmt.Fprintln(buf, "  Dirs:           ", addon.Dirs)
		fmt.Fprintln(buf, "  RelType:        ", addon.RelType)
		fmt.Fprintln(buf, "  includeDirs:    ", addon.includeDirs)
		fmt.Fprintln(buf, "  excludeDirs:    ", addon.excludeDirs)
		fmt.Fprintln(buf, "  addonUpdateInfo:")
		fmt.Fprintln(buf, "    Version:      ", addon.AddonUpdateInfo.Version)
		fmt.Fprintln(buf, "    UpdatedOn:    ", addon.AddonUpdateInfo.UpdatedOn)
		fmt.Fprintln(buf, "    RefSha:       ", addon.AddonUpdateInfo.RefSha)
		fmt.Fprintln(buf, "    ExtractedDirs:", addon.AddonUpdateInfo.ExtractedDirs)
		fmt.Fprintln(buf, "")
	}

	for addon, url := range am.UnmanagedAddons {
		fmt.Fprintf(buf, "%v: %v\n", addon, url)
	}

	return buf.String()
}
