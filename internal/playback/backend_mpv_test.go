//go:build !nompv

package playback

import "testing"

func TestNextPlaylistIndex(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		playlistPos   int64
		playlistCount int64
		wantIndex     int64
		wantOK        bool
	}{
		{name: "empty playlist", playlistPos: -1, playlistCount: 0, wantIndex: 0, wantOK: false},
		{name: "single current item", playlistPos: 0, playlistCount: 1, wantIndex: 0, wantOK: false},
		{name: "current plus preloaded next", playlistPos: 0, playlistCount: 2, wantIndex: 1, wantOK: true},
		{name: "advanced onto previous preload", playlistPos: 1, playlistCount: 2, wantIndex: 0, wantOK: false},
		{name: "further queued item", playlistPos: 1, playlistCount: 3, wantIndex: 2, wantOK: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotIndex, gotOK := nextPlaylistIndex(tt.playlistPos, tt.playlistCount)
			if gotIndex != tt.wantIndex || gotOK != tt.wantOK {
				t.Fatalf("nextPlaylistIndex(%d, %d) = (%d, %v), want (%d, %v)", tt.playlistPos, tt.playlistCount, gotIndex, gotOK, tt.wantIndex, tt.wantOK)
			}
		})
	}
}
