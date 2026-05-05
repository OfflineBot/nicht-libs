package timetable

import "time"

type Timetable struct {
	ClassID int
	Days    [7]WeekDay
}

type WeekDay struct {
	Lessons []Lesson
}

type Lesson struct {
	ID          int
	Start       time.Time
	End         time.Time
	Room        string
	Title       string
	Topic       string
	LessonType  string
	Teacher     string
	IsOnline    bool
	IsCancelled bool
	ExtraInfo   string
}
