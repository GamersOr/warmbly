package models

import (
	"encoding/json"
	"time"
)

// NullableTime distinguishes an absent JSON field from an explicit null in
// PATCH payloads. Absent leaves the column untouched; null clears it.
type NullableTime struct {
	Set   bool       `json:"-"`
	Value *time.Time `json:"-"`
}

func (n *NullableTime) UnmarshalJSON(b []byte) error {
	n.Set = true
	if string(b) == "null" {
		n.Value = nil
		return nil
	}
	return json.Unmarshal(b, &n.Value)
}

func (n NullableTime) MarshalJSON() ([]byte, error) {
	if n.Value == nil {
		return []byte("null"), nil
	}
	return json.Marshal(n.Value)
}
