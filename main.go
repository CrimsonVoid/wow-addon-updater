package main

import (
	"fmt"
)

func main() {
	// slog.SetLogLoggerLevel(slog.LevelDebug)
	const addonsCfg = "addons.json"

	defer func() {
		fmt.Println("\npress any key to exit...")
		// fmt.Scanf("h")
	}()

	am, err := loadAddonCfg(addonsCfg)
	if err != nil {
		fmt.Println(tcRed("error loading addons config from "+addonsCfg), err)
		return
	}
	// am.debugPrint()
	fmt.Println("")

	for _, addon := range am.Addons {
		if err := addon.update(am.buf, am.CacheDir); err != nil {
			addon.Logf("%v %v\n", tcRed("error updating addon"), err)
		}
		fmt.Println("")
	}

	for addon, url := range am.UnmanagedAddons {
		fmt.Printf("%v: %v\n", tcBlue(addon), url)
	}
	fmt.Println("")

	err = am.saveAddonCfg(addonsCfg)
	if err != nil {
		fmt.Printf("error saving addon confing to %v: %v\n", addonsCfg, err)
	}
}
