package main

import (
	"fmt"
)

func main() {
	const addonsCfg = "addons.json"

	defer func() {
		fmt.Println("\npress any key to exit...")
		fmt.Scanf("h")
	}()

	am, err := LoadAddonCfg(addonsCfg)
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
