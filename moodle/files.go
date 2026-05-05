package moodle

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ErrAuthFailed is returned when the Moodle token is invalid or expired.
// Callers should clear the stored credentials when they see this error.
var ErrAuthFailed = errors.New("Moodle token invalid")

type Course struct {
	ID             int      `json:"id"`
	Shortname      string   `json:"shortname"`
	Fullname       string   `json:"fullname"`
	CategoryName   string   `json:"categoryname"`
	CategoryID     int      `json:"category"`
	StartDate      int64    `json:"startdate"`
	EndDate        int64    `json:"enddate"`
	TimeModified   int64    `json:"timemodified"`
	Hidden         bool     `json:"hidden"`
	IsFavourite    bool     `json:"isfavourite"`
	Visible        int      `json:"visible"`
	Progress       *float64 `json:"progress"`
	Completed      *bool    `json:"completed"`
	LastAccess     *int64   `json:"lastaccess"`
	CourseImage    string   `json:"courseimage"`
	Semester       string   `json:"semester"`        // derived: "WiSe 2024/2025"
	SemesterNumber int      `json:"semester_number"` // derived: 1-6
	IsCurrent      bool     `json:"is_current"`      // derived: enddate==0 or enddate > now
}

type File struct {
	Filename     string `json:"filename"`
	Fileurl      string `json:"fileurl"`
	Filesize     int    `json:"filesize"`
	Mimetype     string `json:"mimetype"`
	TimeCreated  int64  `json:"timecreated"`
	TimeModified int64  `json:"timemodified"`
	Type         string `json:"type"`    // "file" or "content"
	Content      string `json:"content"` // HTML body for type="content" (pages)
}

type Section struct {
	ID      int      `json:"id"`
	Name    string   `json:"name"`
	Summary string   `json:"summary"` // HTML section description
	Modules []Module `json:"modules"`
}

type Module struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Modname     string `json:"modname"`
	Description string `json:"description"` // HTML text for labels/descriptions
	Contents    []File `json:"contents"`
}

func DeriveSemester(startDate int64) string {
	if startDate == 0 {
		return ""
	}
	t := time.Unix(startDate, 0)
	month, year := t.Month(), t.Year()
	if month >= 10 {
		return fmt.Sprintf("WiSe %d/%d", year, year+1)
	}
	if month <= 3 {
		return fmt.Sprintf("WiSe %d/%d", year-1, year)
	}
	return fmt.Sprintf("SoSe %d", year)
}

// DeriveSemesterNumber converts a Moodle category name + course startdate into a
// semester number (1–6). Two naming conventions are supported:
//
//   - DHBW format: "N. Studienjahr" (study year, e.g. "WDS1 - 1. Studienjahr")
//     → combined with startdate to get the exact semester within that year
//     (WiSe = first semester of the year, SoSe = second semester)
//   - Generic format: "N. Semester" / "Semester N"
//     → number taken directly
//
// Returns 0 when no recognisable pattern is found.
func DeriveSemesterNumber(categoryName string, startDate int64) int {
	// DHBW-specific: "N. Studienjahr" → study year 1–3
	if re := regexp.MustCompile(`(\d+)\.\s*Studienjahr`); re.MatchString(categoryName) {
		m := re.FindStringSubmatch(categoryName)
		year, _ := strconv.Atoi(m[1])
		if year < 1 || year > 3 {
			return 0
		}
		// Within the study year, WiSe = odd semester, SoSe = even semester.
		if startDate != 0 {
			month := time.Unix(startDate, 0).Month()
			if month >= 10 || month <= 3 { // Oct–Mar = WiSe
				return 2*(year-1) + 1
			}
			return 2 * year // Apr–Sep = SoSe
		}
		return 2*(year-1) + 1 // default to first semester of the year
	}
	// Generic: "N. Semester" or "Semester N"
	re := regexp.MustCompile(`(\d+)\.\s*[Ss]emester|[Ss]emester\s*(\d+)`)
	m := re.FindStringSubmatch(categoryName)
	if len(m) < 2 {
		return 0
	}
	for _, g := range m[1:] {
		if g != "" {
			n, _ := strconv.Atoi(g)
			if n >= 1 && n <= 6 {
				return n
			}
		}
	}
	return 0
}

// fetchCategoryNames fetches Moodle category names for the given set of category IDs.
// Returns a map from category ID to category name. Missing/inaccessible IDs are silently omitted.
func fetchCategoryNames(baseURL, token string, catIDs map[int]struct{}) map[int]string {
	result := map[int]string{}
	if len(catIDs) == 0 {
		return result
	}
	params := url.Values{}
	i := 0
	for id := range catIDs {
		params.Set(fmt.Sprintf("criteria[%d][key]", i), "id")
		params.Set(fmt.Sprintf("criteria[%d][value]", i), strconv.Itoa(id))
		i++
	}
	data, err := CallAPI(baseURL, token, "core_course_get_categories", params)
	if err != nil {
		return result
	}
	var cats []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	if json.Unmarshal(data, &cats) != nil {
		return result
	}
	for _, c := range cats {
		result[c.ID] = c.Name
	}
	return result
}

func DocTypeFromMime(mime string) string {
	switch {
	case strings.Contains(mime, "pdf"):
		return "pdf"
	case strings.Contains(mime, "presentation") || strings.Contains(mime, "powerpoint"):
		return "pptx"
	case strings.Contains(mime, "word") || strings.Contains(mime, "document"):
		return "docx"
	case strings.HasPrefix(mime, "video/"):
		return "video"
	case strings.HasPrefix(mime, "image/"):
		return "image"
	case strings.Contains(mime, "zip") || strings.Contains(mime, "compressed"):
		return "archive"
	default:
		return "other"
	}
}

// apiClient times out individual Moodle API calls so a hanging connection cannot
// stall a sync indefinitely. 60 s is generous — Moodle endpoints normally answer
// in <2 s; anything beyond a minute is a network/server failure we should retry.
var apiClient = &http.Client{Timeout: 60 * time.Second}

// downloadClient is a separate, longer-timeout client for file downloads.
// 10 minutes covers very large lecture recordings without leaving the door open
// to permanent hangs.
var downloadClient = &http.Client{Timeout: 10 * time.Minute}

func CallAPI(baseURL, token, function string, params url.Values) ([]byte, error) {
	params.Set("wstoken", token)
	params.Set("wsfunction", function)
	params.Set("moodlewsrestformat", "json")

	resp, err := apiClient.PostForm(baseURL+"/webservice/rest/server.php", params)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response failed: %w", err)
	}

	// Moodle returns HTTP 200 even for API-level errors.
	// Parse the error envelope to detect invalid/expired tokens early.
	var apiErr struct {
		Exception string `json:"exception"`
		ErrorCode string `json:"errorcode"`
		Message   string `json:"message"`
	}
	if json.Unmarshal(body, &apiErr) == nil && apiErr.Exception != "" {
		if apiErr.ErrorCode == "invalidtoken" || apiErr.ErrorCode == "accessdenied" {
			return nil, fmt.Errorf("moodle token invalid: %w", ErrAuthFailed)
		}
		return nil, fmt.Errorf("moodle API error (%s): %s", apiErr.ErrorCode, apiErr.Message)
	}

	return body, nil
}

// UserInfo holds the Moodle user profile returned by core_webservice_get_site_info.
type UserInfo struct {
	UserID    int    `json:"userid"`
	FullName  string `json:"fullname"`
	FirstName string `json:"firstname"`
	LastName  string `json:"lastname"`
}

// GetUserInfo fetches the Moodle user profile for the given token.
func GetUserInfo(baseURL, token string) (*UserInfo, error) {
	body, err := CallAPI(baseURL, token, "core_webservice_get_site_info", url.Values{})
	if err != nil {
		return nil, err
	}
	var info UserInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("userinfo parse failed: %w", err)
	}
	return &info, nil
}

// ResolveName returns the user's first and last name from a Moodle UserInfo,
// falling back to splitting Fullname when firstname/lastname are not populated
// separately (common on DHBW Moodle installs).
func (u *UserInfo) ResolveName() (firstName, lastName string) {
	if u == nil {
		return "", ""
	}
	first := strings.TrimSpace(u.FirstName)
	last := strings.TrimSpace(u.LastName)
	if first != "" && last != "" {
		return first, last
	}
	full := strings.TrimSpace(u.FullName)
	if full == "" {
		return first, last
	}
	parts := strings.Fields(full)
	if len(parts) < 2 {
		if first == "" {
			first = full
		}
		return first, last
	}
	if first == "" {
		first = parts[0]
	}
	if last == "" {
		last = strings.Join(parts[1:], " ")
	}
	return first, last
}

func GetCourses(baseURL, token string) ([]Course, error) {
	info, err := GetUserInfo(baseURL, token)
	if err != nil {
		return nil, err
	}

	body, err := CallAPI(baseURL, token, "core_enrol_get_users_courses", url.Values{
		"userid": {strconv.Itoa(info.UserID)},
	})
	if err != nil {
		return nil, err
	}

	var courses []Course
	if err := json.Unmarshal(body, &courses); err != nil {
		return nil, fmt.Errorf("course parse failed: %w", err)
	}

	// Collect unique category IDs so we can resolve their names in one API call.
	catIDs := map[int]struct{}{}
	for _, c := range courses {
		if c.CategoryID != 0 {
			catIDs[c.CategoryID] = struct{}{}
		}
	}
	catNames := fetchCategoryNames(baseURL, token, catIDs)

	now := time.Now().Unix()
	for i := range courses {
		courses[i].Semester = DeriveSemester(courses[i].StartDate)
		courses[i].SemesterNumber = DeriveSemesterNumber(catNames[courses[i].CategoryID], courses[i].StartDate)
		courses[i].IsCurrent = courses[i].EndDate == 0 || courses[i].EndDate > now
	}
	return courses, nil
}

func GetCourseFiles(baseURL, token string, courseID int) ([]File, error) {
	body, err := CallAPI(baseURL, token, "core_course_get_contents", url.Values{
		"courseid": {strconv.Itoa(courseID)},
	})
	if err != nil {
		return nil, err
	}

	var sections []Section
	if err := json.Unmarshal(body, &sections); err != nil {
		return nil, fmt.Errorf("content parse failed: %w", err)
	}

	var files []File
	for _, section := range sections {
		for _, mod := range section.Modules {
			for _, f := range mod.Contents {
				if f.Fileurl != "" {
					files = append(files, f)
				}
			}
		}
	}
	return files, nil
}

// Datei-Inhalt als Stream holen (token wird an URL angehängt)
func DownloadFile(token, fileURL string) (io.ReadCloser, string, error) {
	// Moodle benötigt token als URL-Parameter
	sep := "?"
	for _, c := range fileURL {
		if c == '?' {
			sep = "&"
			break
		}
	}
	fullURL := fileURL + sep + "token=" + token

	resp, err := downloadClient.Get(fullURL)
	if err != nil {
		return nil, "", fmt.Errorf("download failed: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, "", fmt.Errorf("server responds with status %d", resp.StatusCode)
	}
	return resp.Body, resp.Header.Get("Content-Type"), nil
}
