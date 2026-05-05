package timetable

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// New rapla.dhbw.de HTML format: time in anchor text, date from column headers.
var (
	reRaplaTime    = regexp.MustCompile(`(\d{2}:\d{2})[\s\x{00a0}]*-[\s\x{00a0}]*(\d{2}:\d{2})`)
	reRaplaTeacher = regexp.MustCompile(`<([A-Z][a-z][A-Z])>\s*$`)
	reRaplaHeader  = regexp.MustCompile(`\w+\s+(\d{1,2})\.(\d{2})\.`)
)

// buildRaplaURL constructs the full Rapla calendar fetch URL from a base URL.
// Supports both the legacy format ("…/rapla?page=calendar") and the new format
// ("…/rapla/calendar"). Always uses HTTPS.
func buildRaplaURL(base, user, file string, monday time.Time) string {
	// Upgrade HTTP → HTTPS (new server redirects to http:// but only HTTPS works).
	base = strings.Replace(base, "http://", "https://", 1)

	u, err := url.Parse(base)
	if err != nil {
		// fallback: old-style string concatenation
		return fmt.Sprintf("%s&user=%s&file=%s&day=%d&month=%d&year=%d",
			base, url.QueryEscape(user), url.QueryEscape(file),
			monday.Day(), int(monday.Month()), monday.Year())
	}

	// Legacy URLs contain "?page=calendar" — rewrite to new path format.
	if u.Query().Get("page") == "calendar" {
		u.Path = strings.TrimSuffix(u.Path, "/") + "/calendar"
		u.RawQuery = ""
	}

	q := u.Query()
	q.Set("user", user)
	q.Set("file", file)
	q.Set("day", fmt.Sprint(monday.Day()))
	q.Set("month", fmt.Sprint(int(monday.Month())))
	q.Set("year", fmt.Sprint(monday.Year()))
	u.RawQuery = q.Encode()
	return u.String()
}

// FetchAndUpdate fetches the Rapla calendar HTML for the given week and parses
// it using the rapla.dhbw.de table format (day headers + rowspan grid tracking).
func FetchAndUpdate(raplaURL, weekStr, user, file string) (*Timetable, error) {
	monday := mondayOfWeek(weekStr)

	fetchURL := buildRaplaURL(raplaURL, user, file, monday)
	resp, err := http.Get(fetchURL)
	if err != nil {
		return nil, fmt.Errorf("fetch error: %w", err)
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	// Build date strings for each day column from "td.week_header nobr" text ("Mo 20.04.").
	year := monday.Year()
	var dayDates []string
	doc.Find("td.week_header nobr").Each(func(_ int, s *goquery.Selection) {
		m := reRaplaHeader.FindStringSubmatch(s.Text())
		if len(m) < 3 {
			return
		}
		dayDates = append(dayDates, fmt.Sprintf("%d-%s-%s", year, m[2], fmt.Sprintf("%02s", m[1])))
	})

	// Table grid: track how many more rows each column is occupied by a rowspan cell.
	const maxCols = 20
	var grid [maxCols]int

	tt := &Timetable{}

	doc.Find("tr").Each(func(_ int, row *goquery.Selection) {
		for i := range grid {
			if grid[i] > 0 {
				grid[i]--
			}
		}
		col := 0
		row.Children().Each(func(_ int, cell *goquery.Selection) {
			for col < maxCols && grid[col] > 0 {
				col++
			}
			if col >= maxCols {
				return
			}
			colspan := raplaAtoi(cell.AttrOr("colspan", "1"))
			rowspan := raplaAtoi(cell.AttrOr("rowspan", "1"))

			if strings.Contains(cell.AttrOr("class", ""), "week_block") {
				// Each day occupies 3 columns after the time-header column (col 0).
				// Block slot is the middle column: (col-1)%3 == 1, day index = (col-1)/3.
				dayIdx := (col - 1) / 3
				if dayIdx >= 0 && dayIdx < len(dayDates) {
					if lesson := parseRaplaBlock(cell, dayDates[dayIdx]); lesson != nil {
						tt.Days[dayIdx].Lessons = append(tt.Days[dayIdx].Lessons, *lesson)
					}
				}
			}

			for c := col; c < col+colspan && c < maxCols; c++ {
				grid[c] = rowspan - 1
			}
			col += colspan
		})
	})

	return tt, nil
}

// parseRaplaBlock extracts a Lesson from a td.week_block cell in the new Rapla HTML format.
func parseRaplaBlock(cell *goquery.Selection, dateStr string) *Lesson {
	anchorText := cell.Find("a").First().Text()

	tm := reRaplaTime.FindStringSubmatch(anchorText)
	if len(tm) < 3 {
		return nil
	}
	start, err := time.ParseInLocation("2006-01-02 15:04", dateStr+" "+tm[1], time.Local)
	if err != nil {
		return nil
	}
	end, _ := time.ParseInLocation("2006-01-02 15:04", dateStr+" "+tm[2], time.Local)

	// Title: anchor text after the time match, strip leading "??" marker.
	titleRaw := strings.TrimSpace(strings.TrimPrefix(anchorText, tm[0]))
	lessonType := ""
	if strings.HasPrefix(titleRaw, "??") {
		lessonType = "Prüfung"
		titleRaw = strings.TrimSpace(strings.TrimPrefix(titleRaw, "??"))
	}

	// Teacher abbreviation <XxX> at the end of the title.
	teacher := ""
	if tm2 := reRaplaTeacher.FindStringSubmatch(titleRaw); len(tm2) >= 2 {
		teacher = tm2[1]
		titleRaw = strings.TrimSpace(reRaplaTeacher.ReplaceAllString(titleRaw, ""))
	}

	isOnline := strings.Contains(strings.ToUpper(titleRaw), "ONLINE")

	// Room: first resource span whose text contains a space (course groups don't).
	room := ""
	cell.Find("span.resource").Each(func(_ int, s *goquery.Selection) {
		if room == "" && strings.Contains(s.Text(), " ") {
			room = strings.TrimSpace(s.Text())
		}
	})

	return &Lesson{
		Start:      start,
		End:        end,
		Title:      titleRaw,
		Teacher:    teacher,
		Room:       room,
		LessonType: lessonType,
		IsOnline:   isOnline,
	}
}

func raplaAtoi(s string) int {
	n := 0
	fmt.Sscan(s, &n)
	if n < 1 {
		return 1
	}
	return n
}

// CanonicalWeek normalises weekStr to "YYYY-Www" format.
// Empty string → current ISO week.
func CanonicalWeek(weekStr string) string {
	t := mondayOfWeek(weekStr)
	year, week := t.ISOWeek()
	return fmt.Sprintf("%d-W%02d", year, week)
}

// WeekWithOffset returns the canonical week string offset weeks relative to base.
// base="" means current week. offset=1 → next week, offset=-1 → last week.
func WeekWithOffset(base string, offset int) string {
	t := mondayOfWeek(base).AddDate(0, 0, offset*7)
	year, week := t.ISOWeek()
	return fmt.Sprintf("%d-W%02d", year, week)
}

// DateToWeek returns the canonical "YYYY-Www" week string for any date.
// date must be in "2006-01-02" format.
func DateToWeek(date string) (string, error) {
	t, err := time.ParseInLocation("2006-01-02", date, time.Local)
	if err != nil {
		return "", fmt.Errorf("ungültiges Datum %q — erwartet yyyy-MM-dd", date)
	}
	monday := mondayOfDate(t)
	year, week := monday.ISOWeek()
	return fmt.Sprintf("%d-W%02d", year, week), nil
}

func mondayOfDate(t time.Time) time.Time {
	offset := int(t.Weekday()) - int(time.Monday)
	if offset < 0 {
		offset += 7
	}
	return t.Truncate(24 * time.Hour).AddDate(0, 0, -offset)
}

// dhbwAppEvent mirrors one entry from api.dhbw.app/rapla/lectures/{id}/events.
type dhbwAppEvent struct {
	EntityType string   `json:"entityType"`
	StartTime  string   `json:"startTime"`
	EndTime    string   `json:"endTime"`
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	Lecturer   string   `json:"lecturer"`
	Rooms      []string `json:"rooms"`
}

// FetchDhbwApp fetches the timetable from api.dhbw.app for the given courseID
// (e.g. "RV-WDS125") and filters to the requested week.
// Returns nil, nil when courseID is empty.
func FetchDhbwApp(courseID, weekStr string) (*Timetable, error) {
	if courseID == "" {
		return nil, nil
	}

	url := "https://api.dhbw.app/rapla/lectures/" + courseID + "/events"
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("dhbw.app fetch error: %w", err)
	}
	defer resp.Body.Close()

	var events []dhbwAppEvent
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		return nil, fmt.Errorf("dhbw.app parse error: %w", err)
	}

	monday := mondayOfWeek(weekStr)
	sunday := monday.AddDate(0, 0, 7)

	tt := &Timetable{}
	for _, e := range events {
		start, err := time.Parse(time.RFC3339Nano, e.StartTime)
		if err != nil {
			continue
		}
		start = start.In(time.Local)

		if start.Before(monday) || !start.Before(sunday) {
			continue
		}

		end, _ := time.Parse(time.RFC3339Nano, e.EndTime)
		end = end.In(time.Local)

		weekday := int(start.Weekday()) - int(time.Monday)
		if weekday < 0 {
			weekday += 7
		}
		if weekday >= 7 {
			continue
		}

		room := ""
		if len(e.Rooms) > 0 {
			room = strings.Join(e.Rooms, ", ")
		}

		lessonType := ""
		if e.EntityType == "EXAM" {
			lessonType = "Prüfung"
		}

		tt.Days[weekday].Lessons = append(tt.Days[weekday].Lessons, Lesson{
			Start:      start,
			End:        end,
			Title:      strings.TrimSpace(e.Name),
			Teacher:    e.Lecturer,
			Room:       room,
			LessonType: lessonType,
			IsOnline:   e.Type == "ONLINE",
		})
	}

	return tt, nil
}

// MergeWith returns a new Timetable containing all lessons from t (primary/Rapla)
// plus any lessons from other (dhbw.app) whose start time does not already exist
// in the primary. other may be nil, in which case t is returned unchanged.
func (t *Timetable) MergeWith(other *Timetable) *Timetable {
	if other == nil {
		return t
	}
	merged := &Timetable{}
	for dayIdx := range t.Days {
		merged.Days[dayIdx].Lessons = append([]Lesson{}, t.Days[dayIdx].Lessons...)
		for _, candidate := range other.Days[dayIdx].Lessons {
			duplicate := false
			for _, existing := range t.Days[dayIdx].Lessons {
				if existing.Start.Equal(candidate.Start) {
					duplicate = true
					break
				}
			}
			if !duplicate {
				merged.Days[dayIdx].Lessons = append(merged.Days[dayIdx].Lessons, candidate)
			}
		}
	}
	return merged
}

// "2026-W12" → Montag dieser Woche; "" → Montag der aktuellen Woche
func mondayOfWeek(weekStr string) time.Time {
	if weekStr == "" {
		now := time.Now()
		offset := int(now.Weekday()) - int(time.Monday)
		if offset < 0 {
			offset += 7
		}
		return now.AddDate(0, 0, -offset).Truncate(24 * time.Hour)
	}

	var year, week int
	fmt.Sscanf(weekStr, "%d-W%d", &year, &week)

	jan4 := time.Date(year, 1, 4, 0, 0, 0, 0, time.Local)
	offset := int(jan4.Weekday()) - int(time.Monday)
	if offset < 0 {
		offset += 7
	}
	week1Monday := jan4.AddDate(0, 0, -offset)
	return week1Monday.AddDate(0, 0, (week-1)*7)
}
