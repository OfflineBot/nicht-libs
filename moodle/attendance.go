package moodle

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// AttendanceCourse is a Moodle course that has at least one attendance module.
type AttendanceCourse struct {
	ID          int                  `json:"id"`
	Fullname    string               `json:"fullname"`
	Attendances []AttendanceActivity `json:"attendances"`
}

// AttendanceActivity is one attendance module instance within a course.
type AttendanceActivity struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	CMID int    `json:"cmid"`
}

// AttendanceStatus is one possible attendance state (e.g. Present, Late, Absent).
type AttendanceStatus struct {
	ID                  int     `json:"id"`
	Acronym             string  `json:"acronym"`
	Description         string  `json:"description"`
	Grade               float64 `json:"grade"`
	StudentAvailability bool    `json:"studentavailability"`
}

// AttendanceLog is the user's attendance record for one session (nil if not yet marked).
type AttendanceLog struct {
	ID        int   `json:"id"`
	SessionID int   `json:"sessionid"`
	StudentID int   `json:"studentid"`
	StatusID  int   `json:"statusid"`
	TimeTaken int64 `json:"timetaken"`
}

// AttendanceSession is one scheduled attendance session.
type AttendanceSession struct {
	ID              int            `json:"id"`
	AttendanceID    int            `json:"attendanceid"`
	SessDate        int64          `json:"sessdate"`
	Duration        int            `json:"duration"`
	Description     string         `json:"description"`
	StudentsCanMark bool           `json:"studentscanmark"`
	StatusSet       int            `json:"statusset"`
	IncludeQRCode   bool           `json:"includeqrcode"`
	Log             *AttendanceLog `json:"log,omitempty"`
	IsOpen          bool           `json:"is_open"` // derived: session is open for self-marking right now
}

// rawAttendanceSession is the intermediate JSON shape from Moodle (0/1 booleans etc.).
type rawAttendanceSession struct {
	ID              int            `json:"id"`
	AttendanceID    int            `json:"attendanceid"`
	SessDate        int64          `json:"sessdate"`
	Duration        int            `json:"duration"`
	Description     string         `json:"description"`
	StudentsCanMark int            `json:"studentscanmark"` // Moodle returns integer 0/1
	StatusSet       int            `json:"statusset"`
	IncludeQRCode   int            `json:"includeqrcode"`
	Log             *AttendanceLog `json:"log"`
}

// AttendanceUserData holds session list and status options for one attendance activity.
type AttendanceUserData struct {
	ID       int
	CMID     int
	Name     string
	Sessions []AttendanceSession
	// Statuses keyed by status-set number (Moodle returns {"0": [...], "1": [...]})
	Statuses map[string][]AttendanceStatus
}

// GetCoursesWithAttendance returns all courses where the token owner has access to
// an attendance module.
func GetCoursesWithAttendance(baseURL, token string) ([]AttendanceCourse, error) {
	body, err := CallAPI(baseURL, token, "mod_attendance_get_courses_with_attendance_activities", url.Values{})
	if err != nil {
		return nil, fmt.Errorf("mod_attendance_get_courses_with_attendance_activities: %w", err)
	}
	var result struct {
		Courses []AttendanceCourse `json:"courses"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("attendance courses parse: %w", err)
	}
	return result.Courses, nil
}

// GetAttendanceUserData fetches session and status data for a given attendance module (by cmid).
// Returns nil if the activity has no data.
func GetAttendanceUserData(baseURL, token string, cmID int) (*AttendanceUserData, error) {
	body, err := CallAPI(baseURL, token, "mod_attendance_get_user_data", url.Values{
		"cmid": {strconv.Itoa(cmID)},
	})
	if err != nil {
		return nil, fmt.Errorf("mod_attendance_get_user_data (cmid=%d): %w", cmID, err)
	}

	var raw struct {
		Attendances []struct {
			ID       int                    `json:"id"`
			CMID     int                    `json:"cmid"`
			Name     string                 `json:"name"`
			Sessions []rawAttendanceSession `json:"sessions"`
			Statuses json.RawMessage        `json:"statuses"`
		} `json:"attendances"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("attendance user data parse: %w", err)
	}
	if len(raw.Attendances) == 0 {
		return nil, nil
	}
	a := raw.Attendances[0]

	now := time.Now().Unix()
	sessions := make([]AttendanceSession, 0, len(a.Sessions))
	for _, rs := range a.Sessions {
		sess := AttendanceSession{
			ID:              rs.ID,
			AttendanceID:    rs.AttendanceID,
			SessDate:        rs.SessDate,
			Duration:        rs.Duration,
			Description:     rs.Description,
			StudentsCanMark: rs.StudentsCanMark != 0,
			StatusSet:       rs.StatusSet,
			IncludeQRCode:   rs.IncludeQRCode != 0,
			Log:             rs.Log,
		}
		end := rs.SessDate + int64(rs.Duration)
		sess.IsOpen = sess.StudentsCanMark && rs.SessDate <= now && now < end
		sessions = append(sessions, sess)
	}

	// Statuses from Moodle: JSON object keyed by status-set number as string.
	var statuses map[string][]AttendanceStatus
	if len(a.Statuses) > 0 {
		_ = json.Unmarshal(a.Statuses, &statuses)
	}
	if statuses == nil {
		statuses = map[string][]AttendanceStatus{}
	}

	return &AttendanceUserData{
		ID:       a.ID,
		CMID:     cmID,
		Name:     a.Name,
		Sessions: sessions,
		Statuses: statuses,
	}, nil
}

// SaveUserStatus submits the student's attendance status for a session.
// password may be empty if the session does not require one.
func SaveUserStatus(baseURL, token string, sessID, statusID, statusSet int, password string) error {
	params := url.Values{
		"sessid":    {strconv.Itoa(sessID)},
		"statusid":  {strconv.Itoa(statusID)},
		"statusset": {strconv.Itoa(statusSet)},
	}
	if password != "" {
		params.Set("sesspassword", password)
	}
	_, err := CallAPI(baseURL, token, "mod_attendance_save_user_status", params)
	if err != nil {
		return fmt.Errorf("mod_attendance_save_user_status: %w", err)
	}
	return nil
}
