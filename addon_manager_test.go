package main

import (
	"testing"
)

func TestAddonManager_initializeAddonManager(t *testing.T) {
	expectedUpdateInfo := func() map[string]*AddonUpdateInfo {
		return map[string]*AddonUpdateInfo{
			"proj/addon1": {
				Version:       "v1.0",
				UpdatedOn:     now,
				ExtractedDirs: []string{"dir1", "dir3/"},
			},
			"proj/addon2": {},
		}
	}
	tests := []struct {
		name     string
		input    *AddonManager
		expected *AddonManager
	}{
		{
			name:     "minimal addon manager",
			input:    &AddonManager{},
			expected: newAddonManager(),
		}, {
			name: "initialize addon manager with addons",
			input: &AddonManager{
				Addons: []*Addon{
					{
						Name:    "proj/addon1",
						RelType: GhRel,
						Dirs:    []string{"dir1", "-dir2", "dir3/"},
					},
					{
						Name:    "proj/addon2",
						RelType: GhRel,
						Skip:    true,
					},
				},
				UpdateInfo: map[string]*AddonUpdateInfo{
					"proj/addon1": expectedUpdateInfo()["proj/addon1"],
					"not/exists": {
						RefSha: "sha-123456",
					},
				},
				CacheDir: "",
			},
			expected: &AddonManager{
				Addons: []*Addon{
					{
						Name:            "proj/addon1",
						RelType:         GhRel,
						Dirs:            []string{"dir1/", "-dir2/", "dir3/"},
						excludeDirs:     []string{"dir2/"},
						includeDirs:     []string{"dir1/", "dir3/"},
						projName:        "proj/",
						shortName:       "addon1",
						AddonUpdateInfo: expectedUpdateInfo()["proj/addon1"],
					},
					{
						Name:            "proj/addon2",
						RelType:         GhRel,
						Skip:            true,
						projName:        "proj/",
						shortName:       "addon2",
						AddonUpdateInfo: expectedUpdateInfo()["proj/addon2"],
					},
				},
				UpdateInfo: expectedUpdateInfo(),
				CacheDir:   "",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			i, e := tc.input, tc.expected
			if err := i.initialize(); err != nil {
				t.Errorf("error while loading addon: %v", err)
				return
			}

			if testEq(t, "addons length", len(i.Addons), len(e.Addons)) {
				for idx := range i.Addons {
					testAddonEq(t, i.Addons[idx], e.Addons[idx])
				}
			}

			// check updateInfo matches number of addons, dont need to check each individual
			// updateInfo since it is checked above
			testEq(t, "updateInfo and addon count", len(i.UpdateInfo), len(e.Addons))
			testEq(t, "updateInfo length", len(i.UpdateInfo), len(e.UpdateInfo))
			// Addon.UpdateInfo should be a pointer to AddonManager.UpdateInfo
			for _, addon := range i.Addons {
				testEqPtr(t, "updateInfo ptr "+addon.Name, addon.AddonUpdateInfo, i.UpdateInfo[addon.Name])
			}

			testEq(t, "CacheDir", i.CacheDir, e.CacheDir)
		})
	}
}

func TestAddonManager_initializeAddonManager_fail(t *testing.T) {
	failedAddons := initializeAddonFailCases()

	for _, addon := range failedAddons {
		t.Run(addon.name, func(t *testing.T) {
			am := newAddonManager()
			am.Addons = []*Addon{addon.input}

			if err := am.initialize(); err == nil {
				t.Errorf("expected error while loading addonManager")
				return
			}
		})
	}
}

func TestAddonManager_initializeAddon(t *testing.T) {
	type input struct {
		addon      *Addon
		updateInfo *AddonUpdateInfo
	}
	tests := []struct {
		name     string
		input    input
		expected *Addon
	}{
		{
			name: "minimal addon",
			input: input{
				&Addon{Name: "proj/name"},
				nil,
			},
			expected: &Addon{
				Name:            "proj/name",
				projName:        "proj/",
				shortName:       "name",
				AddonUpdateInfo: &AddonUpdateInfo{},
			},
		}, {
			name: "load addon all options",
			input: input{
				&Addon{
					Name:    "proj/name",
					RelType: GhRel,
					Dirs:    []string{"dir1", "-dir2", "dir3/"},
				},
				&AddonUpdateInfo{},
			},
			expected: &Addon{
				Name:            "proj/name",
				projName:        "proj/",
				shortName:       "name",
				Dirs:            []string{"dir1/", "-dir2/", "dir3/"},
				excludeDirs:     []string{"dir2/"},
				includeDirs:     []string{"dir1/", "dir3/"},
				AddonUpdateInfo: &AddonUpdateInfo{},
			},
		}, {
			name: "no dirs",
			input: input{
				&Addon{
					Name: "proj/name",
				},
				nil,
			},
			expected: &Addon{
				Name:            "proj/name",
				projName:        "proj/",
				shortName:       "name",
				AddonUpdateInfo: &AddonUpdateInfo{},
			},
		}, {
			name: "include all dirs",
			input: input{
				&Addon{
					Name: "proj/name",
					Dirs: []string{"dir1", "dir2"},
				},
				nil,
			},
			expected: &Addon{
				Name:            "proj/name",
				projName:        "proj/",
				shortName:       "name",
				Dirs:            []string{"dir1/", "dir2/"},
				includeDirs:     []string{"dir1/", "dir2/"},
				AddonUpdateInfo: &AddonUpdateInfo{},
			},
		}, {
			name: "exclude all dirs",
			input: input{
				&Addon{
					Name: "proj/name",
					Dirs: []string{"-dir1", "-dir2"},
				},
				nil,
			},
			expected: &Addon{
				Name:            "proj/name",
				projName:        "proj/",
				shortName:       "name",
				Dirs:            []string{"-dir1/", "-dir2/"},
				excludeDirs:     []string{"dir1/", "dir2/"},
				AddonUpdateInfo: &AddonUpdateInfo{},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			i, e := tc.input, tc.expected
			am := newAddonManager()
			if i.updateInfo != nil {
				am.UpdateInfo[i.addon.Name] = i.updateInfo
			}

			if err := am.initializeAddon(i.addon, am.UpdateInfo[i.addon.Name]); err != nil {
				t.Errorf("error while loading addon: %v", err)
				return
			}

			testAddonEq(t, i.addon, e)
		})
	}
}

func TestAddonManager_initializeAddon_fail(t *testing.T) {
	tests := initializeAddonFailCases()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			am := newAddonManager()
			if err := am.initializeAddon(tc.input, am.UpdateInfo[tc.input.Name]); err == nil {
				t.Errorf("expected error while initializing addon")
				return
			}
		})
	}
}

// test data

func initializeAddonFailCases() []struct {
	name  string
	input *Addon
} {
	return []struct {
		name  string
		input *Addon
	}{
		{
			name: "relType = GhEnd",
			input: &Addon{
				Name:    "proj/name",
				RelType: GhEnd,
			},
		}, {
			name: "relType past GhEnd",
			input: &Addon{
				Name:    "proj/name",
				RelType: GhRelType(5),
			},
		}, {
			name: "name misformatted",
			input: &Addon{
				Name:    "name",
				RelType: GhRel,
			},
		}, {
			name: "projName missing",
			input: &Addon{
				Name:    "/name",
				RelType: GhRel,
			},
		}, {
			name: "shortName missing",
			input: &Addon{
				Name:    "name/",
				RelType: GhRel,
			},
		},
	}
}
