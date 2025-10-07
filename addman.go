package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type AddonManagerRt struct {
	Flavors         map[WowFlavor]*os.Root
	Addons          map[WowFlavor][]*AddonRt
	UnamangedAddons map[string]string

	NetTasks, DiskTasks int

	cfg *AddonManagerCfg
}

type AddonRt struct {
	Name    string
	Flavor  WowFlavor
	Skip    bool
	RelType GhRelType

	ExclDirs, InclDirs []string
	addonDir           *os.Root
	*AddonUpdateInfoCfg

	projNm, shortNm string
}

type AddonManagerCfg struct {
	Flavors         map[WowFlavor]string
	Addons          []*AddonCfg
	UnmanagedAddons map[string]string `json:",omitempty"`
	UpdateInfo      map[string]map[WowFlavor]*AddonUpdateInfoCfg
	NetTasks        int    `json:",omitempty"`
	DiskTasks       int    `json:",omitempty"`
	CacheDir        string `json:",omitempty"`
}

type AddonCfg struct {
	Name    string
	Dirs    map[WowFlavor][]string
	Skip    bool      `json:",omitempty"`
	RelType GhRelType `json:",omitempty"`

	// scratch vars
	projNm, shortNm string
}

type AddonUpdateInfoCfg struct {
	Version       string
	UpdatedOn     time.Time
	ExtractedDirs []string
}

func loadAddonManagerCfg(filename string) (*AddonManagerRt, error) {
	cfg := &AddonManagerCfg{
		UpdateInfo: map[string]map[WowFlavor]*AddonUpdateInfoCfg{},
	}
	var err error

	if data, err := os.ReadFile(filename); err != nil {
		return nil, err
	} else if err = json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	if err = cfg.validate(); err != nil {
		return nil, err
	}
	am, err := cfg.load()
	if err != nil {
		return nil, err
	}
	am.cfg = cfg

	return am, nil
}

func (cfg *AddonManagerCfg) validate() error {
	duplicateAddons := map[string]struct{}{}

	for _, addon := range cfg.Addons {
		if _, found := duplicateAddons[addon.Name]; found {
			return fmt.Errorf("found duplicate addon %v", addon.Name)
		}
		duplicateAddons[addon.Name] = struct{}{}

		// ensure dir names have a trailing '/'
		for _, dirs := range addon.Dirs {
			for i, dir := range dirs {
				if dir[len(dir)-1] != '/' {
					dir += "/"
					dirs[i] = dir
				}
			}
		}

		idx := strings.LastIndexByte(addon.Name, '/')
		if idx <= 0 || idx == len(addon.Name)-1 {
			return fmt.Errorf("addon name misformatted, expected PROJECT/ADDON: %v", addon.Name)
		}
		addon.projNm = addon.Name[:idx+1]
		addon.shortNm = addon.Name[idx+1:]
	}

	if cfg.NetTasks < 0 {
		cfg.NetTasks = 0
	}
	if cfg.DiskTasks < 0 {
		cfg.DiskTasks = 0
	}

	return nil
}

func (cfg *AddonManagerCfg) load() (*AddonManagerRt, error) {
	am := &AddonManagerRt{
		Flavors:         make(map[WowFlavor]*os.Root, len(cfg.Flavors)),
		Addons:          map[WowFlavor][]*AddonRt{},
		UnamangedAddons: maps.Clone(cfg.UnmanagedAddons),

		NetTasks:  cfg.NetTasks,
		DiskTasks: cfg.DiskTasks,
	}

	// create mapping of normalized wow flavors to paths (default to modern wow in current dir)
	flavors := cfg.Flavors
	if len(flavors) == 0 {
		flavors = map[WowFlavor]string{FlavorModern: "."}
	}
	for flavor, path := range flavors {
		flavor = flavor.normalize()

		path += "/Interface/Addons"
		if cfg.CacheDir != "" {
			path = cfg.CacheDir + "/" + string(flavor)
		}
		path = filepath.Clean(path)

		var err error
		if err = os.MkdirAll(path, 0755); err != nil {
			return nil, err
		}
		am.Flavors[flavor], err = os.OpenRoot(path)
		if err != nil {
			return nil, err
		}
	}

	// create instances of Addons for each configured flavor
	cfg.initAddons(am)

	// set default values for Net/Disk Tasks if unset
	if am.NetTasks == 0 {
		am.NetTasks = DefaultNetTasks
	}
	if am.DiskTasks == 0 {
		am.DiskTasks = DefaultDiskTasks
	}

	return am, nil
}

func (cfg *AddonManagerCfg) initAddons(am *AddonManagerRt) {
	addonFlavorsNormalized := func(addonCfg *AddonCfg) map[WowFlavor][]string {
		flavs := map[WowFlavor][]string{}
		for flavor, dirs := range addonCfg.Dirs {
			flavs[flavor.normalize()] = dirs
		}
		if len(flavs) == 0 {
			flavs[FlavorAll] = []string{}
		}
		return flavs
	}

	// create an Addon for each flavor specified; intersection of cfg.Flavors & addon.Flavors
	// flavors: [modern, mists], addon: [modern] => [modern]
	// flavors: [modern, mists], addon: [modern, classic] => [modern]
	// flavors: [modern, mists], addon: []|[all] => [modern, mists]
	// flavors: [modern, mists], addon: [classic] => nil
	// flavors: [modern, mists], addon: [all, modern] => [modern, mists]
	for _, addonCfg := range cfg.Addons {
		flavors := addonFlavorsNormalized(addonCfg)
		addonUpdateInfo := cfg.UpdateInfo[addonCfg.Name]
		allDirs, hasAll := flavors[FlavorAll]

		for flavor, addonDir := range am.Flavors {
			dirs, ok := flavors[flavor]
			if !ok && hasAll {
				dirs = []string{}
			} else if !ok {
				continue
			}

			addon := addonCfg.newAddon(flavor, dirs, allDirs, addonDir, addonUpdateInfo[flavor])
			am.Addons[flavor] = append(am.Addons[flavor], addon)
		}
	}
}

func (cfg *AddonCfg) newAddon(flavor WowFlavor, dirs, allDirs []string, addonDir *os.Root, ui *AddonUpdateInfoCfg) *AddonRt {
	if ui == nil {
		ui = &AddonUpdateInfoCfg{}
	}

	exclDirs, inclDirs := []string{}, []string{}
	splitDirs := func(dirs []string) {
		for _, dir := range dirs {
			if dir[0] == '-' {
				exclDirs = append(exclDirs, dir[1:])
			} else {
				inclDirs = append(inclDirs, dir)
			}
		}
	}
	splitDirs(dirs)
	splitDirs(allDirs)

	addon := &AddonRt{
		Name:    cfg.Name,
		Flavor:  flavor,
		Skip:    cfg.Skip,
		RelType: cfg.RelType,

		ExclDirs:           exclDirs,
		InclDirs:           inclDirs,
		addonDir:           addonDir,
		AddonUpdateInfoCfg: ui,

		projNm:  cfg.projNm,
		shortNm: cfg.shortNm,
	}

	return addon
}

func (am *AddonManagerCfg) print() {
	fmt.Printf("Flavors:\n")
	for flav, path := range am.Flavors {
		fmt.Printf("  %s: %s\n", flav, path)
	}
	fmt.Println()

	fmt.Printf("Addons:\n")
	for _, a := range am.Addons {
		fmt.Printf("  Name: %s\n", a.Name)
		fmt.Printf("  Dirs:\n")
		for flav, dirs := range a.Dirs {
			fmt.Printf("    %v: %v\n", flav, dirs)
		}
		fmt.Printf("  RelType: %v\n", a.RelType)
		fmt.Printf("  Skip: %v\n\n", a.Skip)
	}
	fmt.Println()

	fmt.Printf("UnmanagedAddons:\n")
	for name, url := range am.UnmanagedAddons {
		fmt.Printf("  %s: %s\n", name, url)
	}
	fmt.Println()

	fmt.Printf("UpdateInfo:\n")
	for addon, ui := range am.UpdateInfo {
		fmt.Printf("  %s:\n", addon)
		for key, info := range ui {
			fmt.Printf("    %s:\n", key)
			fmt.Printf("      Version: %s\n", info.Version)
			fmt.Printf("      UpdatedOn: %s\n", info.UpdatedOn)
			fmt.Printf("      ExtractedDirs: %v\n", info.ExtractedDirs)
		}
	}
	fmt.Println()

	fmt.Printf("NetTasks: %v\n", am.NetTasks)
	fmt.Printf("DiskTasks: %v\n", am.DiskTasks)
	fmt.Printf("CacheDir: %s\n", am.CacheDir)
}

func (am *AddonManagerRt) print() {
	fmt.Println("Flavors:")
	for flavor, dir := range am.Flavors {
		fmt.Printf("  %v: %v\n", flavor, dir.Name())
	}
	fmt.Println()

	for flavor, addons := range am.Addons {
		fmt.Printf("Addons[%v]:\n", flavor)
		for _, addon := range addons {
			fmt.Printf("  Name: %v\n", addon.Name)
			fmt.Printf("  Flavor: %v\n", addon.Flavor)
			fmt.Printf("  Skip: %v\n", addon.Skip)
			fmt.Printf("  RelType: %v\n", addon.RelType)
			fmt.Printf("  ExclDirs: %v\n", addon.ExclDirs)
			fmt.Printf("  InclDirs: %v\n", addon.InclDirs)
			fmt.Printf("  addonDir: %v\n", addon.addonDir.Name())
			fmt.Printf("  UpdateInfo:\n")
			fmt.Printf("    Version: %v\n", addon.AddonUpdateInfoCfg.Version)
			fmt.Printf("    UpdatedOn: %v\n", addon.AddonUpdateInfoCfg.UpdatedOn)
			fmt.Printf("    ExtractedDirs: %v\n\n", addon.AddonUpdateInfoCfg.ExtractedDirs)
		}
		fmt.Println()
	}
	fmt.Println()

	fmt.Println("Unmanaged Addons:")
	for name, link := range am.UnamangedAddons {
		fmt.Printf("  %v: %v\n", name, link)
	}
	fmt.Println()

	fmt.Printf("NetTasks: %v\n", am.NetTasks)
	fmt.Printf("DiskTasks: %v\n", am.DiskTasks)

	fmt.Println("Flavors:", slices.Collect(maps.Keys(am.Addons)))
}
