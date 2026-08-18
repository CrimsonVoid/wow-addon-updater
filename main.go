package main

import (
	"fmt"
	"runtime"
)

const addonsCfg = "addons.json"

func main() {
	am, err := LoadAddonManagerCfg(addonsCfg)
	defer waitForExit(am)
	if err != nil {
		fmt.Println(tcRed("error loading addon config from "+addonsCfg), err)
		return
	}
	// fmt.Println(am)

	am.UpdateAddons()

	if err = am.SaveAddonCfg(addonsCfg); err != nil {
		fmt.Println(tcRed("error saving addon confing to "+addonsCfg), err)
	}
}

// keep console open so user can review updates
func waitForExit(am *AddonManager) {
	// dont wait in dev mode (cacheDir is usually only set during dev)
	devMode := am != nil && am.CacheDir != nil
	if devMode {
		return
	}

	if runtime.GOOS == "windows" {
		fmt.Print("\npress any key to exit...")
		fmt.Scanf("h")
	}
}
