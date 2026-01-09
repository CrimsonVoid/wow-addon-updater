package main

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"
)

type AddonManagerCfg struct {
	Addons []*AddonCfg
	// addons that are not managed by us, typically a list of urls
	UnmanagedAddons []string `json:",omitempty"`
	// map of addon name to update info
	UpdateInfo map[string]*AddonUpdateInfo

	// cache data on disk for dev, omit or set to "" to skip caching
	CacheDir string `json:",omitempty"`
	// number of threads to use for network and disk io tasks. (default: 2 and 32 respectively)
	// this is an advanved option, use with care
	NetTasks, DiskTasks int `json:",omitempty"`
}

type AddonCfg struct {
	// addon name from github, expected format Project/Addon, Name's with a leading '-' are skipped
	// Name loaded from config, may differ from AddonCfg.Name (if NameCfg is "-Project/Addon",
	// Name will be "Project/Addon")
	NameCfg string `json:"Name,omitempty"`
	// canonicalized name
	Name string `json:"-"`
	// top-level dirs to extract. empty list will extract everything except for excluded folders.
	// folders starting with '-' will be excluded, takes priority over included dirs
	Dirs []string `json:",omitempty"`
	// 0|GhRel = github release (default); 1|GhTag = tagged commit
	RelType GhAssetType `json:",omitempty"`
}

type AddonUpdateInfo struct {
	// addon version from release.json if found or filename
	Version string `json:",omitempty"`
	// when addon was last updated (exclusive w/ RefSha)
	UpdatedOn time.Time
	// sha hash of latest tagged reference (exclusive w/ UpdatedOn)
	RefSha string `json:",omitempty"`
	// list of folders managed by us, deleted before extracting update
	ExtractedDirs []string
}

func LoadAddonManagerCfg(filename string) (*AddonManager, error) {
	cfg, err := &AddonManagerCfg{}, error(nil)

	if data, err := os.ReadFile(filename); err != nil {
		return nil, fmt.Errorf("error reading config %v: %w", filename, err)
	} else if err = json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("error decoding json config: %w", err)
	}

	if err = cfg.validate(); err != nil {
		return nil, fmt.Errorf("error loading addon manager: %w", err)
	}

	return cfg.load()
}

// validate checks AddonManagerCfg invariants and fixes minor infractions. this should make sure certain
// preconditions are set or corrected so other code does not have to perform validity, sanity,
// index bounds, etc. checks
//
// we ensure each AddonManagerCfg.Addon:
//   - is listed only once
//   - Name is formatted as "ProjName/AddonName"
//   - addon names with a leading '-' are skipped as a convenience shorthand
//   - each dir spcified ends with a trailing slash ('/')
//   - has an entry in UpdateInfo, creating an empty AddonUpdateInfoCfg if missing
//   - RelType is a known GhAssetType value
//
// AddonManagerCfg also checks:
//   - Net and Disk tasks are clamped between 0 and MaxNetTasks (8) or MaxDiskTasks (4096)
//   - UpdateInfo is not nil
//
// for internal use we:
//   - should not modify AddonManagerCfg too much since it should somewhat match the users' config
//     as we read it when saving it later
func (cfg *AddonManagerCfg) validate() error {
	var err *addonCfgValidationErr = nil

	if cfg.UpdateInfo == nil {
		cfg.UpdateInfo = map[string]*AddonUpdateInfo{}
	}

	// more than 4k disk tasks is probably not a good idea, even on nvme drives
	cfg.DiskTasks = clamp(0, cfg.DiskTasks, MaxDiskTasks)
	cfg.NetTasks = clamp(0, cfg.NetTasks, MaxNetTasks)

	duplicateAddons := map[string]int{}

	for _, addon := range cfg.Addons {
		errs := []string{}

		// skip addons with a leading '-', ie "-Project/Addon" is skipped
		// save normalized name as addon.Name since we will want to use the original name when saving configs
		name := addon.NameCfg
		if len(name) > 0 && name[0] == '-' {
			name = name[1:]
		}
		addon.Name = name

		if _, found := duplicateAddons[name]; found {
			errs = append(errs, "found duplicate addon")
		}
		duplicateAddons[name]++

		// name should have two parts separated by a '/' ("Project/Addon")
		idx := strings.IndexRune(name, '/')
		if idx == 0 || idx == len(name)-1 || strings.Count(name, "/") != 1 {
			errs = append(errs, "addon name misformatted, expected Project/Addon")
		}
		if nm := strings.TrimSpace(name); nm == "" || nm == "/" {
			errs = append(errs, "addon name cannot be empty or whitespace, expected Project/Addon")
		}

		if addon.RelType >= GhEnd {
			errs = append(errs, fmt.Sprintf("unknown release type %v", addon.RelType))
		}

		// ensure dirs have a trailing '/'
		for i, dir := range addon.Dirs {
			if d := strings.TrimSpace(dir); len(d) == 0 || d == "-" {
				errs = append(errs, "found empty directory")
				break
			}

			if dir[len(dir)-1] != '/' {
				addon.Dirs[i] = dir + "/"
			}
		}

		// create UpdateInfo if not found
		if cfg.UpdateInfo[name] == nil {
			cfg.UpdateInfo[name] = &AddonUpdateInfo{}
		}

		if len(errs) != 0 {
			err = err.new()
			err.addonErr(name, duplicateAddons[name]-1, errs)
		}
	}

	if err != nil {
		return err
	}
	return nil
}

func (cfg *AddonManagerCfg) load() (*AddonManager, error) {
	var err error
	am := &AddonManager{
		Addons:          make([]*Addon, 0, len(cfg.Addons)),
		UnmanagedAddons: slices.Clone(cfg.UnmanagedAddons),

		CacheDir:  nil,
		NetTasks:  cfg.NetTasks,
		DiskTasks: cfg.DiskTasks,

		cfgVars: struct {
			diskTasks, netTasks int
			cacheDir            string
		}{cfg.DiskTasks, cfg.NetTasks, cfg.CacheDir},
	}

	if am.NetTasks == 0 {
		am.NetTasks = DefaultNetTasks
	}
	if am.DiskTasks == 0 {
		am.DiskTasks = DefaultDiskTasks
	}

	for _, addon := range cfg.Addons {
		dirs := make([]string, len(addon.Dirs))
		inclIdx, exclIdx := 0, len(dirs)-1
		for _, dir := range addon.Dirs {
			if dir[0] == '-' {
				dirs[exclIdx] = dir[1:]
				exclIdx--
			} else {
				dirs[inclIdx] = dir
				inclIdx++
			}
		}
		slices.Reverse(dirs[inclIdx:])

		idx := strings.IndexRune(addon.Name, '/')

		am.Addons = append(am.Addons, &Addon{
			AddonCfg:        addon,
			AddonUpdateInfo: cfg.UpdateInfo[addon.Name],
			skip:            addon.NameCfg[0] == '-',
			projName:        addon.Name[:idx+1],
			shortName:       addon.Name[idx+1:],
			excludeDirs:     dirs[inclIdx:],
			includeDirs:     dirs[:inclIdx],
		})
	}

	if cfg.CacheDir != "" {
		if err = os.MkdirAll(cfg.CacheDir, 0755); err != nil {
			return nil, fmt.Errorf("could not create cache dir: %w", err)
		}
		if am.CacheDir, err = os.OpenRoot(cfg.CacheDir); err != nil {
			return nil, fmt.Errorf("could not open cache dir: %w", err)
		}
	}

	return am, nil
}

type addonCfgValidationErr struct {
	addons []struct {
		name   string
		count  int
		errors []string
	}
}

func (e *addonCfgValidationErr) new() *addonCfgValidationErr {
	if e == nil {
		return &addonCfgValidationErr{}
	}

	return e
}

func (e *addonCfgValidationErr) addonErr(name string, count int, errors []string) {
	e.addons = append(e.addons, struct {
		name   string
		count  int
		errors []string
	}{name, count, errors})
}

func (e *addonCfgValidationErr) Error() string {
	var sb strings.Builder
	sb.WriteString("validation errors found:\n")

	for _, a := range e.addons {
		name := a.name
		if name == "" {
			name = "<empty>"
		}

		if a.count > 0 {
			name = fmt.Sprintf("%s (%d)", name, a.count)
		}

		fmt.Fprintf(&sb, "  %s:\n", name)
		for _, err := range a.errors {
			fmt.Fprintf(&sb, "    - %v\n", err)
		}
	}

	return sb.String()
}
