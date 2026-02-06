// Package threshold parses and evaluates Nagios-standard threshold ranges.
//
// A Nagios threshold range defines when a monitoring check should trigger
// a warning or critical alert. The notation follows the Nagios Plugin
// Development Guidelines:
//
//	10      alert if value < 0 or > 10    (outside 0..10)
//	10:     alert if value < 10           (outside 10..+inf)
//	~:10    alert if value > 10           (outside -inf..10)
//	10:20   alert if value < 10 or > 20   (outside 10..20)
//	@10:20  alert if 10 <= value <= 20    (inside 10..20)
//
// This package has zero external dependencies.
package threshold

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Threshold represents a parsed Nagios threshold range.
type Threshold struct {
	Start    float64 // Lower bound of the range.
	End      float64 // Upper bound of the range.
	Inside   bool    // If true, alert when value is INSIDE the range (@ prefix).
	StartInf bool    // If true, no lower bound (~ prefix means -infinity).
}

// Parse parses a Nagios threshold range string into a Threshold.
//
// Supported formats:
//
//	"10"      → outside 0..10
//	"10:"     → outside 10..+inf
//	"~:10"    → outside -inf..10
//	"10:20"   → outside 10..20
//	"@10:20"  → inside 10..20
//	"@~:20"   → inside -inf..20
func Parse(s string) (Threshold, error) {
	if s == "" {
		return Threshold{}, fmt.Errorf("threshold must not be empty")
	}

	t := Threshold{}

	// Check for @ prefix (inside/inverted range).
	if strings.HasPrefix(s, "@") {
		t.Inside = true
		s = s[1:]
	}

	// Split on colon to separate start:end.
	if idx := strings.Index(s, ":"); idx >= 0 {
		startStr := s[:idx]
		endStr := s[idx+1:]

		// Parse start value.
		if startStr == "~" {
			t.StartInf = true
			t.Start = 0
		} else if startStr == "" {
			t.Start = 0
		} else {
			v, err := strconv.ParseFloat(startStr, 64)
			if err != nil {
				return Threshold{}, fmt.Errorf("invalid start value %q: %w", startStr, err)
			}
			t.Start = v
		}

		// Parse end value.
		if endStr == "" {
			t.End = math.Inf(1)
		} else {
			v, err := strconv.ParseFloat(endStr, 64)
			if err != nil {
				return Threshold{}, fmt.Errorf("invalid end value %q: %w", endStr, err)
			}
			t.End = v
		}
	} else {
		// No colon: simple format like "10" means 0..10.
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return Threshold{}, fmt.Errorf("invalid threshold value %q: %w", s, err)
		}
		t.Start = 0
		t.End = v
	}

	// Validate that start does not exceed end.
	if !t.StartInf && !math.IsInf(t.End, 1) && t.Start > t.End {
		return Threshold{}, fmt.Errorf("start value %s must not exceed end value %s",
			formatFloat(t.Start), formatFloat(t.End))
	}

	return t, nil
}

// Violated reports whether the given value triggers an alert according to
// this threshold.
//
// For a standard threshold (Inside == false), the value violates the
// threshold when it falls OUTSIDE the range [Start, End].
//
// For an inverted threshold (Inside == true, @ prefix), the value violates
// the threshold when it falls INSIDE the range.
func (t Threshold) Violated(value float64) bool {
	var inRange bool
	if t.StartInf {
		inRange = value <= t.End
	} else {
		inRange = value >= t.Start && value <= t.End
	}

	if t.Inside {
		return inRange
	}
	return !inRange
}

// String serializes the Threshold back to Nagios range notation.
//
// The output is suitable for perfdata and can be round-tripped through Parse
// to produce an equivalent Threshold.
func (t Threshold) String() string {
	var b strings.Builder

	if t.Inside {
		b.WriteByte('@')
	}

	if t.StartInf {
		b.WriteByte('~')
		b.WriteByte(':')
		b.WriteString(formatFloat(t.End))
		return b.String()
	}

	if math.IsInf(t.End, 1) {
		b.WriteString(formatFloat(t.Start))
		b.WriteByte(':')
		return b.String()
	}

	if t.Start == 0 && !t.Inside {
		b.WriteString(formatFloat(t.End))
		return b.String()
	}

	b.WriteString(formatFloat(t.Start))
	b.WriteByte(':')
	b.WriteString(formatFloat(t.End))
	return b.String()
}

// formatFloat formats a float64 as a compact string: integers without a
// decimal point (e.g. "80"), and fractional values with minimal precision
// (e.g. "1.5").
func formatFloat(v float64) string {
	if v == math.Trunc(v) && !math.IsInf(v, 0) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}
