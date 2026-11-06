package main

import (
	"runtime/debug"
	"testing"
)

func TestFormatVersion(t *testing.T) {
	tests := []struct {
		name            string
		explicitVersion string
		info            *debug.BuildInfo
		want            string
	}{
		{
			name:            "explicit ldflags version",
			explicitVersion: "v0.4.0",
			info:            nil,
			want:            "goaugtype version v0.4.0 linux/amd64",
		},
		{
			name: "tagged release build",
			info: &debug.BuildInfo{
				Main: debug.Module{Version: "v0.4.0"},
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "8f6f4c61234567890"},
					{Key: "vcs.modified", Value: "false"},
				},
			},
			want: "goaugtype version v0.4.0 linux/amd64",
		},
		{
			name: "untagged commit with pseudo-version and vcs.revision",
			info: &debug.BuildInfo{
				Main: debug.Module{Version: "v0.4.1-0.20261106120000-8f6f4c612345"},
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "8f6f4c61234567890"},
					{Key: "vcs.modified", Value: "false"},
				},
			},
			want: "goaugtype version devel (8f6f4c6) linux/amd64",
		},
		{
			name: "devel build with vcs.revision",
			info: &debug.BuildInfo{
				Main: debug.Module{Version: "(devel)"},
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "423642576d04cf3f"},
				},
			},
			want: "goaugtype version devel (4236425) linux/amd64",
		},
		{
			name: "dirty working tree on tagged commit",
			info: &debug.BuildInfo{
				Main: debug.Module{Version: "v0.4.0+dirty"},
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "8f6f4c61234567890"},
					{Key: "vcs.modified", Value: "true"},
				},
			},
			want: "goaugtype version devel (8f6f4c6+dirty) linux/amd64",
		},
		{
			name: "no build info fallback",
			info: nil,
			want: "goaugtype version devel linux/amd64",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := formatVersion(tc.explicitVersion, tc.info, "linux", "amd64")
			if got != tc.want {
				t.Errorf("formatVersion() = %q, want %q", got, tc.want)
			}
		})
	}
}
