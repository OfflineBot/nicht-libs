package dualis

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// dhbw100Scale is the official DHBW Villingen-Schwenningen 100-point scale.
// Each entry: minimum percentage that maps to that grade.
// Source: https://www.dhbw-vs.de/studierende/service-beratung/noten-und-punkteskala.html
// Walk top-down — first entry whose threshold is <= percent wins.
var dhbw100Scale = []struct {
	minPercent float64
	grade      float64
}{
	{98.5, 1.0},
	{97.0, 1.1},
	{95.5, 1.2},
	{93.5, 1.3},
	{92.0, 1.4},
	{90.5, 1.5},
	{88.5, 1.6},
	{87.0, 1.7},
	{85.5, 1.8},
	{83.5, 1.9},
	{82.0, 2.0},
	{80.5, 2.1},
	{78.5, 2.2},
	{77.0, 2.3},
	{75.5, 2.4},
	{73.5, 2.5},
	{72.0, 2.6},
	{70.5, 2.7},
	{68.5, 2.8},
	{67.0, 2.9},
	{65.5, 3.0},
	{63.5, 3.1},
	{62.0, 3.2},
	{60.5, 3.3},
	{58.5, 3.4},
	{57.0, 3.5},
	{55.5, 3.6},
	{53.5, 3.7},
	{52.0, 3.8},
	{50.5, 3.9},
	{50.0, 4.0},
	{47.0, 4.1},
	{45.5, 4.2},
	{43.5, 4.3},
	{42.0, 4.4},
	{40.5, 4.5},
	{38.5, 4.6},
	{37.0, 4.7},
	{35.5, 4.8},
	{33.5, 4.9},
	{0.0, 5.0},
}

// PercentToGrade maps a percentage (0..100) to a German grade per the official
// DHBW VS 100-point scale. Returns 5.0 for any score <=33%.
func PercentToGrade(percent float64) float64 {
	if percent < 0 {
		percent = 0
	}
	for _, step := range dhbw100Scale {
		if percent >= step.minPercent {
			return step.grade
		}
	}
	return 5.0
}

// reComponentPoints captures the "<float> P" weight token from a component name.
// Examples it must match (case from the live RESULTDETAILS pages):
//
//	"Kurztest am PC - 20 P - Merk"           → 20
//	"Grundlagen IT - TKL - 34 P - Gerlach"   → 34
//	"Programmentwurf am PC - 80 P - Merk"    → 80
//	"Grundlagen - Präsentation Grp. - 30 P - Buck" → 30
var reComponentPoints = regexp.MustCompile(`(?i)\b(\d+(?:[.,]\d+)?)\s*P\b`)

// componentPoints extracts the maximum points for a component from its name.
// Returns 0 when no "X P" marker is present.
func componentPoints(name string) float64 {
	m := reComponentPoints.FindStringSubmatch(name)
	if len(m) < 2 {
		return 0
	}
	v, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", "."), 64)
	if err != nil {
		return 0
	}
	return v
}

// componentScore parses the achieved score (e.g. "17,0", "52,5", "85"). An
// empty / unparseable string returns (0, false).
func componentScore(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" || s == "-" {
		return 0, false
	}
	v, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", "."), 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// ComponentsPreliminaryGrade computes a preliminary German grade from the
// scored components of an exam attempt.
//
// Behaviour:
//   - components with a numeric "X P" weight in the name AND a numeric score are
//     summed: percent = achieved / weighted_max * 100
//   - components without a weight or without a score are ignored
//   - returns ok=false when no usable component is present, or when total
//     weight is zero
//
// The percent is converted via PercentToGrade (DHBW VS 100-point scale).
// totalMax is the sum of weights of components that contributed (used by the
// caller to know how representative the preliminary grade is).
func ComponentsPreliminaryGrade(components []ExamComponent) (grade, percent, totalMax, achieved float64, ok bool) {
	for _, c := range components {
		w := componentPoints(c.Name)
		if w <= 0 {
			continue
		}
		s, hasScore := componentScore(c.Score)
		if !hasScore {
			continue
		}
		totalMax += w
		achieved += s
	}
	if totalMax <= 0 {
		return 0, 0, 0, 0, false
	}
	percent = achieved / totalMax * 100
	grade = PercentToGrade(percent)
	return grade, percent, totalMax, achieved, true
}

// FormatGrade renders a grade as the German notation Dualis uses ("2,8"
// instead of "2.8") with one decimal place.
func FormatGrade(g float64) string {
	return strings.Replace(fmt.Sprintf("%.1f", g), ".", ",", 1)
}
