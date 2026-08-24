package models

import (
	"encoding/json"
	"testing"
	"time"
)

// Issue #171: PATCH /campaigns must distinguish an absent start_date (leave
// untouched) from an explicit null (clear it / start now).
func TestNullableTimeAbsentVsNullVsValue(t *testing.T) {
	var u UpdateCampaign
	if err := json.Unmarshal([]byte(`{}`), &u); err != nil {
		t.Fatal(err)
	}
	if u.StartDate.Set {
		t.Fatal("absent field must not be marked Set")
	}

	u = UpdateCampaign{}
	if err := json.Unmarshal([]byte(`{"start_date":null}`), &u); err != nil {
		t.Fatal(err)
	}
	if !u.StartDate.Set || u.StartDate.Value != nil {
		t.Fatalf("explicit null must be Set with nil Value, got Set=%v Value=%v", u.StartDate.Set, u.StartDate.Value)
	}

	u = UpdateCampaign{}
	if err := json.Unmarshal([]byte(`{"start_date":"2030-01-02T00:00:00Z"}`), &u); err != nil {
		t.Fatal(err)
	}
	want := time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC)
	if !u.StartDate.Set || u.StartDate.Value == nil || !u.StartDate.Value.Equal(want) {
		t.Fatalf("value must round-trip, got Set=%v Value=%v", u.StartDate.Set, u.StartDate.Value)
	}
}

func TestUpdateCampaignTouchesSchedule(t *testing.T) {
	var u UpdateCampaign
	if err := json.Unmarshal([]byte(`{"name":"x"}`), &u); err != nil {
		t.Fatal(err)
	}
	if u.TouchesSchedule() {
		t.Fatal("a name-only patch must not touch the schedule")
	}
	if err := json.Unmarshal([]byte(`{"start_date":null}`), &u); err != nil {
		t.Fatal(err)
	}
	if !u.TouchesSchedule() {
		t.Fatal("clearing start_date must count as a schedule change")
	}
}
