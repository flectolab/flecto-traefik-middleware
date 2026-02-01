package types

import (
	"encoding/json"
	"fmt"
	"time"
)

// Duration is a wrapper around time.Duration that supports JSON unmarshaling
// from both string ("10ms", "1s") and number (nanoseconds) formats.
type Duration time.Duration

// NewDuration creates a Duration from a time.Duration.
func NewDuration(d time.Duration) Duration {
	return Duration(d)
}

// Duration returns the underlying time.Duration value.
func (d Duration) Duration() time.Duration {
	return time.Duration(d)
}

// String returns the string representation of the duration.
func (d Duration) String() string {
	return time.Duration(d).String()
}

// Nanoseconds returns the duration as an integer nanosecond count.
func (d Duration) Nanoseconds() int64 {
	return int64(d)
}

// Milliseconds returns the duration as an integer millisecond count.
func (d Duration) Milliseconds() int64 {
	return int64(d) / int64(time.Millisecond)
}

// Seconds returns the duration as a floating point number of seconds.
func (d Duration) Seconds() float64 {
	return time.Duration(d).Seconds()
}

// MarshalJSON implements json.Marshaler.
// Marshals as nanoseconds (int64) for compatibility.
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(int64(d))
}

// UnmarshalJSON implements json.Unmarshaler.
// Accepts both string ("10ms", "1s") and number (nanoseconds) formats.
func (d *Duration) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as string first (e.g., "10ms", "1s")
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		parsed, err := time.ParseDuration(str)
		if err != nil {
			return fmt.Errorf("invalid duration string: %w", err)
		}
		*d = Duration(parsed)
		return nil
	}

	// Try to unmarshal as number (nanoseconds)
	var ns int64
	if err := json.Unmarshal(data, &ns); err != nil {
		return fmt.Errorf("duration must be a string (e.g., \"10ms\") or number (nanoseconds): %w", err)
	}
	*d = Duration(ns)
	return nil
}

// Scan implements sql.Scanner for database reads.
func (d *Duration) Scan(value interface{}) error {
	if value == nil {
		*d = 0
		return nil
	}
	switch v := value.(type) {
	case int64:
		*d = Duration(v)
		return nil
	case float64:
		*d = Duration(int64(v))
		return nil
	default:
		return fmt.Errorf("cannot scan %T into Duration", value)
	}
}
