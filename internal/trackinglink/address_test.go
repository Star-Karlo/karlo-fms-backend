package trackinglink

import (
	"testing"

	"github.com/karlo/fms-backend/internal/liveview"
)

func TestFormatAddress(t *testing.T) {
	cases := []struct {
		name string
		pos  *liveview.Position
		want string
	}{
		{"nil position", nil, ""},
		{
			"full city address prefers kota and drops the duplicate kabupaten",
			&liveview.Position{Jalan: "Jl. Jend. Sudirman", Kecamatan: "Menteng",
				Kota: "Jakarta Pusat", Kabupaten: "Jakarta Pusat", Provinsi: "DKI Jakarta"},
			"Jl. Jend. Sudirman, Menteng, Jakarta Pusat, DKI Jakarta",
		},
		{
			"rural address falls back to kabupaten when kota is empty",
			&liveview.Position{Jalan: "Jl. Raya Pantura", Kecamatan: "Kaliwungu",
				Kabupaten: "Kendal", Provinsi: "Jawa Tengah"},
			"Jl. Raya Pantura, Kaliwungu, Kendal, Jawa Tengah",
		},
		{
			// The geocoder often resolves the province but not the street; the
			// result must still read as an address rather than ", , , Jawa Barat".
			"unresolved parts are skipped, not left as empty segments",
			&liveview.Position{Provinsi: "Jawa Barat"},
			"Jawa Barat",
		},
		{"nothing resolved yields empty, so the field is omitted entirely",
			&liveview.Position{}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := formatAddress(c.pos); got != c.want {
				t.Errorf("formatAddress()\n got: %q\nwant: %q", got, c.want)
			}
		})
	}
}
