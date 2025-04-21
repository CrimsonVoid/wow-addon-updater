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

func testEqPtr[T any](t *testing.T, name string, i, e *T) bool {
	if i != e {
		t.Errorf("%v mismatch: (input != expected) %p != %p", name, i, e)
		return false
	}
	return true
}

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
	nm := " (" + i.Name + ")"

	testEq(t, "Addon.Name"+nm, i.Name, e.Name)
	testEq(t, "Addon.RelType"+nm, i.RelType, e.RelType)
	testEq(t, "Addon.Skip"+nm, i.Skip, e.Skip)
	testEqFunc(t, "Addon.Dirs"+nm, i.Dirs, e.Dirs, slices.Equal)
	testEqFunc(t, "Addon.excludeDirs"+nm, i.excludeDirs, e.excludeDirs, slices.Equal)
	testEqFunc(t, "Addon.includeDirs"+nm, i.includeDirs, e.includeDirs, slices.Equal)
	testEq(t, "Addon.projName"+nm, i.projName, e.projName)
	testEq(t, "Addon.shortName"+nm, i.shortName, e.shortName)
	// AddonUpdateInfo
	testEq(t, "Addon.Version"+nm, i.Version, e.Version)
	testEqFunc(t, "Addon.UpdatedOn"+nm, i.UpdatedOn, e.UpdatedOn, time.Time.Equal)
	testEq(t, "Addon.RefSha"+nm, i.RefSha, e.RefSha)
	testEqFunc(t, "Addon.ExtractedDirs"+nm, i.ExtractedDirs, e.ExtractedDirs, slices.Equal)
}

func testDownloadAssetEq(t *testing.T, i, e *downloadAsset) {
	testEq(t, "Name", i.Name, e.Name)
	testEq(t, "Size", i.Size, e.Size)
	testEq(t, "DownloadUrl", i.DownloadUrl, e.DownloadUrl)
	testEq(t, "ContentType", i.ContentType, e.ContentType)
	testEqFunc(t, "UpdatedAt", i.UpdatedAt, e.UpdatedAt, time.Time.Equal)
	testEq(t, "RefSha", i.RefSha, e.RefSha)
	testEq(t, "Version", i.Version, e.Version)
	testEq(t, "RelType", i.RelType, e.RelType)
}
