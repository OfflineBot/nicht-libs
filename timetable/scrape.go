package timetable

import (
    "fmt"
    "net/http"
    "regexp"
    "strings"

    "github.com/PuerkitoBio/goquery"
)

var reRoom = regexp.MustCompile(`RV-\w+\s+(.+)`)

type RoomKey struct {
    Weekday int
    Start   string
    End     string
}

func ScrapeRooms(raplaURL string) (map[RoomKey]string, error) {
    resp, err := http.Get(raplaURL)
    if err != nil {
        return nil, fmt.Errorf("scrape fetch error: %w", err)
    }
    defer resp.Body.Close()

    doc, err := goquery.NewDocumentFromReader(resp.Body)
    if err != nil {
        return nil, fmt.Errorf("scrape parse error: %w", err)
    }

    rooms := make(map[RoomKey]string)


    doc.Find("td.week_block").Each(func(i int, s *goquery.Selection) {
        text := strings.TrimSpace(s.Text())

        if m := reRoom.FindStringSubmatch(text); len(m) > 1 {
            room := strings.TrimSpace(m[1])

            timeText := s.Find("a").First().Text()
            times := parseTimeRange(timeText)
            if times == nil {
                return
            }

            col := s.Index()
            rooms[RoomKey{
                Weekday: col,
                Start:   times[0],
                End:     times[1],
            }] = room
        }
    })

    return rooms, nil
}


func parseTimeRange(s string) []string {
	m := reRaplaTime.FindStringSubmatch(s)
	if len(m) < 3 {
		return nil
	}
	return []string{m[1], m[2]}
}
