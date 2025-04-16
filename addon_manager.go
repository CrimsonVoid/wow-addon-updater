package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"strings"
)

type AddonManager struct {
	Addons []*Addon
	// addons that are not managed by us, typically map of urls
	UnmanagedAddons map[string]string
	// map of addon name to update info
	UpdateInfo map[string]*AddonUpdateInfo
	// cache data on disk for dev, omit or set to "" to skip caching
	CacheDir  string `json:",omitempty"`
	cacheRoot *os.Root
}

func loadAddonCfg(filename string) (*AddonManager, error) {
	am := &AddonManager{
		Addons:          []*Addon{},
		UnmanagedAddons: map[string]string{},
		UpdateInfo:      map[string]*AddonUpdateInfo{},
	}

	// read and unmarshal json data
	if data, err := os.ReadFile(filename); err != nil {
		return nil, fmt.Errorf("error reading config %v: %w", filename, err)
	} else if err = json.Unmarshal(data, &am); err != nil {
		return nil, fmt.Errorf("error decoding json data: %w", err)
	}

	// validate and load each addon
	for _, addon := range am.Addons {
		if err := am.loadAddon(addon); err != nil {
			return nil, fmt.Errorf("error loading addon %v: %w", addon.Name, err)
		}
	}

	// create cache dir if provided
	if am.CacheDir != "" {
		if err := os.MkdirAll(am.CacheDir, 0755); err != nil {
			return nil, fmt.Errorf("could not create cache dir: %w", err)
		} else if am.cacheRoot, err = os.OpenRoot(am.CacheDir); err != nil {
			return nil, fmt.Errorf("could not open cache dir: %w", err)
		}
	}

	return am, nil
}

func (am *AddonManager) loadAddon(addon *Addon) error {
	if addon.RelType >= GhEnd {
		return fmt.Errorf("unknown release type for addon %v: %v", addon.Name, addon.RelType)
	}

	// update projName and shortname
	// addon.Name = "PROJECT/ADDON"; projName, shortName = "PROJECT/", "ADDON"
	idx := strings.LastIndexByte(addon.Name, '/')
	if idx == -1 {
		return fmt.Errorf("addon name not formatted correctly: expected PROJECT/ADDON, found %v", addon.Name)
	}
	addon.projName = addon.Name[:idx+1]
	addon.shortName = addon.Name[idx+1:]

	// set AddonUpdateInfo from addonManager, creating it if not found
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

	return nil
}

func (am *AddonManager) updateAddons() {
	// in-memory buffer for downloads
	buf := &bytes.Buffer{}

	for _, addon := range am.Addons {
		buf.Reset()
		addon.buf, addon.cacheDir = buf, am.cacheRoot
		if err := addon.update(); err != nil {
			addon.Logf("%v %v\n", tcRed("error updating addon"), err)
		}
		addon.buf, addon.cacheDir = nil, nil

		fmt.Println("")
	}

	for addon, url := range am.UnmanagedAddons {
		fmt.Printf("%v: %v\n", tcCyan(addon), url)
	}
}

func (am *AddonManager) saveAddonCfg(filename string) error {
	// delete addons in UpdateInfo that are not in Addons, most likely from previous updates
	addonSet := map[string]bool{}
	for _, addon := range am.Addons {
		addonSet[addon.Name] = true
	}
	maps.DeleteFunc(am.UpdateInfo, func(name string, _ *AddonUpdateInfo) bool {
		return !addonSet[name]
	})

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

func (am *AddonManager) debugPrint() {
	fmt.Println("CacheDir:", am.CacheDir)

	for _, addon := range am.Addons {
		fmt.Printf("Name: %v%v\n", tcDim(addon.projName), tcCyan(addon.shortName))
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
