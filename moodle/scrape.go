package moodle

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
)

// ErrWrongPassword is returned by SubmitAttendanceWeb when the session password is incorrect.
var ErrWrongPassword = fmt.Errorf("wrong session password")

func jsonUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

// ScrapedSession is one open attendance session found by scraping the Moodle web UI.
type ScrapedSession struct {
	SessID          string          `json:"sess_id"`
	SessKey         string          `json:"sess_key"`
	CourseName      string          `json:"course_name"`
	CourseID        string          `json:"course_id"` // Moodle course ID (string)
	CMID            string          `json:"cmid"`
	Description     string          `json:"description"`
	RequiresPassword bool           `json:"requires_password"`
	Statuses        []ScrapedStatus `json:"statuses"`
}

// ScrapedStatus is one selectable attendance status (e.g. Anwesend, Verspätet).
type ScrapedStatus struct {
	Value       string `json:"value"`       // e.g. "1209"
	Description string `json:"description"` // e.g. "Anwesend"
}

// moodleClient returns an HTTP client with a cookie jar.
func moodleClient() (*http.Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &http.Client{Jar: jar, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects")
		}
		return nil
	}}, nil
}

// moodleGet performs an authenticated GET request.
func moodleGet(client *http.Client, rawURL string) (string, error) {
	resp, err := client.Get(rawURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// moodlePost performs a POST request with form data.
func moodlePost(client *http.Client, rawURL string, data url.Values) (string, error) {
	resp, err := client.PostForm(rawURL, data)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// WebLogin logs into Moodle via the web form and returns an authenticated client.
func WebLogin(baseURL, username, password string) (*http.Client, error) {
	client, err := moodleClient()
	if err != nil {
		return nil, err
	}

	// Step 1: GET login page for logintoken (CSRF)
	loginPage, err := moodleGet(client, baseURL+"/login/index.php")
	if err != nil {
		return nil, fmt.Errorf("moodle login page: %w", err)
	}
	loginToken := extractAttr(loginPage, `name="logintoken"`, "value")

	// Step 2: POST credentials
	body, err := moodlePost(client, baseURL+"/login/index.php", url.Values{
		"username":   {username},
		"password":   {password},
		"logintoken": {loginToken},
		"anchor":     {""},
	})
	if err != nil {
		return nil, fmt.Errorf("moodle login post: %w", err)
	}
	// Login failed if error message present OR none of the dashboard indicators found
	loggedIn := strings.Contains(body, "Dashboard") ||
		strings.Contains(body, "Abmelden") ||
		strings.Contains(body, "Logout") ||
		strings.Contains(body, "loggedin")
	if strings.Contains(body, "loginerrormessage") || !loggedIn {
		return nil, fmt.Errorf("moodle web login failed — wrong credentials")
	}
	return client, nil
}

// GetOpenAttendanceSessions finds all attendance activities across enrolled courses
// that currently have a session open for student self-marking.
// apiToken is the Moodle mobile API token (for reliable course discovery).
func GetOpenAttendanceSessions(client *http.Client, baseURL, apiToken string) ([]ScrapedSession, error) {
	// Use API to get all enrolled course IDs (more reliable than scraping dashboard)
	courseIDs, err := getCourseIDsViaAPI(baseURL, apiToken)
	if err != nil {
		slog.Warn("moodle attendance: API course discovery failed, falling back to scrape", "err", err)
		// Fallback: scrape dashboard
		courseIDs, err = getCourseIDsViaScrape(client, baseURL)
		if err != nil {
			return nil, err
		}
	}

	var result []ScrapedSession
	for _, courseID := range courseIDs {
		sessions, err := getOpenSessionsForCourse(client, baseURL, courseID)
		if err != nil {
			continue
		}
		result = append(result, sessions...)
	}
	return result, nil
}

// getCourseIDsViaAPI uses the Moodle web service to list enrolled courses.
func getCourseIDsViaAPI(baseURL, token string) ([]string, error) {
	// First get userid
	resp, err := http.PostForm(baseURL+"/webservice/rest/server.php", url.Values{
		"wstoken":             {token},
		"wsfunction":         {"core_webservice_get_site_info"},
		"moodlewsrestformat": {"json"},
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var siteInfo struct {
		UserID int `json:"userid"`
	}
	body, _ := io.ReadAll(resp.Body)
	_ = jsonUnmarshal(body, &siteInfo)
	if siteInfo.UserID == 0 {
		return nil, fmt.Errorf("could not get userid")
	}

	// Get enrolled courses
	resp2, err := http.PostForm(baseURL+"/webservice/rest/server.php", url.Values{
		"wstoken":             {token},
		"wsfunction":         {"core_enrol_get_users_courses"},
		"userid":              {fmt.Sprintf("%d", siteInfo.UserID)},
		"moodlewsrestformat": {"json"},
	})
	if err != nil {
		return nil, err
	}
	defer resp2.Body.Close()
	body2, _ := io.ReadAll(resp2.Body)
	var courses []struct {
		ID int `json:"id"`
	}
	if err := jsonUnmarshal(body2, &courses); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(courses))
	for _, c := range courses {
		ids = append(ids, fmt.Sprintf("%d", c.ID))
	}
	return ids, nil
}

// getCourseIDsViaScrape falls back to scraping the Moodle dashboard.
func getCourseIDsViaScrape(client *http.Client, baseURL string) ([]string, error) {
	body, err := moodleGet(client, baseURL+"/my/courses.php")
	if err != nil {
		return nil, err
	}
	re := regexp.MustCompile(`/course/view\.php\?id=(\d+)`)
	matches := re.FindAllStringSubmatch(body, -1)
	seen := map[string]bool{}
	var ids []string
	for _, m := range matches {
		if !seen[m[1]] {
			seen[m[1]] = true
			ids = append(ids, m[1])
		}
	}
	return ids, nil
}

// getOpenSessionsForCourse checks a single course for open attendance sessions.
func getOpenSessionsForCourse(client *http.Client, baseURL, courseID string) ([]ScrapedSession, error) {
	// Find attendance modules in the course
	body, err := moodleGet(client, baseURL+"/course/view.php?id="+courseID)
	if err != nil {
		return nil, err
	}

	courseName := extractTag(body, "<title")
	if idx := strings.Index(courseName, " | "); idx > 0 {
		courseName = courseName[:idx]
	}

	// Find attendance module links
	reAttend := regexp.MustCompile(`/mod/attendance/view\.php\?id=(\d+)`)
	cmMatches := reAttend.FindAllStringSubmatch(body, -1)

	seenCM := map[string]bool{}
	var result []ScrapedSession
	for _, m := range cmMatches {
		cmid := m[1]
		if seenCM[cmid] {
			continue
		}
		seenCM[cmid] = true

		sessions, err := getOpenSessionsFromAttendancePage(client, baseURL, courseID, courseName, cmid)
		if err != nil {
			continue
		}
		result = append(result, sessions...)
	}
	return result, nil
}

// getOpenSessionsFromAttendancePage loads the attendance view page and finds open sessions.
func getOpenSessionsFromAttendancePage(client *http.Client, baseURL, courseID, courseName, cmid string) ([]ScrapedSession, error) {
	body, err := moodleGet(client, baseURL+"/mod/attendance/view.php?id="+cmid)
	if err != nil {
		return nil, err
	}

	// Find all self-marking links: attendance.php?sessid=NNN&sesskey=XXX
	reSess := regexp.MustCompile(`attendance\.php\?sessid=(\d+)&(?:amp;)?sesskey=([A-Za-z0-9]+)`)
	matches := reSess.FindAllStringSubmatch(body, -1)

	var result []ScrapedSession
	for _, m := range matches {
		sessID, sessKey := m[1], m[2]

		// Fetch the self-marking page to get status options
		markBody, err := moodleGet(client, baseURL+"/mod/attendance/attendance.php?sessid="+sessID+"&sesskey="+sessKey)
		if err != nil {
			continue
		}

		statuses := parseStatusOptions(markBody)
		if len(statuses) == 0 {
			slog.Warn("moodle attendance: no status options parsed — session skipped", "sess_id", sessID, "cmid", cmid)
			continue
		}

		requiresPW := strings.Contains(markBody, `name="studentpassword"`)

		// Find description
		desc := ""
		if idx := strings.Index(body, "sessid="+sessID); idx > 0 {
			snip := body[max(0, idx-500) : idx+500]
			rDesc := regexp.MustCompile(`<div class="text_to_html">(.*?)</div>`)
			if dm := rDesc.FindStringSubmatch(snip); dm != nil {
				desc = strings.TrimSpace(dm[1])
			}
		}

		result = append(result, ScrapedSession{
			SessID:           sessID,
			SessKey:          sessKey,
			CourseName:       courseName,
			CourseID:         courseID,
			CMID:             cmid,
			Description:      desc,
			RequiresPassword: requiresPW,
			Statuses:         statuses,
		})
	}
	return result, nil
}

// parseStatusOptions extracts the status radio button options from the self-marking form.
func parseStatusOptions(html string) []ScrapedStatus {
	// Find each <input name="status"> element, extract its value, then look for the
	// nearest statusdesc span. This is more reliable than a single greedy regex because:
	// - Moodle may add extra CSS classes to the span (e.g. "statusdesc bold")
	// - The gap between the input and its label can exceed 200 chars in newer Moodle themes.
	reInput := regexp.MustCompile(`<input\b[^>]*\bname="status"\b[^>]*>`)
	reValue := regexp.MustCompile(`\bvalue="(\d+)"`)
	reDesc := regexp.MustCompile(`(?i)<span[^>]*class="[^"]*statusdesc[^"]*"[^>]*>(.*?)</span>`)

	var statuses []ScrapedStatus
	for _, loc := range reInput.FindAllStringIndex(html, -1) {
		tag := html[loc[0]:loc[1]]
		vm := reValue.FindStringSubmatch(tag)
		if vm == nil {
			continue
		}
		end := loc[1] + 600
		if end > len(html) {
			end = len(html)
		}
		dm := reDesc.FindStringSubmatch(html[loc[1]:end])
		if dm == nil {
			continue
		}
		statuses = append(statuses, ScrapedStatus{
			Value:       vm[1],
			Description: strings.TrimSpace(dm[1]),
		})
	}
	return statuses
}

// SubmitAttendanceWeb submits the attendance form for a session via web scraping.
// cmid is the course-module ID of the attendance activity (returned by GetOpenAttendanceSessions).
// password is the session password shown by the lecturer (empty string if none required).
func SubmitAttendanceWeb(client *http.Client, baseURL, sessID, cmid, statusValue, password string) error {
	// Load the attendance view page to get a fresh sesskey for this web session.
	viewBody, err := moodleGet(client, baseURL+"/mod/attendance/view.php?id="+cmid)
	if err != nil {
		return fmt.Errorf("attendance view page: %w", err)
	}
	reSess := regexp.MustCompile(`attendance\.php\?sessid=` + regexp.QuoteMeta(sessID) + `&(?:amp;)?sesskey=([A-Za-z0-9]+)`)
	m := reSess.FindStringSubmatch(viewBody)
	if m == nil {
		return fmt.Errorf("session %s not found or no longer open", sessID)
	}
	sessKey := m[1]

	// Load the marking page to get hidden form fields (context ID etc.).
	markBody, err := moodleGet(client, baseURL+"/mod/attendance/attendance.php?sessid="+sessID+"&sesskey="+sessKey)
	if err != nil {
		return fmt.Errorf("attendance mark page: %w", err)
	}
	contextID := extractAttr(markBody, `name="context"`, "value")

	formData := url.Values{
		"sessid":          {sessID},
		"sesskey":         {sessKey},
		"status":          {statusValue},
		"studentpassword": {password},
		"_qf__mod_attendance_form_studentattendance": {"1"},
		"mform_isexpanded_id_session":                {"1"},
		"submitbutton":                               {"Änderungen speichern"},
	}
	if contextID != "" {
		formData.Set("context", contextID)
	}

	body, err := moodlePost(client, baseURL+"/mod/attendance/attendance.php", formData)
	if err != nil {
		return fmt.Errorf("attendance submit: %w", err)
	}
	// On error Moodle re-renders the form with inline field errors.
	if strings.Contains(body, `id_error_studentpassword`) {
		return ErrWrongPassword
	}
	if strings.Contains(body, "alert-danger") {
		// Try to extract the human-readable error text.
		re := regexp.MustCompile(`class="alert-danger[^"]*"[^>]*>([\s\S]{0,300})`)
		if m2 := re.FindStringSubmatch(body); m2 != nil {
			text := regexp.MustCompile(`<[^>]+>`).ReplaceAllString(m2[1], "")
			return fmt.Errorf("attendance submit failed: %s", strings.TrimSpace(text))
		}
		return fmt.Errorf("attendance submit failed: Moodle rejected the request")
	}
	return nil
}

// extractAttr finds the value of an attribute in the first matching tag.
// e.g. extractAttr(html, `name="logintoken"`, "value") → "abc123"
func extractAttr(html, tagFragment, attr string) string {
	idx := strings.Index(html, tagFragment)
	if idx < 0 {
		return ""
	}
	// Find the enclosing tag start
	start := strings.LastIndex(html[:idx], "<")
	// Find the tag end
	end := strings.Index(html[idx:], ">")
	if start < 0 || end < 0 {
		return ""
	}
	tag := html[start : idx+end+1]
	re := regexp.MustCompile(attr + `="([^"]*)"`)
	m := re.FindStringSubmatch(tag)
	if m == nil {
		return ""
	}
	return m[1]
}

// extractTag returns the inner text of the first occurrence of a tag.
func extractTag(html, tag string) string {
	start := strings.Index(html, tag)
	if start < 0 {
		return ""
	}
	start += len(tag)
	// skip to >
	gt := strings.Index(html[start:], ">")
	if gt >= 0 {
		start += gt + 1
	}
	end := strings.Index(html[start:], "<")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(html[start : start+end])
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
