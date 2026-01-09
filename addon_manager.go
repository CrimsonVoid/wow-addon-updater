package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

type AddonManager struct {
	Addons []*Addon
	// addons that are not managed by us, typically a list of urls
	UnmanagedAddons []string

	// number of threads to use for network and disk io tasks
	NetTasks, DiskTasks int
	// cache data on disk for dev, omit or set to "" to skip caching
	CacheDir *os.Root

	// vars from original config, used when saving when vars used at runtime might differ from values
	// specified in configs (ie. diskTasks set to 0 in config would load as DefaultDiskTasks, but we
	// should save them back as 0)
	cfgVars struct {
		diskTasks, netTasks int
		cacheDir            string
	}
}

func (am *AddonManager) UpdateAddons() {
	netTasks, netCancel := spawnTaskPool(am.NetTasks, am.NetTasks)
	defer netCancel()
	diskTasks, diskCancel := spawnTaskPool(am.DiskTasks, 12)
	defer diskCancel()
	updateTasks, updateRes, updateCancel := spawnTaskResPool[*addonUpdateStatus](am.NetTasks+2, len(am.Addons))
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
			addon.addonSharedState = &addonSharedState{buf, am.CacheDir, netTasks, diskTasks, logs}
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
		addonExecSum += status.execTime
	}
	execTime := time.Since(start)
	logTasksWg.Wait()

	fmt.Printf("[%v]\n", tcDim("Unmanaged Addons"))
	for _, addon := range am.UnmanagedAddons {
		// https://example.com/wow/addonA => url, name = "https://example.com/wow", "addonA"
		idx := strings.LastIndexByte(addon, '/')
		if idx == -1 {
			idx = len(addon)
		}
		url, name := addon[:idx], addon[idx+1:]

		fmt.Printf("%v/%v\n", url, tcCyan(name))
	}
	fmt.Println()

	fmt.Printf("updated addons in %v (total: %v)\n", execTime, addonExecSum)
}

func (am *AddonManager) SaveAddonCfg(filename string) error {
	cfg := AddonManagerCfg{
		Addons:          make([]*AddonCfg, 0, len(am.Addons)),
		UpdateInfo:      make(map[string]*AddonUpdateInfo, len(am.Addons)),
		UnmanagedAddons: am.UnmanagedAddons,
		CacheDir:        am.cfgVars.cacheDir,
		NetTasks:        am.cfgVars.netTasks,
		DiskTasks:       am.cfgVars.diskTasks,
	}

	for _, addon := range am.Addons {
		cfg.Addons = append(cfg.Addons, addon.AddonCfg)
		cfg.UpdateInfo[addon.Name] = addon.AddonUpdateInfo
	}

	data, err := json.MarshalIndent(cfg, "", "    ")
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
	fmt.Fprintln(buf, "AddonManager {")
	fmt.Fprintf(buf, "  NetTasks: %v, DiskTasks: %v\n", am.NetTasks, am.DiskTasks)
	if am.CacheDir != nil {
		fmt.Fprintf(buf, "  CacheDir: %v\n", am.CacheDir.Name())
	}

	fmt.Fprintf(buf, "  Addons (%v):\n", len(am.Addons))
	for _, addon := range am.Addons {
		skip := "\033[1m" + tcGreen("-")
		if addon.skip {
			skip = tcRed("-")
		}

		dirs := slices.Concat(addon.includeDirs, addon.excludeDirs)
		for i, dir := range dirs[len(addon.includeDirs):] {
			dirs[i] = "-" + dir
		}

		updateInfo := ""
		if !addon.UpdatedOn.IsZero() {
			updateInfo = tcDim(addon.UpdatedOn.Local().Format("Jan 2, 2006"))
		}
		if addon.RefSha != "" {
			updateInfo += " " + tcDim(addon.RefSha)
		}

		release := ""
		switch addon.RelType {
		case GhRelease:
			release = "GhRelease"
		case GhTag:
			release = "GhTag"
		case GhEnd:
			release = tcRed("GhEnd")
		default:
			release = tcRed(fmt.Sprint("unkown release: ", addon.RelType))
		}

		fmt.Fprintf(buf, "    %v %v%v", skip, tcDim(addon.projName), tcCyan(addon.shortName))
		fmt.Fprintf(buf, " (%v on %v %v)\n", tcGreen(addon.Version), updateInfo, release)
		fmt.Fprintf(buf, "        Dirs:      %v\n", dirs)
		fmt.Fprintf(buf, "        Extracted: %v\n", addon.ExtractedDirs)
		fmt.Fprintln(buf)
	}

	fmt.Fprintf(buf, "  UnmanagedAddons (%v):\n", len(am.UnmanagedAddons))
	for _, addon := range am.UnmanagedAddons {
		fmt.Fprintf(buf, "    - %v\n", addon)
	}

	buf.WriteString("}")

	return buf.String()
}
