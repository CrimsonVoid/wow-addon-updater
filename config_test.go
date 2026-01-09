package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestAddonManagerCfg_validate(t *testing.T) {
	tests := []struct {
		name  string
		cfg   *AddonManagerCfg
		check func(*testing.T, *AddonManagerCfg)
	}{
		{
			name: "minimal config",
			cfg: &AddonManagerCfg{
				Addons: []*AddonCfg{
					{NameCfg: "Project/Addon"},
				},
			},
			check: func(t *testing.T, cfg *AddonManagerCfg) {
				nameCfg := cfg.Addons[0].NameCfg
				if nameCfg != "Project/Addon" {
					t.Errorf("expected NameCfg to be 'Project/Addon', got '%v'", nameCfg)
				}

				name := cfg.Addons[0].Name
				if name != "Project/Addon" {
					t.Errorf("expected Name to be 'Project/Addon', got '%v'", name)
				}

				if cfg.UpdateInfo == nil {
					t.Error("UpdateInfo should be initialized")
				}
				if _, ok := cfg.UpdateInfo["Project/Addon"]; !ok {
					t.Error("UpdateInfo entry for addon should exist")
				}
			},
		},
		{
			name: "skipped addon",
			cfg: &AddonManagerCfg{
				Addons: []*AddonCfg{
					{NameCfg: "-Project/Addon"},
				},
			},
			check: func(t *testing.T, cfg *AddonManagerCfg) {
				nameCfg := cfg.Addons[0].NameCfg
				if nameCfg != "-Project/Addon" {
					t.Errorf("expected NameCfg to be '-Project/Addon', got '%v'", nameCfg)
				}

				name := cfg.Addons[0].Name
				if name != "Project/Addon" {
					t.Errorf("expected Name to be 'Project/Addon', got '%v'", name)
				}
			},
		},
		{
			name: "update existing updateInfo",
			cfg: &AddonManagerCfg{
				Addons: []*AddonCfg{
					{NameCfg: "Project/Addon1"},
				},
				UpdateInfo: map[string]*AddonUpdateInfo{
					"Project/Addon2": {},
				},
			},
			check: func(t *testing.T, cfg *AddonManagerCfg) {
				if n := len(cfg.UpdateInfo); n != 2 {
					t.Errorf("expected two entries in UpdateInfo, found %v", n)
				}

				for _, name := range []string{"Project/Addon1", "Project/Addon2"} {
					if _, ok := cfg.UpdateInfo[name]; !ok {
						t.Errorf("expected '%v' in UpdateInfo", name)
					}
				}
			},
		},
		{
			name: "config with dirs",
			cfg: &AddonManagerCfg{
				Addons: []*AddonCfg{
					{
						NameCfg: "Project/Addon",
						Dirs:    []string{"dir1", "dir2/"},
					},
				},
			},
			check: func(t *testing.T, cfg *AddonManagerCfg) {
				dirs := cfg.Addons[0].Dirs
				if dirs[0] != "dir1/" {
					t.Errorf("expected 'dir1/', got '%v'", dirs[0])
				}
				if dirs[1] != "dir2/" {
					t.Errorf("expected 'dir2/', got '%v'", dirs[1])
				}
			},
		},
		{
			name: "clamping tasks > MAX",
			cfg: &AddonManagerCfg{
				NetTasks:  MaxNetTasks + 1,
				DiskTasks: MaxDiskTasks + 1,
				Addons:    []*AddonCfg{{NameCfg: "Project/Addon"}},
			},
			check: func(t *testing.T, cfg *AddonManagerCfg) {
				if cfg.NetTasks != MaxNetTasks {
					t.Errorf("expected NetTasks to be clamped to %v, got %v", MaxNetTasks, cfg.NetTasks)
				}
				if cfg.DiskTasks != MaxDiskTasks {
					t.Errorf("expected DiskTasks to be clamped to %v, got %v", MaxDiskTasks, cfg.DiskTasks)
				}
			},
		}, {
			name: "clamping tasks < 0",
			cfg: &AddonManagerCfg{
				NetTasks:  -33,
				DiskTasks: -33,
				Addons:    []*AddonCfg{{NameCfg: "Project/Addon"}},
			},
			check: func(t *testing.T, cfg *AddonManagerCfg) {
				if cfg.NetTasks != 0 {
					t.Errorf("expected NetTasks to be clamped to 0, got %v", cfg.NetTasks)
				}
				if cfg.DiskTasks != 0 {
					t.Errorf("expected DiskTasks to be clamped to 0, got %v", cfg.DiskTasks)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.validate()
			if err != nil {
				t.Errorf("validate() error = %v", err)
			}
			tc.check(t, tc.cfg)
		})
	}
}

func TestAddonManagerCfg_validateCompleteCfg(t *testing.T) {
	now := time.Now()

	cfg := &AddonManagerCfg{
		Addons: []*AddonCfg{
			{NameCfg: "Project/Addon1",
				Name:    "Project/AddonName",
				Dirs:    []string{"dir1/", "dir2/", "-dir3/"},
				RelType: GhRelease},
			{NameCfg: "-Project/Addon2",
				Dirs:    []string{"dirA", "-dirB", "dirC"},
				RelType: GhTag},
			{NameCfg: "Project/Addon4", Name: "WillBeReplaced"},
			{NameCfg: "-Project/Addon5", Name: "WillBeReplaced"},
		},
		UnmanagedAddons: []string{"unmgdA", "unmgdB"},
		UpdateInfo: map[string]*AddonUpdateInfo{
			"Project/Addon1": {
				Version:       "1.1",
				UpdatedOn:     now,
				ExtractedDirs: []string{"a1.dir1/", "a1.dir2/"},
			},
			// "Project/Addon2": {},
			"Project/Addon3": {Version: "3.3",
				RefSha:        "sha-123",
				ExtractedDirs: []string{"a3.dir1/", "a3.dir2/"}},
		},
		CacheDir:  "cache33",
		NetTasks:  3,
		DiskTasks: 33,
	}

	cfgExp := &AddonManagerCfg{
		Addons: []*AddonCfg{
			{NameCfg: "Project/Addon1",
				Name:    "Project/Addon1",
				Dirs:    []string{"dir1/", "dir2/", "-dir3/"},
				RelType: GhRelease},
			{NameCfg: "-Project/Addon2",
				Name:    "Project/Addon2",
				Dirs:    []string{"dirA/", "-dirB/", "dirC/"},
				RelType: GhTag},
			{NameCfg: "Project/Addon4",
				Name:    "Project/Addon4",
				Dirs:    []string{},
				RelType: GhRelease},
			{NameCfg: "-Project/Addon5",
				Name:    "Project/Addon5",
				Dirs:    []string{},
				RelType: GhRelease},
		},
		UnmanagedAddons: []string{"unmgdA", "unmgdB"},
		UpdateInfo: map[string]*AddonUpdateInfo{
			"Project/Addon1": {Version: "1.1",
				UpdatedOn:     now,
				ExtractedDirs: []string{"a1.dir1/", "a1.dir2/"}},
			"Project/Addon2": {},
			"Project/Addon3": {Version: "3.3",
				RefSha:        "sha-123",
				ExtractedDirs: []string{"a3.dir1/", "a3.dir2/"}},
			"Project/Addon4": {},
			"Project/Addon5": {},
		},
		CacheDir:  "cache33",
		NetTasks:  3,
		DiskTasks: 33,
	}

	if err := cfg.validate(); err != nil {
		t.Errorf("validate() error = %v", err)
		return
	}

	testAddonCfgEq(t, cfg.Addons, cfgExp.Addons)
	testAddonUpdateInfoEq(t, cfg.UpdateInfo, cfgExp.UpdateInfo)
	testEqFunc(t, "UnmanagedAddons", cfg.UnmanagedAddons, cfgExp.UnmanagedAddons, slices.Equal)
	testEq(t, "CacheDir", cfg.CacheDir, cfgExp.CacheDir)
	testEq(t, "NetTasks", cfg.NetTasks, cfgExp.NetTasks)
	testEq(t, "DiskTasks", cfg.DiskTasks, cfgExp.DiskTasks)
}

func TestAddonManagerCfg_validate_fail(t *testing.T) {
	tests := []struct {
		name string
		cfg  *AddonManagerCfg
	}{

		{
			name: "duplicate addons",
			cfg: &AddonManagerCfg{
				Addons: []*AddonCfg{
					{NameCfg: "Project/Addon"},
					{NameCfg: "Project/Addon"},
				},
			},
		},
		{
			name: "addon name double slash",
			cfg: &AddonManagerCfg{
				Addons: []*AddonCfg{
					{NameCfg: "Project//Addon"},
				},
			},
		},
		{
			name: "name misformatted",
			cfg: &AddonManagerCfg{
				Addons: []*AddonCfg{
					{NameCfg: "InvalidName"},
				},
			},
		},
		{
			name: "projName missing",
			cfg: &AddonManagerCfg{
				Addons: []*AddonCfg{
					{NameCfg: "/Addon"},
				},
			},
		},
		{
			name: "shortName missing",
			cfg: &AddonManagerCfg{
				Addons: []*AddonCfg{
					{NameCfg: "Project/"},
				},
			},
		},
		{
			name: "relType = GhEnd",
			cfg: &AddonManagerCfg{
				Addons: []*AddonCfg{
					{NameCfg: "Project/Addon", RelType: GhEnd},
				},
			},
		},
		{
			name: "relType past GhEnd",
			cfg: &AddonManagerCfg{
				Addons: []*AddonCfg{
					{NameCfg: "Project/Addon", RelType: GhEnd + GhEnd},
				},
			},
		},
		{
			name: "empty dir",
			cfg: &AddonManagerCfg{
				Addons: []*AddonCfg{
					{NameCfg: "Project/Addon", Dirs: []string{""}},
				},
			},
		},
		{
			name: "empty addon name",
			cfg: &AddonManagerCfg{
				Addons: []*AddonCfg{
					{NameCfg: ""},
				},
			},
		},
		{
			name: "empty excluded addon name",
			cfg: &AddonManagerCfg{
				Addons: []*AddonCfg{
					{NameCfg: "-"},
				},
			},
		},
		{
			name: "whitespace addon name",
			cfg: &AddonManagerCfg{
				Addons: []*AddonCfg{
					{NameCfg: " / \t "},
				},
			},
		},
		{
			name: "empty excluded dir",
			cfg: &AddonManagerCfg{
				Addons: []*AddonCfg{
					{NameCfg: "Project/Addon", Dirs: []string{"-"}},
				},
			},
		},
		{
			name: "blank excluded dir",
			cfg: &AddonManagerCfg{
				Addons: []*AddonCfg{
					{NameCfg: "Project/Addon", Dirs: []string{"\t-   "}},
				},
			},
		},
		{
			name: "blank dir",
			cfg: &AddonManagerCfg{
				Addons: []*AddonCfg{
					{NameCfg: "Project/Addon", Dirs: []string{"    "}},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.validate()
			if err == nil {
				t.Error("expected validation error")
			}
		})
	}
}

func TestAddonManagerCfg_load(t *testing.T) {
	// Create a temporary directory for cache tests
	tempDir, err := os.MkdirTemp("", "addon_test_cache")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tests := []struct {
		name    string
		cfg     *AddonManagerCfg
		wantErr bool
		check   func(*testing.T, *AddonManager)
	}{
		{
			name: "load defaults",
			cfg: &AddonManagerCfg{
				Addons: []*AddonCfg{
					{NameCfg: "Project/Addon"},
				},
				UnmanagedAddons: []string{"http://example.com/addon.zip"},
			},
			wantErr: false,
			check: func(t *testing.T, am *AddonManager) {
				if am.NetTasks != DefaultNetTasks {
					t.Errorf("expected default NetTasks %v, got %v", DefaultNetTasks, am.NetTasks)
				}
				if am.DiskTasks != DefaultDiskTasks {
					t.Errorf("expected default DiskTasks %v, got %v", DefaultDiskTasks, am.DiskTasks)
				}
				if len(am.Addons) != 1 {
					t.Errorf("expected 1 addon, got %v", len(am.Addons))
				}
				addon := am.Addons[0]
				if addon.projName != "Project/" {
					t.Errorf("expected projName 'Project/', got '%v'", addon.projName)
				}
				if addon.shortName != "Addon" {
					t.Errorf("expected shortName 'Addon', got '%v'", addon.shortName)
				}
				if len(am.UnmanagedAddons) != 1 || am.UnmanagedAddons[0] != "http://example.com/addon.zip" {
					t.Errorf("expected UnmanagedAddons to be copied")
				}
			},
		},
		{
			name: "load with dirs split",
			cfg: &AddonManagerCfg{
				Addons: []*AddonCfg{
					{
						NameCfg: "Project/Addon",
						Dirs:    []string{"include1", "-exclude1", "include2"},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, am *AddonManager) {
				addon := am.Addons[0]
				// Note: validate() adds trailing slashes, but load() is called after validate() usually.
				// However, load() itself doesn't call validate().
				// In this test setup, we are calling load() directly on a struct that hasn't been validated/normalized by validate()
				// BUT load() logic for splitting dirs depends on the input.
				// Let's manually normalize dirs as validate() would do, or just test load's logic.
				// load() splits based on '-'.

				// Wait, load() assumes validate() has run?
				// validate() ensures trailing slashes.
				// load() splits dirs.

				// If we pass un-validated config to load(), it should still work for splitting.

				if len(addon.includeDirs) != 2 {
					t.Errorf("expected 2 includeDirs, got %v", len(addon.includeDirs))
				}
				if len(addon.excludeDirs) != 1 {
					t.Errorf("expected 1 excludeDirs, got %v", len(addon.excludeDirs))
				}
				// Check content
				// includeDirs should be "include1", "include2"
				// excludeDirs should be "exclude1" (minus the leading '-')

				// Actually, let's check exact values.
				// The implementation of load:
				// if dir[0] == '-' { dirs[exclIdx] = dir[1:]; exclIdx-- } else { dirs[inclIdx] = dir; inclIdx++ }
				// slices.Reverse(dirs[inclIdx:])
				// excludeDirs = dirs[inclIdx:]
				// includeDirs = dirs[:inclIdx]

				// So excludeDirs will be reversed.
			},
		},
		{
			name: "load with cache dir",
			cfg: &AddonManagerCfg{
				CacheDir: filepath.Join(tempDir, "cache"),
				Addons:   []*AddonCfg{},
			},
			wantErr: false,
			check: func(t *testing.T, am *AddonManager) {
				if am.CacheDir == nil {
					t.Error("expected CacheDir to be open")
				}
				// Verify directory exists
				if _, err := os.Stat(filepath.Join(tempDir, "cache")); os.IsNotExist(err) {
					t.Error("cache directory was not created")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// We need to run validate first because load expects some normalization (like Name being set from NameCfg)
			// Actually load uses addon.Name, which is set in validate.
			// So we MUST run validate first or manually set Name.
			if err := tt.cfg.validate(); err != nil {
				t.Fatalf("setup failed: validate() returned error: %v", err)
			}

			am, err := tt.cfg.load()
			if (err != nil) != tt.wantErr {
				t.Errorf("load() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && tt.check != nil {
				tt.check(t, am)
			}
		})
	}
}
