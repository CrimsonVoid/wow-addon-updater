package main

import (
	"maps"
	"slices"
	"testing"
	"time"
)

func testEqPtr[T any](t *testing.T, name string, i, e *T) bool {
	t.Helper()

	if i != e {
		t.Errorf("%v mismatch: (input != expected) %p != %p", name, i, e)
		return false
	}
	return true
}

func testEq[T comparable](t *testing.T, name string, i, e T) bool {
	t.Helper()
	return testEqFunc(t, name, i, e, func(i, e T) bool { return i == e })
}

func testEqFunc[T any](t *testing.T, name string, i, e T, eq func(T, T) bool) bool {
	t.Helper()

	if !eq(i, e) {
		t.Errorf("%v mismatch: (input != expected) %v != %v", name, i, e)
		return false
	}
	return true
}

func testAddonCfgEq(t *testing.T, i, e []*AddonCfg) {
	t.Helper()

	if !testEq(t, "addon len", len(i), len(e)) {
		return
	}

	for idx := range len(i) {
		i, e := i[idx], e[idx]
		nm := "(" + i.NameCfg + ") "

		testEq(t, nm+"NameCfg", i.NameCfg, e.NameCfg)
		testEq(t, nm+"Name", i.Name, e.Name)
		testEqFunc(t, nm+"Dirs", i.Dirs, e.Dirs, slices.Equal)
		testEq(t, nm+"RelType", i.RelType, e.RelType)
	}
}

func testAddonUpdateInfoEq(t *testing.T, i, e map[string]*AddonUpdateInfo) {
	t.Helper()

	if !testEq(t, "addon len", len(i), len(e)) {
		return
	}

	iKeys, eKeys := slices.Collect(maps.Keys(i)), slices.Collect(maps.Keys(e))
	slices.Sort(iKeys)
	slices.Sort(eKeys)
	if !testEqFunc(t, "updateInfo keys", iKeys, eKeys, slices.Equal) {
		return
	}

	for nm := range i {
		i, e := i[nm], e[nm]
		nm := " (" + nm + ")"

		testEq(t, nm+"Version", i.Version, e.Version)
		testEqFunc(t, nm+"UpdatedOn", i.UpdatedOn, e.UpdatedOn, time.Time.Equal)
		testEq(t, nm+"RefSha", i.RefSha, e.RefSha)
		testEqFunc(t, nm+"ExtractedDirs", i.ExtractedDirs, e.ExtractedDirs, slices.Equal)
	}
}

func testDownloadAssetEq(t *testing.T, i, e *downloadAsset) {
	t.Helper()

	testEq(t, "Name", i.Name, e.Name)
	testEq(t, "Size", i.Size, e.Size)
	testEq(t, "DownloadUrl", i.DownloadUrl, e.DownloadUrl)
	testEq(t, "ContentType", i.ContentType, e.ContentType)
	testEqFunc(t, "UpdatedAt", i.UpdatedAt, e.UpdatedAt, time.Time.Equal)
	testEq(t, "RefSha", i.RefSha, e.RefSha)
	testEq(t, "Version", i.Version, e.Version)
	testEq(t, "RelType", i.RelType, e.RelType)
}
