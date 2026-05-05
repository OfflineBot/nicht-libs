package dualis

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// ErrAuthFailed is returned when Dualis rejects the credentials (wrong password).
// Callers should clear the stored password when they see this error.
var ErrAuthFailed = errors.New("Dualis authentication failed")

// reRefreshURL extracts the redirect URL from a "Refresh: 0; URL=..." header.
var reRefreshURL = regexp.MustCompile(`(?i)URL=(\S+)`)

const baseURL = "https://dualis.dhbw.de/scripts/mgrqispi.dll"

// reToken extracts the session token from a Dualis ARGUMENTS string like
// "-N254097612213167,-N000307,"
var reToken = regexp.MustCompile(`-N(\d{15,})`)

// Session holds an authenticated Dualis HTTP client and the session token.
type Session struct {
	client *http.Client
	Token  string // e.g. "254097612213167"
}

// Login authenticates against Dualis and returns a live Session.
func Login(username, password string) (*Session, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Jar: jar}

	form := url.Values{
		"usrname":   {username},
		"pass":      {password},
		"APPNAME":   {"CampusNet"},
		"PRGNAME":   {"LOGINCHECK"},
		"ARGUMENTS": {"clino,usrname,pass,menuno,menu_type,browser,platform"},
		"clino":     {"000000000000001"},
		"menuno":    {"000324"},
		"menu_type": {"classic"},
		"browser":   {""},
		"platform":  {""},
	}

	resp, err := client.PostForm(baseURL, form)
	if err != nil {
		return nil, fmt.Errorf("login request failed: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("login failed — status %d (check credentials)", resp.StatusCode)
	}

	// Dualis returns 200 with a "Refresh: 0; URL=..." header containing the session token.
	refresh := resp.Header.Get("Refresh")
	if refresh == "" {
		return nil, fmt.Errorf("credentials incorrect: %w", ErrAuthFailed)
	}
	m := reRefreshURL.FindStringSubmatch(refresh)
	if len(m) < 2 {
		return nil, fmt.Errorf("login failed — cannot parse Refresh URL: %s", refresh)
	}
	refreshURL := m[1]
	token := extractToken(refreshURL)
	if token == "" {
		return nil, fmt.Errorf("login failed — no session token in Refresh URL: %s", refreshURL)
	}

	// Follow the redirect URL to establish the session cookie fully.
	if !strings.HasPrefix(refreshURL, "http") {
		refreshURL = "https://dualis.dhbw.de" + refreshURL
	}
	finalResp, err := client.Get(refreshURL)
	if err == nil {
		finalResp.Body.Close()
	}

	return &Session{client: client, Token: token}, nil
}

// GetStudentName fetches the student's first and last name from the Dualis landing page.
// The name is shown in the page heading after login.
func GetStudentName(s *Session) (firstName, lastName string, err error) {
	resp, err := s.get(s.buildURL("STARTSEITE"))
	if err != nil {
		return "", "", fmt.Errorf("could not fetch start page: %w", err)
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("parse error: %w", err)
	}

	// Dualis CampusNet shows the student name in the page heading
	raw := strings.TrimSpace(doc.Find("#pageHeadingText h1").First().Text())
	if raw == "" {
		raw = strings.TrimSpace(doc.Find("h1").First().Text())
	}
	if raw == "" {
		return "", "", fmt.Errorf("name not found in page")
	}

	parts := strings.Fields(raw)
	if len(parts) == 1 {
		return parts[0], "", nil
	}
	return parts[0], strings.Join(parts[1:], " "), nil
}

// buildURL constructs a Dualis URL for the given PRGNAME using the session token.
func (s *Session) buildURL(prgname string, extraArgs ...string) string {
	args := "-N" + s.Token + ",-N000307,"
	if len(extraArgs) > 0 {
		args = strings.Join(extraArgs, ",")
	}
	return fmt.Sprintf("%s?APPNAME=CampusNet&PRGNAME=%s&ARGUMENTS=%s", baseURL, prgname, args)
}

func (s *Session) get(rawURL string) (*http.Response, error) {
	resp, err := s.client.Get(rawURL)
	if err != nil {
		return nil, fmt.Errorf("request to %s failed: %w", rawURL, err)
	}
	return resp, nil
}

func extractToken(s string) string {
	m := reToken.FindStringSubmatch(s)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}
