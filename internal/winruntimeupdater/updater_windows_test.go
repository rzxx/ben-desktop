//go:build windows

package winruntimeupdater

import "testing"

func TestCompareRuntimeVersion(t *testing.T) {
	tests := []struct {
		name   string
		remote string
		local  string
		want   int
	}{
		{name: "missing local updates", remote: "1", local: "", want: 1},
		{name: "same", remote: "1.2.3", local: "1.2.3", want: 0},
		{name: "newer patch", remote: "1.2.4", local: "1.2.3", want: 1},
		{name: "older major", remote: "1.9.0", local: "2.0.0", want: -1},
		{name: "monotonic integer", remote: "11", local: "2", want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compareRuntimeVersion(tt.remote, tt.local)
			if got != tt.want {
				t.Fatalf("compareRuntimeVersion(%q, %q) = %d, want %d", tt.remote, tt.local, got, tt.want)
			}
		})
	}
}

func TestCleanManifestPath(t *testing.T) {
	valid, err := cleanManifestPath("runtime/ffmpeg/bin/ffmpeg.exe")
	if err != nil {
		t.Fatalf("valid path rejected: %v", err)
	}
	if valid == "" {
		t.Fatal("valid path cleaned to empty")
	}

	for _, path := range []string{"", "../libmpv.dll", "runtime/../../libmpv.dll", `C:\Program Files\ben\libmpv.dll`, "/tmp/libmpv.dll"} {
		t.Run(path, func(t *testing.T) {
			if _, err := cleanManifestPath(path); err == nil {
				t.Fatalf("unsafe path %q was accepted", path)
			}
		})
	}
}
