package dualis

import "testing"

func TestDeriveStatus(t *testing.T) {
	cases := []struct {
		grade, statusText, want string
	}{
		{"4,5", "", "failed"},
		{"4,5", "offen", "failed"},
		{"2,1", "bestanden", "passed"},
		{"5,0", "nicht bestanden", "failed"},
		{"noch nicht gesetzt", "", "pending"},
		{"-", "", "pending"},
		{"1,0", "", "passed"},
		{"4,0", "", "passed"},
		{"", "nicht bestanden", "failed"},
	}
	for _, tc := range cases {
		got := deriveStatus(tc.grade, tc.statusText)
		if got != tc.want {
			t.Errorf("deriveStatus(%q, %q) = %q, want %q", tc.grade, tc.statusText, got, tc.want)
		}
	}
}
