package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	DefaultNetTasks  = 2
	DefaultDiskTasks = 32
)

type AddonManager struct {
	Addons []*Addon
	// addons that are not managed by us, typically map of urls
	UnmanagedAddons map[string]string
	// map of addon name to update info, only used when (de)serializing. most likely should use
	// Addon.AddonUpdateInfo instead
	UpdateInfo map[string]*AddonUpdateInfo
	// number of threads to use for network and disk io tasks. (default: 2 and 128 respectively)
	// this is an advanved option, use with care
	NetTasksCfg  int `json:"NetTasks,omitempty"`
	DiskTasksCfg int `json:"DiskTasks,omitempty"`
	// copy of NetTasks and DiskTasks, keeping the original values when saving config
	netTasks, diskTasks int
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
		if _, ok := am.UpdateInfo[addon.Name]; ok {
			return fmt.Errorf("duplicate addon found: %v", addon.Name)
		}
		if err := am.initializeAddon(addon, prevUpdateInfo[addon.Name]); err != nil {
			return fmt.Errorf("error loading addon %v: %w", addon.Name, err)
		}
		am.UpdateInfo[addon.Name] = addon.AddonUpdateInfo
	}

	am.netTasks, am.diskTasks = am.NetTasksCfg, am.DiskTasksCfg
	if am.NetTasksCfg <= 0 {
		am.NetTasksCfg, am.netTasks = 0, DefaultNetTasks
	}
	if am.DiskTasksCfg <= 0 {
		am.DiskTasksCfg, am.diskTasks = 0, DefaultDiskTasks
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
	netTasks, netCancel := spawnTaskPool(am.netTasks, am.netTasks)
	defer netCancel()
	diskTasks, diskCancel := spawnTaskPool(am.diskTasks, 12)
	defer diskCancel()
	updateTasks, updateRes, updateCancel := spawnTaskResPool[*addonUpdateStatus](am.netTasks*2, len(am.Addons))
	defer updateCancel()

	bufPool := sync.Pool{New: func() any { return &bytes.Buffer{} }}
	logsCh := make(chan chan string, len(am.Addons))

	start := time.Now()
	for _, addon := range am.Addons {
		logs := make(chan string, 8)
		logsCh <- logs

		updateTasks <- func() *addonUpdateStatus {
			defer close(logs)
			buf := bufPool.Get().(*bytes.Buffer)
			defer func() { buf.Reset(); bufPool.Put(buf) }()
			addon.addonSharedState = &addonSharedState{buf, am.cacheRoot, netTasks, diskTasks, logs}
			defer func() { addon.addonSharedState = nil }()

			start := time.Now()
			status := addon.update()
			status.execTime = time.Since(start)
			// addon.Logf("updated in %v\n", status.execTime)
			return status
		}
	}
	close(updateTasks)
	close(logsCh)

	logTasksWg := &sync.WaitGroup{}
	logTasksWg.Add(1)
	go func() {
		defer logTasksWg.Done()
		for logCh := range logsCh {
			for log := range logCh {
				fmt.Print(log)
			}
			fmt.Println()
		}
	}()

	addonExecSum := time.Duration(0)
	for status := range updateRes {
		am.UpdateInfo[status.addon.Name] = status.addon.AddonUpdateInfo
		addonExecSum += status.execTime
	}
	execTime := time.Since(start)
	logTasksWg.Wait()

	for addon, url := range am.UnmanagedAddons {
		fmt.Printf("%v: %v\n", tcCyan(addon), url)
	}
	fmt.Println()

	fmt.Printf("updated addons in %v (total: %v)\n", execTime, addonExecSum)
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
