package telemetry

import (
	"math"
	"time"
)

// maxPlausibleKmh: a fix implying a faster jump than this from the previous
// kept fix is a bad GPS fix (multipath / cold start), not a truck.
const maxPlausibleKmh = 220.0

// FilterTeleports drops physically impossible GPS fixes from a track — the
// "GPS quality filter" applied before any analytics (trips, behavior, reports).
// A point is dropped when reaching it from the last kept point would require
// implausible speed. Points without a fix pass through untouched (they carry
// time/ignition info).
func FilterTeleports(pos []Position) []Position {
	if len(pos) < 3 {
		return pos
	}
	out := make([]Position, 0, len(pos))
	var lastLat, lastLon *float64
	var lastT time.Time
	for _, p := range pos {
		if p.Lat == nil || p.Lon == nil {
			out = append(out, p)
			continue
		}
		if lastLat != nil {
			dt := p.Time.Sub(lastT).Hours()
			if dt > 0 {
				if distKm(*lastLat, *lastLon, *p.Lat, *p.Lon)/dt > maxPlausibleKmh {
					continue // teleport — drop the fix
				}
			}
		}
		lastLat, lastLon, lastT = p.Lat, p.Lon, p.Time
		out = append(out, p)
	}
	return out
}

func distKm(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371.0
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return R * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}
