package main

const (
	DefaultNetTasks = 2
	MaxNetTasks     = 8

	DefaultDiskTasks = 32
	MaxDiskTasks     = 4096
)

type GhAssetType uint8

const (
	GhRelease GhAssetType = iota
	GhTag
	GhEnd // this should always be the last variant
)
