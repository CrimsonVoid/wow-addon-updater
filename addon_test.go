package main

import (
	"testing"
)

func TestAddon_findTaggedRel(t *testing.T) {
	mkDlAsset := func(fileNm, ver string, relType GhAssetType) *downloadAsset {
		return &downloadAsset{
			Name:        fileNm,
			DownloadUrl: "https://example.com/" + fileNm,
			ContentType: "application/zip",
			Version:     ver,
			RelType:     relType,
		}
	}

	type input struct {
		ghRel   ghTaggedRel
		relInfo releaseInfo
	}
	tests := []struct {
		name     string
		input    *input
		expected *downloadAsset
	}{
		{
			name: "no release.json find any modern zip",
			input: &input{
				ghRel: ghTaggedRel{
					TagName: "v1.tag",
					Assets: []*downloadAsset{
						mkDlAsset("addon-1.0.0-classic.zip", "", 0),
						mkDlAsset("addon-1.0.0-cata.zip", "", 0),
						mkDlAsset("addon-1.0.0.zip", "", 0),
					},
				},
				relInfo: releaseInfo{},
			},
			expected: mkDlAsset("addon-1.0.0.zip", "v1.tag", GhRelease),
		}, {
			name: "release.json found",
			input: &input{
				ghRel: ghTaggedRel{
					TagName: "v1.tag",
					Assets: []*downloadAsset{
						mkDlAsset("aaa.zip", "", 0),
						mkDlAsset("bbb.zip", "", 0),
						mkDlAsset("ccc.zip", "", 0),
					},
				},
				relInfo: releaseInfo{[]release{
					{"v1.0.0", "aaa.zip", []*releaseMetadata{{"classic", 11501}}},
					{"v2.0.0", "bbb.zip", []*releaseMetadata{}},
					{"v3.0.0", "ccc.zip", []*releaseMetadata{{"cata", 40400}, {"mainline", 110002}}},
				}},
			},
			expected: mkDlAsset("ccc.zip", "v3.0.0", GhRelease),
		}, {
			name: "no mainline release in release.json, fallback to first non-classic zip",
			input: &input{
				ghRel: ghTaggedRel{
					TagName: "v1.tag",
					Assets: []*downloadAsset{
						mkDlAsset("aaa.zip", "", 0),
						mkDlAsset("bbb.zip", "", 0),
						mkDlAsset("ccc.zip", "", 0),
					},
				},
				relInfo: releaseInfo{[]release{
					{"v1.0.0", "aaa.zip", []*releaseMetadata{{"classic", 11501}}},
					{"v2.0.0", "bbb.zip", []*releaseMetadata{{"cata", 40400}}},
					{"v3.0.0", "ccc.zip", []*releaseMetadata{{"mainline_", 110002}}},
				}},
			},
			expected: mkDlAsset("aaa.zip", "v1.tag", GhRelease),
		}, {
			name: "no mainline release in release.json with classic zips",
			input: &input{
				ghRel: ghTaggedRel{
					TagName: "v1.tag",
					Assets: []*downloadAsset{
						mkDlAsset("addon-1.0.0-classic.zip", "", 0),
						mkDlAsset("addon-1.0.0-cata.zip", "", 0),
						mkDlAsset("addon-1.0.0.zip", "", 0),
					},
				},
				relInfo: releaseInfo{[]release{
					{"v1.0.0", "aaa.zip", []*releaseMetadata{{"classic", 11501}}},
					{"v2.0.0", "bbb.zip", []*releaseMetadata{{"cata", 40400}}},
					{"v3.0.0", "ccc.zip", []*releaseMetadata{{"mainline_", 110002}}},
				}},
			},
			expected: mkDlAsset("addon-1.0.0.zip", "v1.tag", GhRelease),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			addon := &Addon{}
			i, e := tc.input, tc.expected

			res, err := addon.findTaggedRel(&i.ghRel, &i.relInfo)
			if err != nil {
				t.Errorf("error finding tagged release: %v", err)
				return
			}
			testDownloadAssetEq(t, res, e)
		})
	}
}

func TestAddon_findTaggedRel_fail(t *testing.T) {
	mkDlAsset := func(fileNm, contentType string) *downloadAsset {
		return &downloadAsset{
			Name:        fileNm,
			ContentType: contentType,
		}
	}

	type input struct {
		ghRel   ghTaggedRel
		relInfo releaseInfo
	}
	tests := []struct {
		name  string
		input *input
	}{
		{
			name: "no zip ContentType",
			input: &input{
				ghRel: ghTaggedRel{
					TagName: "v1.tag",
					Assets: []*downloadAsset{
						mkDlAsset("addon.tar", "application/tar"),
					},
				},
				relInfo: releaseInfo{},
			},
		}, {
			name: "no modern asset",
			input: &input{
				ghRel: ghTaggedRel{
					TagName: "v1.tag",
					Assets: []*downloadAsset{
						mkDlAsset("addon-classic.zip", "application/zip"),
					},
				},
				relInfo: releaseInfo{},
			},
		}, {
			name: "asset in release.json not found",
			input: &input{
				ghRel: ghTaggedRel{
					TagName: "v1.tag",
					Assets: []*downloadAsset{
						mkDlAsset("addon.zip", "application/zip"),
					},
				},
				relInfo: releaseInfo{[]release{
					{"v1.0.0", "aaa.zip", []*releaseMetadata{{"mainline", 110002}}},
				}},
			},
		}, {
			name: "file in release.json not zip",
			input: &input{
				ghRel: ghTaggedRel{
					TagName: "v1.tag",
					Assets: []*downloadAsset{
						mkDlAsset("addon.zip", "application/tar"),
					},
				},
				relInfo: releaseInfo{[]release{
					{"v1.0.0", "addon.zip", []*releaseMetadata{{"mainline", 110002}}},
				}},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			addon, i := &Addon{}, tc.input

			if _, err := addon.findTaggedRel(&i.ghRel, &i.relInfo); err == nil {
				t.Errorf("expected error while finding tagged release")
				return
			}
		})
	}
}
