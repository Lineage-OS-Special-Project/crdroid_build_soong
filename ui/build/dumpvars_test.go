// Copyright 2026 The LOSP Project
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package build

import "testing"

func TestBanner(t *testing.T) {
	env := Environment{"TARGET_RELEASE=cp2a"}
	config := Config{&configImpl{environ: &env}}
	vars := map[string]string{
		"PLATFORM_VERSION":         "17",
		"BUILD_ID":                 "CP2A.260605.016",
		"TARGET_RELEASE":           "cp2a",
		"TARGET_PRODUCT":           "lineage_blazer",
		"TARGET_BUILD_VARIANT":     "userdebug",
		"TARGET_ARCH":              "arm64",
		"WITH_GMS":                 "true",
		"OUT_DIR":                  "out",
		"PRODUCT_SOONG_NAMESPACES": "internal.namespace.list",
		"PRODUCT_SOURCE_ROOT_DIRS": "large source root list",
	}

	want := "LOSP  |  Android 17  |  build CP2A.260605.016\n" +
		"Target  lineage_blazer  |  userdebug  |  arm64\n" +
		"Release  cp2a  |  GMS  |  output out"
	if got := Banner(config, vars); got != want {
		t.Errorf("Banner() = %q, want %q", got, want)
	}
}

func TestBannerOptionalValues(t *testing.T) {
	config := Config{&configImpl{environ: &Environment{}}}

	tests := []struct {
		name string
		vars map[string]string
		want string
	}{
		{
			name: "missing values",
			vars: map[string]string{},
			want: "LOSP",
		},
		{
			name: "custom output and false gms",
			vars: map[string]string{
				"PLATFORM_VERSION": "17",
				"OUT_DIR":          "/build/output/custom",
				"WITH_GMS":         "false",
			},
			want: "LOSP  |  Android 17\nRelease  Vanilla  |  output /build/output/custom",
		},
		{
			name: "unknown gms omitted",
			vars: map[string]string{"WITH_GMS": ""},
			want: "LOSP",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Banner(config, tt.vars); got != tt.want {
				t.Errorf("Banner() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseResolvedBool(t *testing.T) {
	tests := []struct {
		value string
		want  bool
		ok    bool
	}{
		{value: "true", want: true, ok: true},
		{value: "yes", want: true, ok: true},
		{value: "1", want: true, ok: true},
		{value: "false", want: false, ok: true},
		{value: "0", want: false, ok: true},
		{value: "", want: false, ok: false},
		{value: "unset", want: false, ok: false},
	}
	for _, tt := range tests {
		got, ok := parseResolvedBool(tt.value)
		if got != tt.want || ok != tt.ok {
			t.Errorf("parseResolvedBool(%q) = (%t, %t), want (%t, %t)", tt.value, got, ok, tt.want, tt.ok)
		}
	}
}
