package coldstore

import (
	"testing"
	"time"
)

func TestKeyAndPartitionRoundTrip(t *testing.T) {
	day := time.Date(2025, 2, 14, 9, 30, 0, 0, time.UTC)
	key := Key("alerts", 7, day, 1700000000000)
	want := "archive/alerts/tenant=7/dt=2025-02-14/part-1700000000000.ndjson.gz"
	if key != want {
		t.Fatalf("Key = %q, want %q", key, want)
	}
	got, ok := PartitionDate(key)
	if !ok {
		t.Fatal("PartitionDate: not found")
	}
	if !got.Equal(DayOf(day)) {
		t.Errorf("PartitionDate = %v, want %v", got, DayOf(day))
	}
}

func TestPartitionDateIgnoresNonPartitionKeys(t *testing.T) {
	if _, ok := PartitionDate("archive/alerts/tenant=7/part-1.ndjson.gz"); ok {
		t.Error("expected no partition date when dt= segment is absent")
	}
	if _, ok := PartitionDate("video/2595003744/2026-07-28/ch2.mp4"); ok {
		t.Error("a bare date segment must not be mistaken for a dt= partition")
	}
}

func TestPrefix(t *testing.T) {
	if got := Prefix("driving_events", 3); got != "archive/driving_events/tenant=3/" {
		t.Errorf("Prefix = %q", got)
	}
}

func TestNewNilWithoutBucket(t *testing.T) {
	if New(nil, "ap-southeast-1", "") != nil {
		t.Error("New with empty bucket must return nil (cold disabled)")
	}
}
