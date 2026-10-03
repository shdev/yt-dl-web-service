package ytdlp

import (
	"reflect"
	"testing"
)

func TestInfoJSONCandidates(t *testing.T) {
	tests := []struct {
		media string
		want  []string
	}{
		{"a/b/Titel [id].mp4", []string{"a/b/Titel [id].info.json", "a/b/Titel [id].mp4.info.json"}},
		{"a/b/Titel", []string{"a/b/Titel.info.json", "a/b/Titel.info.json"}},
	}
	for _, tc := range tests {
		if got := infoJSONCandidates(tc.media); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("infoJSONCandidates(%q) = %v, want %v", tc.media, got, tc.want)
		}
	}
}
