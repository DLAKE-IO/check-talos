package threshold

import (
	"math"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Threshold
		wantErr bool
		errMsg  string
	}{
		// Standard formats from Nagios guidelines.
		{
			name:  "simple upper bound",
			input: "10",
			want:  Threshold{Start: 0, End: 10, Inside: false, StartInf: false},
		},
		{
			name:  "open-ended upper",
			input: "10:",
			want:  Threshold{Start: 10, End: math.Inf(1), Inside: false, StartInf: false},
		},
		{
			name:  "no lower bound",
			input: "~:10",
			want:  Threshold{Start: 0, End: 10, Inside: false, StartInf: true},
		},
		{
			name:  "explicit range",
			input: "10:20",
			want:  Threshold{Start: 10, End: 20, Inside: false, StartInf: false},
		},
		{
			name:  "inside range",
			input: "@10:20",
			want:  Threshold{Start: 10, End: 20, Inside: true, StartInf: false},
		},
		{
			name:  "inside with no lower bound",
			input: "@~:20",
			want:  Threshold{Start: 0, End: 20, Inside: true, StartInf: true},
		},

		// Negative values.
		{
			name:  "negative start",
			input: "-10:20",
			want:  Threshold{Start: -10, End: 20, Inside: false, StartInf: false},
		},
		{
			name:  "negative both",
			input: "-20:-10",
			want:  Threshold{Start: -20, End: -10, Inside: false, StartInf: false},
		},

		// Float values.
		{
			name:  "float range",
			input: "1.5:9.5",
			want:  Threshold{Start: 1.5, End: 9.5, Inside: false, StartInf: false},
		},
		{
			name:  "float simple",
			input: "3.14",
			want:  Threshold{Start: 0, End: 3.14, Inside: false, StartInf: false},
		},

		// Large values (etcd DB size thresholds).
		{
			name:  "large value simple",
			input: "100000000",
			want:  Threshold{Start: 0, End: 100000000, Inside: false, StartInf: false},
		},
		{
			name:  "large value range",
			input: "100000000:200000000",
			want:  Threshold{Start: 100000000, End: 200000000, Inside: false, StartInf: false},
		},

		// Zero.
		{
			name:  "zero threshold",
			input: "0",
			want:  Threshold{Start: 0, End: 0, Inside: false, StartInf: false},
		},
		{
			name:  "tilde colon zero",
			input: "~:0",
			want:  Threshold{Start: 0, End: 0, Inside: false, StartInf: true},
		},

		// Inside with zero start.
		{
			name:  "inside with zero start",
			input: "@0:10",
			want:  Threshold{Start: 0, End: 10, Inside: true, StartInf: false},
		},

		// Error cases.
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
			errMsg:  "threshold must not be empty",
		},
		{
			name:    "non-numeric",
			input:   "abc",
			wantErr: true,
		},
		{
			name:    "non-numeric end",
			input:   "10:abc",
			wantErr: true,
		},
		{
			name:    "non-numeric start",
			input:   "abc:10",
			wantErr: true,
		},
		{
			name:    "start exceeds end",
			input:   "20:10",
			wantErr: true,
			errMsg:  "start value 20 must not exceed end value 10",
		},
		{
			name:    "start exceeds end float",
			input:   "9.5:1.5",
			wantErr: true,
			errMsg:  "start value 9.5 must not exceed end value 1.5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("Parse(%q) expected error, got nil", tt.input)
				}
				if tt.errMsg != "" && !containsSubstring(err.Error(), tt.errMsg) {
					t.Errorf("Parse(%q) error = %q, want substring %q", tt.input, err.Error(), tt.errMsg)
				}
				return
			}

			if err != nil {
				t.Fatalf("Parse(%q) unexpected error: %v", tt.input, err)
			}

			if got.Start != tt.want.Start {
				t.Errorf("Parse(%q).Start = %v, want %v", tt.input, got.Start, tt.want.Start)
			}
			if got.End != tt.want.End {
				t.Errorf("Parse(%q).End = %v, want %v", tt.input, got.End, tt.want.End)
			}
			if got.Inside != tt.want.Inside {
				t.Errorf("Parse(%q).Inside = %v, want %v", tt.input, got.Inside, tt.want.Inside)
			}
			if got.StartInf != tt.want.StartInf {
				t.Errorf("Parse(%q).StartInf = %v, want %v", tt.input, got.StartInf, tt.want.StartInf)
			}
		})
	}
}

func TestViolated(t *testing.T) {
	tests := []struct {
		name      string
		threshold string
		value     float64
		want      bool
	}{
		// Standard threshold "80": alert if outside 0..80.
		{"80 / value=0", "80", 0, false},
		{"80 / value=79", "80", 79, false},
		{"80 / value=80 (boundary)", "80", 80, false},
		{"80 / value=80.1", "80", 80.1, true},
		{"80 / value=-1", "80", -1, true},

		// Range "10:20": alert if outside 10..20.
		{"10:20 / value=9", "10:20", 9, true},
		{"10:20 / value=10 (lower boundary)", "10:20", 10, false},
		{"10:20 / value=15", "10:20", 15, false},
		{"10:20 / value=20 (upper boundary)", "10:20", 20, false},
		{"10:20 / value=21", "10:20", 21, true},

		// Inside range "@10:20": alert if inside 10..20.
		{"@10:20 / value=9", "@10:20", 9, false},
		{"@10:20 / value=10 (lower boundary)", "@10:20", 10, true},
		{"@10:20 / value=15", "@10:20", 15, true},
		{"@10:20 / value=20 (upper boundary)", "@10:20", 20, true},
		{"@10:20 / value=21", "@10:20", 21, false},

		// Open-ended "10:": alert if < 10 (outside 10..+inf).
		{"10: / value=9", "10:", 9, true},
		{"10: / value=10 (boundary)", "10:", 10, false},
		{"10: / value=1000000", "10:", 1000000, false},

		// No lower bound "~:10": alert if > 10 (outside -inf..10).
		{"~:10 / value=-1000", "~:10", -1000, false},
		{"~:10 / value=0", "~:10", 0, false},
		{"~:10 / value=10 (boundary)", "~:10", 10, false},
		{"~:10 / value=11", "~:10", 11, true},

		// Zero threshold.
		{"0 / value=0", "0", 0, false},
		{"0 / value=0.1", "0", 0.1, true},
		{"0 / value=-0.1", "0", -0.1, true},

		// ~:0 -- alert if > 0.
		{"~:0 / value=-1", "~:0", -1, false},
		{"~:0 / value=0", "~:0", 0, false},
		{"~:0 / value=1", "~:0", 1, true},

		// Negative range.
		{"-10:20 / value=-11", "-10:20", -11, true},
		{"-10:20 / value=-10", "-10:20", -10, false},
		{"-10:20 / value=0", "-10:20", 0, false},
		{"-10:20 / value=20", "-10:20", 20, false},
		{"-10:20 / value=21", "-10:20", 21, true},

		// Inside with tilde.
		{"@~:20 / value=-1000", "@~:20", -1000, true},
		{"@~:20 / value=20", "@~:20", 20, true},
		{"@~:20 / value=21", "@~:20", 21, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			th, err := Parse(tt.threshold)
			if err != nil {
				t.Fatalf("Parse(%q) unexpected error: %v", tt.threshold, err)
			}

			got := th.Violated(tt.value)
			if got != tt.want {
				t.Errorf("Parse(%q).Violated(%v) = %v, want %v",
					tt.threshold, tt.value, got, tt.want)
			}
		})
	}
}

func TestString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple upper", "80", "80"},
		{"explicit range", "10:20", "10:20"},
		{"inside range", "@10:20", "@10:20"},
		{"no lower bound", "~:10", "~:10"},
		{"open-ended upper", "10:", "10:"},
		{"float range", "1.5:9.5", "1.5:9.5"},
		{"inside with tilde", "@~:20", "@~:20"},
		{"zero", "0", "0"},
		{"tilde colon zero", "~:0", "~:0"},
		{"negative start", "-10:20", "-10:20"},
		{"large value", "100000000", "100000000"},
		{"large range", "100000000:200000000", "100000000:200000000"},
		{"inside zero start", "@0:10", "@0:10"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			th, err := Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse(%q) unexpected error: %v", tt.input, err)
			}

			got := th.String()
			if got != tt.want {
				t.Errorf("Parse(%q).String() = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestStringRoundtrip(t *testing.T) {
	inputs := []string{
		"80",
		"10:20",
		"@10:20",
		"~:10",
		"10:",
		"1.5:9.5",
		"@~:20",
		"0",
		"~:0",
		"-10:20",
		"@0:10",
	}

	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			th1, err := Parse(input)
			if err != nil {
				t.Fatalf("Parse(%q) unexpected error: %v", input, err)
			}

			s := th1.String()

			th2, err := Parse(s)
			if err != nil {
				t.Fatalf("Parse(%q) [roundtrip] unexpected error: %v", s, err)
			}

			if th1.Start != th2.Start || th1.End != th2.End ||
				th1.Inside != th2.Inside || th1.StartInf != th2.StartInf {
				t.Errorf("roundtrip mismatch: Parse(%q) = %+v, Parse(%q) = %+v",
					input, th1, s, th2)
			}
		})
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
