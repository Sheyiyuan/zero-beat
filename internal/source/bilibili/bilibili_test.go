package bilibili

import (
	"testing"
	"time"

	"github.com/sheyiyuan/zero-beat/internal/bili"
	"github.com/sheyiyuan/zero-beat/internal/source"
)

func TestMetadataAndQualityMapping(t *testing.T) {
	s := New(bili.New())
	if s.ID() != source.Bilibili || s.Name() != "Bilibili" {
		t.Fatalf("音源标识不符: %s %s", s.ID(), s.Name())
	}
	if !s.Caps().Has(source.CapMultiPart) || !s.Caps().Has(source.CapRemotePlaylists) || !s.Caps().Has(source.CapPlaylistWrite) {
		t.Fatal("B 站能力位不完整")
	}
	cases := map[source.Quality]bili.Quality{
		source.QualityAuto:     bili.QualityAuto,
		source.QualityHigh:     bili.QualityHigh,
		source.QualityLossless: bili.QualityLossless,
	}
	for in, want := range cases {
		if got := mapQuality(in); got != want {
			t.Fatalf("mapQuality(%v) = %v，期望 %v", in, got, want)
		}
	}
	if timeMS(1500) != 1500*time.Millisecond {
		t.Fatal("timeMS 换算错误")
	}
}
