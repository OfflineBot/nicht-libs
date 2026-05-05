package dualis

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// Exam represents a single exam within a module (top-level module list).
type Exam struct {
	Name   string  `json:"name"`
	Grade  string  `json:"grade"`  // e.g. "1,7" or "bestanden" or "-"
	Status string  `json:"status"` // "passed", "failed", "pending"
	ECTS   float64 `json:"ects"`
}

// ExamComponent is an individual graded sub-component within an exam attempt.
type ExamComponent struct {
	Name  string `json:"name"`
	Score string `json:"score"`
}

// ExamAttempt is a full exam attempt for a module with its sub-components.
// Attempt is the 1-based attempt number within the module (1 = first attempt,
// 2 = Wiederholung, 3 = mündliche Nachprüfung etc.).
type ExamAttempt struct {
	Attempt    int             `json:"attempt"`
	Semester   string          `json:"semester"`
	Name       string          `json:"name"`
	Grade      string          `json:"grade"`
	Status     string          `json:"status"`
	Components []ExamComponent `json:"components"`
}

// Module represents a university module with its exams.
type Module struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	Grade      string        `json:"grade"`
	Status     string        `json:"status"`
	ECTS       float64       `json:"ects"`
	DetailArgs string        `json:"detail_args,omitempty"` // token-independent args for RESULTDETAILS
	Exams      []Exam        `json:"exams"`
	Attempts   []ExamAttempt `json:"attempts,omitempty"` // populated by enrichment when module has no final grade
	// Failed is true when Dualis shows a registered Wiederholungsprüfung
	// (≥2 Versuche) and no attempt so far is passed. Implies the student is
	// awaiting / has to take a retry. Frontend can use this to highlight the
	// module before the official module-level grade is updated.
	Failed bool `json:"failed,omitempty"`
	// AttemptsTotal is the count of attempts visible in Dualis (1 = first
	// attempt only, 2 = retry registered, 3 = mündliche Nachprüfung).
	AttemptsTotal int `json:"attempts_total,omitempty"`
	// Stale=true when the module was previously seen on Dualis but is missing
	// in the most recent fetch. The stored grade is shown until the module
	// reappears (then stale clears, and a differing grade replaces the value).
	Stale bool `json:"stale,omitempty"`
}

// Semester identifies a Dualis semester.
type Semester struct {
	ID   string `json:"id"`   // e.g. "000000015178000"
	Name string `json:"name"` // e.g. "SoSe 2026"
}

// SemesterGrades holds all module grades for one semester.
type SemesterGrades struct {
	Semester     Semester `json:"semester"`
	Modules      []Module `json:"modules"`
	AverageGrade string   `json:"average_grade"`
	TotalECTS    float64  `json:"total_ects"`
}

// AllGradesResult holds grades across all semesters with overall averages.
type AllGradesResult struct {
	Semesters    []SemesterGrades `json:"semesters"`
	AverageGrade string           `json:"average_grade"`
	TotalECTS    float64          `json:"total_ects"`
}

var reDetailArgs = regexp.MustCompile(`ARGUMENTS=([^"&]+)`)

// GetSemesters fetches the list of available semesters from the COURSERESULTS page.
func GetSemesters(s *Session) ([]Semester, error) {
	resp, err := s.get(s.buildURL("COURSERESULTS"))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	var semesters []Semester
	doc.Find("select#semester option").Each(func(_ int, opt *goquery.Selection) {
		val, exists := opt.Attr("value")
		if !exists || val == "" {
			return
		}
		semesters = append(semesters, Semester{
			ID:   val,
			Name: strings.TrimSpace(opt.Text()),
		})
	})
	return semesters, nil
}

// GetGradesBySemester fetches module grades for a specific semester ID.
func GetGradesBySemester(s *Session, semesterID string) ([]Module, error) {
	resp, err := s.client.PostForm(baseURL, url.Values{
		"APPNAME":   {"CampusNet"},
		"PRGNAME":   {"COURSERESULTS"},
		"ARGUMENTS": {"sessionno,menuno,semester"},
		"sessionno": {s.Token},
		"menuno":    {"000307"},
		"semester":  {semesterID},
	})
	if err != nil {
		return nil, fmt.Errorf("fetch semester %s failed: %w", semesterID, err)
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	return parseModuleRows(doc, s.Token), nil
}

// GetAllGrades fetches grades for all available semesters.
func GetAllGrades(s *Session) (*AllGradesResult, error) {
	semesters, err := GetSemesters(s)
	if err != nil {
		return nil, fmt.Errorf("could not list semesters: %w", err)
	}

	result := &AllGradesResult{}
	var allModules []Module

	for _, sem := range semesters {
		modules, err := GetGradesBySemester(s, sem.ID)
		if err != nil {
			return nil, fmt.Errorf("semester %s: %w", sem.Name, err)
		}

		var semECTS float64
		for _, m := range modules {
			semECTS += m.ECTS
		}
		allModules = append(allModules, modules...)

		result.Semesters = append(result.Semesters, SemesterGrades{
			Semester:     sem,
			Modules:      modules,
			AverageGrade: computeAverage(modules),
			TotalECTS:    semECTS,
		})
		result.TotalECTS += semECTS
	}

	result.AverageGrade = computeAverage(allModules)
	return result, nil
}

// GetGradeDetails fetches detailed exam attempts for a specific module.
// detailArgs is the token-independent part: "-N{moduleRef},-N{semesterID}".
func GetGradeDetails(s *Session, detailArgs string) ([]ExamAttempt, error) {
	args := fmt.Sprintf("-N%s,-N000307,%s", s.Token, detailArgs)
	rawURL := fmt.Sprintf("%s?APPNAME=CampusNet&PRGNAME=RESULTDETAILS&ARGUMENTS=%s", baseURL, args)
	resp, err := s.get(rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	return parseExamAttempts(doc), nil
}

// parseExamAttempts extracts exam attempts from a RESULTDETAILS page.
//
// Page structure (each tr):
//   - tbdata colspan=2 → exam header row: col0=semester, col1=exam name, col2=date, col3=grade
//   - tbdata colspan=2 (empty col0) → sub-component: col1=name, col3=score
//   - level02 row with "Gesamt N" in col1 → col3 has final "1,0 bestanden"
func parseExamAttempts(doc *goquery.Document) []ExamAttempt {
	var attempts []ExamAttempt
	var current *ExamAttempt

	doc.Find("tr").Each(func(_ int, row *goquery.Selection) {
		cells := row.Find("td")
		if cells.Length() == 0 {
			return
		}

		firstCell := cells.Eq(0)
		firstClass := firstCell.AttrOr("class", "")
		firstText := strings.TrimSpace(firstCell.Text())

		col := func(i int) string {
			if i >= cells.Length() {
				return ""
			}
			return strings.TrimSpace(cells.Eq(i).Text())
		}

		// Exam header row: tbdata colspan=2 with non-empty semester text
		if strings.Contains(firstClass, "tbdata") {
			colspan := firstCell.AttrOr("colspan", "")
			if colspan == "2" && firstText != "" && firstText != "\u00a0" {
				// New exam attempt
				attempt := ExamAttempt{
					Attempt:    len(attempts) + 1,
					Semester:   firstText,
					Name:       col(1),
					Grade:      col(3),
					Status:     parseStatus(col(3)),
					Components: []ExamComponent{},
				}
				attempts = append(attempts, attempt)
				current = &attempts[len(attempts)-1]
				return
			}

			// Sub-component row: tbdata colspan=2 with empty col0, non-empty col1
			if colspan == "2" && (firstText == "" || firstText == "\u00a0") && current != nil {
				name := strings.TrimSpace(col(1))
				score := col(3)
				if name != "" && name != "\u00a0" {
					current.Components = append(current.Components, ExamComponent{
						Name:  strings.TrimLeft(name, "\u00a0 "),
						Score: score,
					})
				}
				return
			}
		}

		// level02 "Gesamt N" row: overwrite grade with final grade string
		if strings.Contains(firstClass, "level02") && current != nil {
			label := col(1)
			if strings.HasPrefix(label, "Gesamt") {
				gradeText := col(3)
				// "1,0 bestanden" → split grade and status
				parts := strings.SplitN(strings.ReplaceAll(gradeText, "\u00a0", " "), " ", 2)
				current.Grade = parts[0]
				if len(parts) > 1 {
					current.Status = parseStatus(parts[1])
				} else {
					current.Status = parseStatus(current.Grade)
				}
				current = nil // done with this attempt
			}
		}
	})

	return attempts
}

// parseModuleRows extracts Module entries from a COURSERESULTS HTML document.
func parseModuleRows(doc *goquery.Document, token string) []Module {
	var modules []Module

	doc.Find("tr").Each(func(_ int, row *goquery.Selection) {
		cells := row.Find("td")
		if cells.Length() < 5 {
			return
		}

		col := func(i int) string {
			return strings.TrimSpace(cells.Eq(i).Text())
		}

		id := col(0)
		name := col(1)
		if id == "" && name == "" {
			return
		}
		// Skip header rows (tbsubhead class)
		if cells.Eq(0).HasClass("tbsubhead") {
			return
		}

		grade := col(2)
		credits := parseCredits(col(3))
		status := deriveStatus(grade, col(4))

		// Extract token-independent detail args from the "Prüfungen" link
		detailArgs := ""
		cells.Each(func(_ int, cell *goquery.Selection) {
			cell.Find("a[href*=RESULTDETAILS]").Each(func(_ int, a *goquery.Selection) {
				href, _ := a.Attr("href")
				if m := reDetailArgs.FindStringSubmatch(href); len(m) >= 2 {
					// Full args: -N{token},-N000307,-N{moduleRef},-N{semesterID}
					// Strip the first two token-bound segments
					parts := strings.Split(m[1], ",")
					if len(parts) >= 3 {
						// Keep from index 2 onward (module ref + semester id)
						detailArgs = strings.Join(parts[2:], ",")
					}
				}
			})
		})

		modules = append(modules, Module{
			ID:         id,
			Name:       name,
			Grade:      grade,
			Status:     status,
			ECTS:       credits,
			DetailArgs: detailArgs,
			Exams:      []Exam{},
		})
	})

	return modules
}

func parseCredits(s string) float64 {
	s = strings.ReplaceAll(s, ",", ".")
	var v float64
	fmt.Sscanf(s, "%f", &v)
	return v
}

func parseStatus(s string) string {
	lower := strings.ToLower(strings.TrimSpace(s))
	switch {
	case strings.Contains(lower, "nicht bestanden") || strings.Contains(lower, "failed"):
		return "failed"
	case strings.Contains(lower, "bestanden") || strings.Contains(lower, "passed"):
		return "passed"
	default:
		return "pending"
	}
}

// deriveStatus combines the textual status column with the numeric grade.
// If the text is unambiguous (passed/failed) it wins; otherwise we fall back
// to the grade so a 4,5 without status text is still recognised as failed.
func deriveStatus(grade, statusText string) string {
	if s := parseStatus(statusText); s != "pending" {
		return s
	}
	g := strings.ReplaceAll(strings.TrimSpace(grade), ",", ".")
	var v float64
	if _, err := fmt.Sscanf(g, "%f", &v); err == nil {
		if v >= 4.05 {
			return "failed"
		}
		if v >= 1.0 && v <= 4.0 {
			return "passed"
		}
	}
	return "pending"
}

func computeAverage(modules []Module) string {
	var sum, count float64
	for _, m := range modules {
		g := strings.ReplaceAll(m.Grade, ",", ".")
		var v float64
		if _, err := fmt.Sscanf(g, "%f", &v); err == nil && v >= 1.0 && v <= 5.0 {
			sum += v
			count++
		}
	}
	if count == 0 {
		return "-"
	}
	return fmt.Sprintf("%.2f", sum/count)
}
