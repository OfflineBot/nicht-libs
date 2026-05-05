package mensa

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// ParseMensaDate converts a German mensa date label like "Mo. 30.03." to an
// ISO date string "2026-03-30".  Returns "" on any parse error.
func ParseMensaDate(label string) string {
	// Split "Mo. 30.03." → ["Mo.", "30.03."]
	parts := strings.Fields(label)
	if len(parts) < 2 {
		return ""
	}
	// "30.03." → strip trailing dot → "30.03" → ["30", "03"]
	dateStr := strings.TrimSuffix(parts[1], ".")
	segs := strings.Split(dateStr, ".")
	if len(segs) != 2 {
		return ""
	}
	day, err1 := strconv.Atoi(segs[0])
	month, err2 := strconv.Atoi(segs[1])
	if err1 != nil || err2 != nil || month < 1 || month > 12 || day < 1 || day > 31 {
		return ""
	}
	now := time.Now()
	year := now.Year()
	t := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.Local)
	// Mensa shows current+next week: if candidate is >6 months in the past it must be next year.
	if now.Sub(t) > 180*24*time.Hour {
		t = time.Date(year+1, time.Month(month), day, 0, 0, 0, 0, time.Local)
	}
	return t.Format("2006-01-02")
}

type Prices struct {
	Students string `json:"studierende"`
	Staff    string `json:"mitarbeiter"`
	Guests   string `json:"gaeste"`
}

type Meal struct {
	Name     string   `json:"name"`
	Category string   `json:"category"`
	Prices   Prices   `json:"preise"`
	Types    []string `json:"typen"` // "vegetarisch", "vegan", "schwein", "rind", "fisch", "geflügel", "fleisch"
}

type DayMenu struct {
	Date  string `json:"datum"` // "Mo. 30.03."
	Meals []Meal `json:"gerichte"`
}

// FetchWeekMenus fetches the mensa page for the given mensa_id and returns
// all days found (up to 10 — current + next week).
func FetchWeekMenus(mensaID string) ([]DayMenu, error) {
	url := fmt.Sprintf("https://seezeit.com/essen/speiseplaene/mensa-%s/", mensaID)

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	// collect dates from tab spans: <a class="tab tab1 ..."><span> Mo. 30.03.</span></a>
	dates := make(map[int]string)
	doc.Find("a.tab").Each(func(_ int, s *goquery.Selection) {
		rel := s.AttrOr("rel", "")
		if rel == "" {
			return
		}
		idx := 0
		fmt.Sscanf(rel, "%d", &idx)
		if idx > 0 {
			dates[idx] = strings.TrimSpace(s.Find("span").Text())
		}
	})

	var days []DayMenu

	// content divs: <div class="contents contents_1 ...">
	reContents := regexp.MustCompile(`contents_(\d+)`)
	doc.Find("div[class*='contents contents_']").Each(func(_ int, s *goquery.Selection) {
		class := s.AttrOr("class", "")
		m := reContents.FindStringSubmatch(class)
		if len(m) < 2 {
			return
		}
		idx := 0
		fmt.Sscanf(m[1], "%d", &idx)
		date := dates[idx]

		var meals []Meal
		s.Find("div.speiseplanTagKat").Each(func(_ int, item *goquery.Selection) {
			category := strings.TrimSpace(item.Find("div.category").Text())
			title := strings.TrimSpace(item.Find("div.title").Text())
			// strip allergen codes in parentheses from title: "(25a,26)"
			title = regexp.MustCompile(`\s*\([^)]+\)`).ReplaceAllString(title, "")
			title = strings.TrimSpace(title)

			if title == "" {
				return
			}

			priceText := strings.TrimSpace(item.Find("div.preise").Text())
			prices := parsePrices(priceText)

			var types []string
			item.Find("div.speiseplanTagKatIcon").Each(func(_ int, icon *goquery.Selection) {
				class := icon.AttrOr("class", "")
				t := mealType(class)
				if t != "" {
					types = append(types, t)
				}
			})
			if len(types) == 0 {
				types = []string{"fleisch"}
			}

			meals = append(meals, Meal{
				Name:     title,
				Category: category,
				Prices:   prices,
				Types:    types,
			})
		})

		if len(meals) > 0 || date != "" {
			days = append(days, DayMenu{Date: date, Meals: meals})
		}
	})

	return days, nil
}

// parsePrices parses "4,40 € Studierende | 5,90 € Mitarbeiter | 8,70 € Gäste"
func parsePrices(s string) Prices {
	re := regexp.MustCompile(`([\d,]+)\s*€\s*(\w+)`)
	matches := re.FindAllStringSubmatch(s, -1)
	p := Prices{}
	for _, m := range matches {
		price := m[1] + " €"
		label := strings.ToLower(m[2])
		switch {
		case strings.HasPrefix(label, "stud"):
			p.Students = price
		case strings.HasPrefix(label, "mit"):
			p.Staff = price
		case strings.HasPrefix(label, "g"):
			p.Guests = price
		}
	}
	return p
}

// mealType maps a speiseplanTagKatIcon CSS class string to a meal type label.
func mealType(class string) string {
	switch {
	case strings.Contains(class, "Vegan"):
		return "vegan"
	case strings.Contains(class, "Veg"):
		return "vegetarisch"
	case strings.Contains(class, "Sch"):
		return "schwein"
	case strings.Contains(class, " R"):
		return "rind"
	case strings.Contains(class, " B"):
		return "fleisch"
	case strings.Contains(class, " F"):
		return "fisch"
	case strings.Contains(class, "Gef"):
		return "geflügel"
	}
	return ""
}
