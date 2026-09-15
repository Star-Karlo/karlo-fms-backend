package liveview

import "testing"

// A third of the fleet's fixes have no street — yards, ports, plantation
// roads. The address must still name the place from what IS known, or a
// consumer showing it labels those trucks "unknown" while their district and
// city are sitting in the same payload.
func TestComposeAddress(t *testing.T) {
	cases := []struct {
		jalan, kec, kota, kab, prov, want string
	}{
		{"Jl. Pemuda", "Semarang Tengah", "Semarang", "", "Jawa Tengah", "Jl. Pemuda, Semarang Tengah, Semarang, Jawa Tengah"},
		{"", "Genuk", "Semarang", "", "Jawa Tengah", "Genuk, Semarang, Jawa Tengah"},                   // no street: still a place
		{"", "Kalidoni", "", "Palembang", "Sumatera Selatan", "Kalidoni, Palembang, Sumatera Selatan"}, // regency stands in for city
		{"", "", "Semarang", "Kab. Semarang", "", "Semarang"},                                          // kota wins over kabupaten
		{"", "", "", "", "", ""}, // nothing known: empty, not "unknown"
	}
	for _, c := range cases {
		if got := composeAddress(c.jalan, c.kec, c.kota, c.kab, c.prov); got != c.want {
			t.Errorf("composeAddress(%q,%q,%q,%q,%q) = %q, want %q", c.jalan, c.kec, c.kota, c.kab, c.prov, got, c.want)
		}
	}
}
