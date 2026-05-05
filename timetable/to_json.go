package timetable

import (
	"fmt"
	"strings"
)


type LectureJSON struct {
	ID       string `json:"id"`
	Day      string `json:"day"`
	Title    string `json:"title"`
	Room     string `json:"room"`
	Lecturer string `json:"lecturer"`
	Start    string `json:"start"`
	End      string `json:"end"`
	Status   string `json:"status"`
	Color    string `json:"color"`
}

type TimetableJSON struct {
	Lectures []LectureJSON `json:"lectures"`
}

var dayNames = [7]string{"Montag", "Dienstag", "Mittwoch", "Donnerstag", "Freitag", "Samstag", "Sonntag"}

func (t *Timetable) ToJSON() TimetableJSON {
	var lectures []LectureJSON
	for dayIdx, day := range t.Days {
		for lessonIdx, l := range day.Lessons {
			status := "normal"
			if l.IsCancelled {
				status = "cancelled"
			} else if l.LessonType == "Prüfung" {
				status = "exam"
			}

			lectures = append(lectures, LectureJSON{
				ID:       fmt.Sprintf("%d-%d", dayIdx, lessonIdx),
				Day:      dayNames[dayIdx],
				Title:    l.Title,
				Room:     l.Room,
				Lecturer: l.Teacher,
				Start:    l.Start.Format("2006-01-02T15:04:00Z07:00"),
				End:      l.End.Format("2006-01-02T15:04:00Z07:00"),
				Status:   status,
				Color:    "", // assigned persistently in store when saved to DB
			})
		}
	}
	if lectures == nil {
		lectures = []LectureJSON{}
	}
	return TimetableJSON{Lectures: lectures}
}

// FilterByDate returns a TimetableJSON containing only lectures on the given date.
// date must be in "2006-01-02" format.
func (tj TimetableJSON) FilterByDate(date string) TimetableJSON {
	var filtered []LectureJSON
	for _, l := range tj.Lectures {
		if strings.HasPrefix(l.Start, date) {
			filtered = append(filtered, l)
		}
	}
	if filtered == nil {
		filtered = []LectureJSON{}
	}
	return TimetableJSON{Lectures: filtered}
}

