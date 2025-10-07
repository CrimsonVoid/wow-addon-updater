package main

import "strings"

const (
	DefaultNetTasks  = 2
	DefaultDiskTasks = 32
)

type WowFlavor string

const (
	FlavorAll       = "all"
	FlavorModern    = "modern"
	FlavorClassicTw = "classictw" // classic "timewalking", latest classic era expac
	FlavorClassic   = "classic"
)

func (f WowFlavor) normalize() WowFlavor {
	switch strings.ToLower(string(f)) {
	case "modern", "retail", "tww":
		return FlavorModern
	case "cata", "mists", "mop", "classictw":
		return FlavorClassicTw
	case "classic":
		return FlavorClassic
	default:
		return f
	}
}
