package main

import (
	"fmt"
)

func main() {
	// slog.SetLogLoggerLevel(slog.LevelDebug)
	const addonsCfg = "addons.json"

	defer func() {
		fmt.Println("\npress any key to exit...")
		fmt.Scanf("h")
	}()

	am, err := loadAddonCfg(addonsCfg)
	if err != nil {
		fmt.Println(tcRed("error loading addons config from "+addonsCfg), err)
		return
	}
	// am.debugPrint()

	am.updateAddons()

	if err = am.saveAddonCfg(addonsCfg); err != nil {
		fmt.Printf("error saving addon confing to %v: %v\n", addonsCfg, err)
	}
}
