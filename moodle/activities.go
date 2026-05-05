package moodle

import (
	"encoding/json"
	"net/url"
	"strconv"
)

func itoa(n int) string { return strconv.Itoa(n) }

// ── Completion ────────────────────────────────────────────────────────────────

type ActivityCompletion struct {
	CMID          int   `json:"cmid"`
	ModuleName    string `json:"modname"`
	Instance      int    `json:"instance"`
	IsTracked     bool   `json:"istracked"`
	State         int    `json:"state"` // 0=incomplete 1=complete 2=complete+pass 3=complete+fail
	Timecompleted int64  `json:"timecompleted"`
}

type CompletionStatus struct {
	CourseID   int                  `json:"courseid"`
	Activities []ActivityCompletion `json:"statuses"`
}

// GetCompletionStatus returns completion state for all activities in a course.
func GetCompletionStatus(baseURL, token string, courseID int) (*CompletionStatus, error) {
	body, err := CallAPI(baseURL, token, "core_completion_get_activities_completion_status", url.Values{
		"courseid": {itoa(courseID)},
	})
	if err != nil {
		return nil, err
	}
	var out CompletionStatus
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SetCompletion marks an activity complete (completed=1) or incomplete (completed=0).
// Only works for activities with manual completion tracking.
func SetCompletion(baseURL, token string, cmID, completed int) error {
	_, err := CallAPI(baseURL, token, "core_completion_update_activity_completion_status_manually", url.Values{
		"cmid":      {itoa(cmID)},
		"completed": {itoa(completed)},
	})
	return err
}

// ── Assignments ───────────────────────────────────────────────────────────────

type AssignFile struct {
	Filename string `json:"filename"`
	FileURL  string `json:"fileurl"`
	Filesize int    `json:"filesize"`
	MimeType string `json:"mimetype"`
}

type Assignment struct {
	ID               int          `json:"id"`
	CMID             int          `json:"cmid"`
	CourseID         int          `json:"course"`
	Name             string       `json:"name"`
	Intro            string       `json:"intro"`
	IntroFiles       []AssignFile `json:"introfiles"`
	DueDate          int64        `json:"duedate"`
	AllowSubmissions int64        `json:"allowsubmissionsfromdate"`
	CutoffDate       int64        `json:"cutoffdate"`
	Grade            float64      `json:"grade"`
	MaxAttempts      int          `json:"maxattempts"`
}

// GetAssignments returns all assignments in a course.
func GetAssignments(baseURL, token string, courseID int) ([]Assignment, error) {
	body, err := CallAPI(baseURL, token, "mod_assign_get_assignments", url.Values{
		"courseids[0]": {itoa(courseID)},
	})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Courses []struct {
			Assignments []Assignment `json:"assignments"`
		} `json:"courses"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	if len(resp.Courses) == 0 {
		return []Assignment{}, nil
	}
	return resp.Courses[0].Assignments, nil
}

// GetAssignmentSubmissionStatus returns the raw Moodle submission status for an assignment.
func GetAssignmentSubmissionStatus(baseURL, token string, assignID int) (map[string]any, error) {
	body, err := CallAPI(baseURL, token, "mod_assign_get_submission_status", url.Values{
		"assignid": {itoa(assignID)},
	})
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ── Quizzes ───────────────────────────────────────────────────────────────────

type Quiz struct {
	ID          int     `json:"id"`
	CMID        int     `json:"coursemodule"`
	CourseID    int     `json:"course"`
	Name        string  `json:"name"`
	Intro       string  `json:"intro"`
	TimeOpen    int64   `json:"timeopen"`
	TimeClose   int64   `json:"timeclose"`
	TimeLimit   int     `json:"timelimit"`
	Grade       float64 `json:"grade"`
	MaxAttempts int     `json:"attempts"`
}

type QuizAttempt struct {
	ID         int     `json:"id"`
	QuizID     int     `json:"quiz"`
	Attempt    int     `json:"attempt"`
	State      string  `json:"state"` // inprogress|overdue|finished|abandoned
	TimeStart  int64   `json:"timestart"`
	TimeFinish int64   `json:"timefinish"`
	SumGrades  float64 `json:"sumgrades"`
}

// GetQuizzes returns all quizzes in a course.
func GetQuizzes(baseURL, token string, courseID int) ([]Quiz, error) {
	body, err := CallAPI(baseURL, token, "mod_quiz_get_quizzes_by_courses", url.Values{
		"courseids[0]": {itoa(courseID)},
	})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Quizzes []Quiz `json:"quizzes"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return resp.Quizzes, nil
}

// GetQuizAttempts returns all attempts the current user made for a quiz.
func GetQuizAttempts(baseURL, token string, quizID int) ([]QuizAttempt, error) {
	body, err := CallAPI(baseURL, token, "mod_quiz_get_user_attempts", url.Values{
		"quizid": {itoa(quizID)},
		"status": {"all"},
	})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Attempts []QuizAttempt `json:"attempts"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return resp.Attempts, nil
}

// ── Participants ──────────────────────────────────────────────────────────────

type ParticipantRole struct {
	RoleID    int    `json:"roleid"`
	Name      string `json:"name"`
	ShortName string `json:"shortname"`
}

type Participant struct {
	ID              int               `json:"id"`
	Username        string            `json:"username"`
	FirstName       string            `json:"firstname"`
	LastName        string            `json:"lastname"`
	FullName        string            `json:"fullname"`
	Email           string            `json:"email"`
	Roles           []ParticipantRole `json:"roles"`
	ProfileImageURL string            `json:"profileimageurl"`
}

// GetParticipants returns all enrolled users in a course.
func GetParticipants(baseURL, token string, courseID int) ([]Participant, error) {
	body, err := CallAPI(baseURL, token, "core_enrol_get_enrolled_users", url.Values{
		"courseid": {itoa(courseID)},
	})
	if err != nil {
		return nil, err
	}
	var out []Participant
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ── Grades ────────────────────────────────────────────────────────────────────

type GradeItem struct {
	ID             int      `json:"id"`
	ItemName       string   `json:"itemname"`
	ItemType       string   `json:"itemtype"`
	ItemModule     string   `json:"itemmodule"`
	Grade          *float64 `json:"graderaw"`
	GradeMin       float64  `json:"grademin"`
	GradeMax       float64  `json:"grademax"`
	GradePassed    *bool    `json:"gradepassed"`
	Feedback       string   `json:"feedback"`
	GradeFormatted string   `json:"gradeformatted"`
}

// GetGrades returns grade items for the current user in a course.
func GetGrades(baseURL, token string, courseID int) ([]GradeItem, error) {
	body, err := CallAPI(baseURL, token, "gradereport_user_get_grade_items", url.Values{
		"courseid": {itoa(courseID)},
	})
	if err != nil {
		return nil, err
	}
	var resp struct {
		UserGrades []struct {
			GradeItems []GradeItem `json:"gradeitems"`
		} `json:"usergrades"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	if len(resp.UserGrades) == 0 {
		return []GradeItem{}, nil
	}
	return resp.UserGrades[0].GradeItems, nil
}

// ── Favourites / Hidden ───────────────────────────────────────────────────────

// SetFavourite stars or unstars a course in the overview.
func SetFavourite(baseURL, token string, courseID int, favourite bool) error {
	fav := "0"
	if favourite {
		fav = "1"
	}
	_, err := CallAPI(baseURL, token, "core_course_set_favourite_courses", url.Values{
		"courses[0][id]":        {itoa(courseID)},
		"courses[0][favourite]": {fav},
	})
	return err
}

// SetHidden hides or shows a course in the Moodle overview.
func SetHidden(baseURL, token string, courseID int, hidden bool) error {
	h := "0"
	if hidden {
		h = "1"
	}
	_, err := CallAPI(baseURL, token, "block_myoverview_set_hidden_courses", url.Values{
		"courses[0][id]":     {itoa(courseID)},
		"courses[0][hidden]": {h},
	})
	return err
}

// ── Search ────────────────────────────────────────────────────────────────────

type SearchResult struct {
	Title      string `json:"title"`
	DocURL     string `json:"docurl"`
	CourseID   int    `json:"courseid"`
	CourseName string `json:"coursefullname"`
	AreaName   string `json:"areaname"`
	Content    string `json:"content"`
	Modified   int64  `json:"modified"`
}

// Search performs a full-text search across Moodle content.
// If courseID > 0, results are filtered to that course.
func Search(baseURL, token, query string, courseID int) ([]SearchResult, int, error) {
	params := url.Values{"q": {query}}
	if courseID > 0 {
		params.Set("filters[courseid]", itoa(courseID))
	}
	body, err := CallAPI(baseURL, token, "core_search_get_results", params)
	if err != nil {
		return nil, 0, err
	}
	var resp struct {
		Results    []SearchResult `json:"results"`
		TotalCount int            `json:"totalcount"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, 0, err
	}
	if resp.Results == nil {
		resp.Results = []SearchResult{}
	}
	return resp.Results, resp.TotalCount, nil
}
