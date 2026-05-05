package moodle

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Message represents a Moodle direct message.
type Message struct {
	ID               int    `json:"id"`
	UserIDFrom       int    `json:"useridfrom"`
	UserFromFullName string `json:"userfromfullname"`
	Text             string `json:"text"`
	SmallMessage     string `json:"smallmessage"`
	IsRead           bool   `json:"isread"`
	TimeSent         int64  `json:"timecreated"`
	ContextURL       string `json:"contexturl"`
}

// Notification represents a Moodle popup notification.
type Notification struct {
	ID              int    `json:"id"`
	UserIDFrom      int    `json:"useridfrom"`
	Subject         string `json:"subject"`
	Text            string `json:"text"`
	FullMessage     string `json:"fullmessage"`
	FullMessageHTML string `json:"fullmessagehtml"`
	IsRead          bool   `json:"isread"`
	TimeCreated     int64  `json:"timecreated"`
	IconURL         string `json:"iconurl"`
	ContextURL      string `json:"contexturl"`
	ContextURLName  string `json:"contexturlname"`
}

// NotificationsResult is the response from message_popup_get_popup_notifications.
type NotificationsResult struct {
	Notifications []Notification `json:"notifications"`
	UnreadCount   int            `json:"unreadcount"`
}

// Announcement is a forum news post.
type Announcement struct {
	ID         int    `json:"id"`
	Subject    string `json:"subject"`
	Message    string `json:"message"`
	Author     string `json:"author"`
	CourseName string `json:"course_name"`
	Created    int64  `json:"created"`
}

// RecentActivity represents a recently accessed item.
type RecentActivity struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	CourseID   int    `json:"courseid"`
	TimeAccess int64  `json:"timeaccess"`
	URL        string `json:"url,omitempty"`
}

// PrivateFile represents a file in the user's private files area.
type PrivateFile struct {
	Filename string `json:"filename"`
	FileURL  string `json:"fileurl"`
	Filesize int    `json:"filesize"`
	MimeType string `json:"mimetype"`
	Modified int64  `json:"timemodified"`
}

// CalendarEvent represents a Moodle calendar event.
type CalendarEvent struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	CourseID     int    `json:"courseid"`
	GroupID      int    `json:"groupid"`
	EventType    string `json:"eventtype"`
	TimeStart    int64  `json:"timestart"`
	TimeDuration int64  `json:"timeduration"`
	Visible      int    `json:"visible"`
}

// GetMessages fetches direct messages for a user.
func GetMessages(baseURL, token string, moodleUserID int) ([]Message, error) {
	data, err := CallAPI(baseURL, token, "core_message_get_messages", url.Values{
		"useridto":    {strconv.Itoa(moodleUserID)},
		"type":        {"conversations"},
		"newestfirst": {"1"},
	})
	if err != nil {
		return nil, err
	}
	var result struct {
		Messages []Message `json:"messages"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("messages parse failed: %w", err)
	}
	if result.Messages == nil {
		return []Message{}, nil
	}
	for i := range result.Messages {
		if result.Messages[i].ContextURL == "" {
			result.Messages[i].ContextURL = fmt.Sprintf("%s/message/index.php?id=%d", baseURL, result.Messages[i].UserIDFrom)
		}
	}
	return result.Messages, nil
}

// GetNotifications fetches popup notifications for a user.
func GetNotifications(baseURL, token string, moodleUserID int) (*NotificationsResult, error) {
	data, err := CallAPI(baseURL, token, "message_popup_get_popup_notifications", url.Values{
		"useridto":    {strconv.Itoa(moodleUserID)},
		"newestfirst": {"1"},
	})
	if err != nil {
		return nil, err
	}
	var result NotificationsResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("notifications parse failed: %w", err)
	}
	if result.Notifications == nil {
		result.Notifications = []Notification{}
	}
	return &result, nil
}

// GetAnnouncements fetches news forum posts across the given courses.
func GetAnnouncements(baseURL, token string, courseIDs []int) ([]Announcement, error) {
	vals := url.Values{}
	for i, id := range courseIDs {
		vals.Set(fmt.Sprintf("courseids[%d]", i), strconv.Itoa(id))
	}
	data, err := CallAPI(baseURL, token, "mod_forum_get_forums_by_courses", vals)
	if err != nil {
		return nil, err
	}
	var forums []struct {
		ID         int    `json:"id"`
		CourseName string `json:"coursefullname"`
		Type       string `json:"type"`
	}
	if err := json.Unmarshal(data, &forums); err != nil {
		return nil, fmt.Errorf("forums parse failed: %w", err)
	}

	var announcements []Announcement
	for _, f := range forums {
		if f.Type != "news" {
			continue
		}
		discData, err := CallAPI(baseURL, token, "mod_forum_get_forum_discussions", url.Values{
			"forumid": {strconv.Itoa(f.ID)},
		})
		if err != nil {
			continue
		}
		var discResult struct {
			Discussions []struct {
				ID           int    `json:"id"`
				Subject      string `json:"subject"`
				Message      string `json:"message"`
				UserFullName string `json:"userfullname"`
				Created      int64  `json:"created"`
			} `json:"discussions"`
		}
		if err := json.Unmarshal(discData, &discResult); err != nil {
			continue
		}
		for _, d := range discResult.Discussions {
			announcements = append(announcements, Announcement{
				ID:         d.ID,
				Subject:    d.Subject,
				Message:    d.Message,
				Author:     d.UserFullName,
				CourseName: f.CourseName,
				Created:    d.Created,
			})
		}
	}
	if announcements == nil {
		announcements = []Announcement{}
	}
	return announcements, nil
}

// GetRecentActivities fetches recently accessed course items.
func GetRecentActivities(baseURL, token string) ([]RecentActivity, error) {
	data, err := CallAPI(baseURL, token, "core_block_recentlyaccesseditems_get_recent_items", url.Values{})
	if err != nil {
		return nil, err
	}
	var items []struct {
		ID         int    `json:"id"`
		Name       string `json:"name"`
		Type       string `json:"type"`
		CourseID   int    `json:"courseid"`
		ViewURL    string `json:"viewurl"`
		TimeAccess int64  `json:"timeaccess"`
	}
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("recent activities parse failed: %w", err)
	}
	result := make([]RecentActivity, 0, len(items))
	for _, it := range items {
		result = append(result, RecentActivity{
			ID:         it.ID,
			Name:       it.Name,
			Type:       it.Type,
			CourseID:   it.CourseID,
			TimeAccess: it.TimeAccess,
			URL:        it.ViewURL,
		})
	}
	return result, nil
}

// GetPrivateFiles fetches the user's private files from Moodle.
func GetPrivateFiles(baseURL, token string, moodleUserID int) ([]PrivateFile, error) {
	data, err := CallAPI(baseURL, token, "core_files_get_files", url.Values{
		"contextlevel": {"user"},
		"instanceid":   {strconv.Itoa(moodleUserID)},
		"component":    {"user"},
		"filearea":     {"private"},
		"filepath":     {"/"},
	})
	if err != nil {
		return nil, err
	}
	var result struct {
		Files []struct {
			Filename string `json:"filename"`
			FileURL  string `json:"fileurl"`
			Filesize int    `json:"filesize"`
			MimeType string `json:"mimetype"`
			Modified int64  `json:"timemodified"`
		} `json:"files"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("private files parse failed: %w", err)
	}
	out := make([]PrivateFile, 0, len(result.Files))
	for _, f := range result.Files {
		out = append(out, PrivateFile{
			Filename: f.Filename,
			FileURL:  f.FileURL,
			Filesize: f.Filesize,
			MimeType: f.MimeType,
			Modified: f.Modified,
		})
	}
	return out, nil
}

// GetCalendarEvents fetches upcoming calendar events for the current user.
func GetCalendarEvents(baseURL, token string) ([]CalendarEvent, error) {
	data, err := CallAPI(baseURL, token, "core_calendar_get_calendar_events", url.Values{
		"options[userevents]":  {"1"},
		"options[siteevents]":  {"1"},
		"options[groupevents]": {"1"},
	})
	if err != nil {
		return nil, err
	}
	var result struct {
		Events []CalendarEvent `json:"events"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("calendar events parse failed: %w", err)
	}
	if result.Events == nil {
		result.Events = []CalendarEvent{}
	}
	return result.Events, nil
}

// FetchImageBytes downloads a Moodle pluginfile URL using the given token.
// It rewrites /pluginfile.php/ → /webservice/pluginfile.php/ so the token
// is accepted by the webservice endpoint.
func FetchImageBytes(token, rawURL string) ([]byte, string, error) {
	imageURL := strings.Replace(rawURL, "/pluginfile.php/", "/webservice/pluginfile.php/", 1)
	sep := "?"
	if strings.Contains(imageURL, "?") {
		sep = "&"
	}
	resp, err := downloadClient.Get(imageURL + sep + "token=" + token)
	if err != nil {
		return nil, "", fmt.Errorf("image fetch failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("image server returned %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	imgData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("image read failed: %w", err)
	}
	return imgData, ct, nil
}

// MarkMessageRead marks a single direct message as read via the Moodle API.
func MarkMessageRead(baseURL, token string, messageID int) error {
	_, err := CallAPI(baseURL, token, "core_message_mark_message_read", url.Values{
		"messageid": {strconv.Itoa(messageID)},
		"timeread":  {strconv.FormatInt(time.Now().Unix(), 10)},
	})
	return err
}

// GetCourseImageURL fetches the overview image URL for a single course.
// Uses core_enrol_get_users_courses (same endpoint as GetCourses) and filters
// by courseID, then reads overviewfiles[0].fileurl.
// Returns empty string when no image is available.
func GetCourseImageURL(baseURL, token string, courseID int) (string, error) {
	info, err := GetUserInfo(baseURL, token)
	if err != nil {
		return "", err
	}
	data, err := CallAPI(baseURL, token, "core_enrol_get_users_courses", url.Values{
		"userid": {strconv.Itoa(info.UserID)},
	})
	if err != nil {
		return "", err
	}
	var courses []struct {
		ID            int    `json:"id"`
		OverviewFiles []struct {
			FileURL string `json:"fileurl"`
		} `json:"overviewfiles"`
		CourseImage string `json:"courseimage"`
	}
	if err := json.Unmarshal(data, &courses); err != nil {
		return "", fmt.Errorf("course list parse failed: %w", err)
	}
	for _, c := range courses {
		if c.ID != courseID {
			continue
		}
		if len(c.OverviewFiles) > 0 && c.OverviewFiles[0].FileURL != "" {
			return c.OverviewFiles[0].FileURL, nil
		}
		if c.CourseImage != "" {
			return c.CourseImage, nil
		}
		return "", fmt.Errorf("no course image found")
	}
	return "", fmt.Errorf("course %d not found in enrollment list", courseID)
}

// GetCourseImage proxies the course overview image. Returns image bytes and content-type.
// It first resolves the URL via GetCourseImageURL, then downloads it.
func GetCourseImage(baseURL, token string, courseID int) ([]byte, string, error) {
	imageURL, err := GetCourseImageURL(baseURL, token, courseID)
	if err != nil {
		return nil, "", err
	}
	return FetchImageBytes(token, imageURL)
}
