package main

import (
	"slices"
	"testing"
	"time"
)

var (
	// global now so all tests can use the same time
	now = time.Now()
)

func testEq[T comparable](t *testing.T, name string, i, e T) bool {
	return testEqFunc(t, name, i, e, func(i, e T) bool { return i == e })
}

func testEqFunc[T any](t *testing.T, name string, i, e T, eq func(T, T) bool) bool {
	if !eq(i, e) {
		t.Errorf("%v mismatch: (input != expected) %v != %v", name, i, e)
		return false
	}
	return true
}

func testAddonEq(t *testing.T, i, e *Addon) {
	testEq(t, "Addon.Name", i.Name, e.Name)
	testEq(t, "Addon.RelType", i.RelType, e.RelType)
	testEq(t, "Addon.Skip", i.Skip, e.Skip)
	testEqFunc(t, "Addon.Dirs", i.Dirs, e.Dirs, slices.Equal)
	testEqFunc(t, "Addon.excludeDirs", i.excludeDirs, e.excludeDirs, slices.Equal)
	testEqFunc(t, "Addon.includeDirs", i.includeDirs, e.includeDirs, slices.Equal)
	testEq(t, "Addon.projName", i.projName, e.projName)
	testEq(t, "Addon.shortName", i.shortName, e.shortName)
	// AddonUpdateInfo
	testEq(t, "Addon.Version", i.Version, e.Version)
	testEqFunc(t, "Addon.UpdatedOn", i.UpdatedOn, e.UpdatedOn, time.Time.Equal)
	testEq(t, "Addon.RefSha", i.RefSha, e.RefSha)
	testEqFunc(t, "Addon.ExtractedDirs", i.ExtractedDirs, e.ExtractedDirs, slices.Equal)
}
