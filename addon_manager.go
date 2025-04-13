package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
)

type AddonManager struct {
	Addons          []*Addon
	UnmanagedAddons map[string]string
	UpdateInfo      map[string]*AddonUpdateInfo
	// cache data on disk for dev, omit or set to "" to skip caching
	CacheDir  string
	cacheRoot *os.Root
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
	}

	if err = json.Unmarshal(data, &am); err != nil {
		return nil, fmt.Errorf("error decoding json data: %w", err)
	}

	for _, addon := range am.Addons {
		if addon.RelType >= GhEnd {
			return nil, fmt.Errorf("unknown release type for addon %v: %v", addon.Name, addon.RelType)
		}

		// update shortname; addon.Name = "PROJECT/ADDON"; projName, shortName = "PROJECT", "ADDON"
		idx := strings.LastIndexByte(addon.Name, '/')
		if idx == -1 {
			return nil, fmt.Errorf("addon name not formatted correctly: expected PROJECT/ADDON, found %v", addon.Name)
		}
		addon.projName = addon.Name[:idx+1]
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

	// open cacheRoot
	if am.CacheDir != "" {
		if err = os.MkdirAll(am.CacheDir, 0755); err != nil {
			return nil, fmt.Errorf("could not create cache dir: %w", err)
		}

		if am.cacheRoot, err = os.OpenRoot(am.CacheDir); err != nil {
			return nil, fmt.Errorf("could not open cache dir: %w", err)
		}
	}

	return am, nil
}

func (am *AddonManager) updateAddons() {
	buf := &bytes.Buffer{}

	for _, addon := range am.Addons {
		buf.Reset()
		addon.buf, addon.cacheDir = buf, am.cacheRoot

		if err := addon.update(); err != nil {
			addon.Logf("%v %v\n", tcRed("error updating addon"), err)
		}
		fmt.Println("")
	}

	for addon, url := range am.UnmanagedAddons {
		fmt.Printf("%v: %v\n", tcBlue(addon), url)
	}
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

func (am *AddonManager) debugPrint() {
	fmt.Println("CacheDir:", am.CacheDir)

	for _, addon := range am.Addons {
		fmt.Printf("Name: %v%v\n", tcDim(addon.projName), tcBlue(addon.shortName))
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
