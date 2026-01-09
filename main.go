package main

import (
	"fmt"
)

func main() {
	var (
		am        *AddonManager
		err       error
		addonsCfg = "addons.json"
	)

	defer func() {
		fmt.Println("\npress any key to exit...")
		// cacheDir is usually only set during development, use it as a proxy for dev
		devMode := am != nil && am.CacheDir != nil
		if !devMode {
			fmt.Scanf("h")
		}
	}()

	am, err = LoadAddonManagerCfg(addonsCfg)
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
