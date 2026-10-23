package liveview

import (
	"testing"
	"time"

	"github.com/karlo/fms-backend/internal/telemetry"
)

// The four alerts K-9467-BF raised on 20-21 October 2026. None of them was
// fuel: the CAN counter did not move at any of them, two drained their litres
// in under five seconds, and all four are the sender wandering in the 54-58%
// band it sits in while parked. A 340 L tank with a whole-percent sender
// moves 3.4 L per step, and the 5 L floor was less than two of those.
func TestDetectTheftRejectsSensorNoise(t *testing.T) {
	tank := 340.0
	stopped := 0.0
	on, off := true, false
	at := time.Date(2026, 10, 20, 23, 1, 21, 0, time.UTC)

	// The four events, with the metadata the device actually sent. Every one
	// of them carried Movement 1 on the reading BEFORE the drop — the truck
	// was being shunted, or the engine was still running — and two of them
	// shed their litres in under five seconds.
	moving := []byte(`{"Movement":1}`)
	stoppedMd := []byte(`{"Movement":0}`)
	cases := []struct {
		name    string
		dropPct float64
		gapMin  float64
		prev    telemetry.Position
		cur     telemetry.Position
	}{
		{
			"20 Oct 23:01 — 14 L in 4s, engine stopping",
			4, 4.0 / 60,
			telemetry.Position{Time: at.Add(-4 * time.Second), Speed: &stopped, Ignition: &on, Metadata: moving},
			telemetry.Position{Time: at, Speed: &stopped, Ignition: &off, Metadata: stoppedMd},
		},
		{
			"21 Oct 18:16 — 10 L in 3s, engine stopping",
			3, 3.0 / 60,
			telemetry.Position{Time: at.Add(-3 * time.Second), Speed: &stopped, Ignition: &on, Metadata: moving},
			telemetry.Position{Time: at, Speed: &stopped, Ignition: &off, Metadata: stoppedMd},
		},
		{
			"21 Oct 10:21 — 10 L while being shunted (speed 0, Movement 1)",
			3, 1.5,
			telemetry.Position{Time: at.Add(-90 * time.Second), Speed: &stopped, Metadata: moving},
			telemetry.Position{Time: at, Speed: &stopped, Metadata: moving},
		},
		{
			"21 Oct 15:09 — 7 L, two sensor steps and shunting",
			2, 1.5,
			telemetry.Position{Time: at.Add(-90 * time.Second), Speed: &stopped, Metadata: moving},
			telemetry.Position{Time: at, Speed: &stopped, Metadata: moving},
		},
	}
	for _, c := range cases {
		if _, ok := detectTheft(c.dropPct, c.gapMin, c.prev, c.cur, &tank, false); ok {
			t.Errorf("%s: flagged as theft, should not be", c.name)
		}
	}
}

// Three sensor steps while genuinely stationary is still a theft: 10 L off a
// parked truck over ninety seconds is 6.7 L/min, which is a hose. The floor
// exists to clear the quantiser, not to excuse a siphon.
func TestDetectTheftKeepsASmallButRealLoss(t *testing.T) {
	tank := 340.0
	stopped := 0.0
	now := time.Now()
	still := []byte(`{"Movement":0}`)
	prev := telemetry.Position{Time: now.Add(-90 * time.Second), Speed: &stopped, Metadata: still}
	cur := telemetry.Position{Time: now, Speed: &stopped, Metadata: still}
	if _, ok := detectTheft(3, 1.5, prev, cur, &tank, false); !ok {
		t.Error("10 L off a stationary truck in 90s should still be flagged")
	}
}

// A siphon still has to be caught: a big drop, at a stop, over minutes.
func TestDetectTheftStillCatchesARealSiphon(t *testing.T) {
	tank := 340.0
	stopped := 0.0
	pt := telemetry.Position{Time: time.Now(), Speed: &stopped}
	prev := telemetry.Position{Time: time.Now().Add(-8 * time.Minute), Speed: &stopped}

	// 60 L (17.6%) drawn off over 8 minutes — 7.5 L/min, a hose and a drum.
	te, ok := detectTheft(17.6, 8, prev, pt, &tank, false)
	if !ok {
		t.Fatal("60 L over 8 minutes at a stop is exactly what this is for")
	}
	if te.Liters < 59 || te.Liters > 61 {
		t.Errorf("litres = %v, want ~60", te.Liters)
	}
}

// The rate ceiling must not be the only guard: a vehicle with no tank size
// configured is judged on percent alone, and that path is unchanged.
func TestDetectTheftWithoutATankSize(t *testing.T) {
	stopped := 0.0
	pt := telemetry.Position{Time: time.Now(), Speed: &stopped}
	prev := telemetry.Position{Time: time.Now().Add(-90 * time.Second), Speed: &stopped}

	if _, ok := detectTheft(4, 1.5, prev, pt, nil, false); ok {
		t.Error("a 4% drop is under the 15% fallback threshold")
	}
	if _, ok := detectTheft(20, 5, prev, pt, nil, false); !ok {
		t.Error("a 20% drop at a stop should still be flagged when no tank is set")
	}
}

// The device's own Movement flag, and an ignition transition, are the two
// mechanical explanations behind the four false alarms. Neither is a loss.
func TestDetectTheftIgnoresShuntingAndIgnitionChange(t *testing.T) {
	tank := 340.0
	stopped := 0.0
	on, off := true, false
	now := time.Now()
	moving := telemetry.Position{Time: now, Speed: &stopped, Metadata: []byte(`{"Movement":1}`)}
	still := telemetry.Position{Time: now.Add(-90 * time.Second), Speed: &stopped, Metadata: []byte(`{"Movement":0}`)}

	if _, ok := detectTheft(17.6, 1.5, still, moving, &tank, false); ok {
		t.Error("speed 0 with Movement 1 is a truck being shunted — the tank sloshes, nothing is stolen")
	}
	if _, ok := detectTheft(17.6, 1.5, moving, still, &tank, false); ok {
		t.Error("movement on either side of the drop is enough to explain it")
	}

	wasOn := telemetry.Position{Time: now.Add(-3 * time.Second), Speed: &stopped, Ignition: &on}
	nowOff := telemetry.Position{Time: now, Speed: &stopped, Ignition: &off}
	if _, ok := detectTheft(17.6, 0.05, wasOn, nowOff, &tank, false); ok {
		t.Error("the level the sender reports shifts when the engine stops; the fuel does not")
	}
}

// Moving, or across a long gap, was never theft and still is not.
func TestDetectTheftIgnoresMovingAndLongGaps(t *testing.T) {
	tank := 340.0
	moving := 40.0
	pt := telemetry.Position{Time: time.Now(), Speed: &moving}
	prev := telemetry.Position{Time: time.Now().Add(-8 * time.Minute), Speed: &moving}
	if _, ok := detectTheft(17.6, 8, prev, pt, &tank, false); ok {
		t.Error("you cannot siphon a moving truck")
	}
	stopped := 0.0
	pt.Speed = &stopped
	prev.Speed = &stopped
	if _, ok := detectTheft(17.6, 45, prev, pt, &tank, false); ok {
		t.Error("a drop spread over 45 minutes of silence is consumption, not a siphon")
	}
}
