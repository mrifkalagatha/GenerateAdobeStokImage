package services

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

type SeasonalEvent struct {
	Name        string   `json:"name"`
	Month       int      `json:"month"`
	Day         int      `json:"day"`
	WindowDays  int      `json:"window_days"`
	Themes      []string `json:"themes"`
	PromptCues  []string `json:"prompt_cues"`
	Keywords    []string `json:"keywords"`
	StockAngles []string `json:"stock_angles"`
}

type SeasonalContext struct {
	Summary string
	Themes  []string
}

func BuildSeasonalContext(path string, now time.Time, niche string) SeasonalContext {
	events, err := loadSeasonalEvents(path)
	if err != nil || len(events) == 0 {
		return SeasonalContext{}
	}

	type matched struct {
		event SeasonalEvent
		score int
		diff  int
	}

	nicheLower := strings.ToLower(strings.TrimSpace(niche))
	var matches []matched
	for _, event := range events {
		eventDate := time.Date(now.Year(), time.Month(event.Month), event.Day, 0, 0, 0, 0, now.Location())
		diff := int(eventDate.Sub(time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())).Hours() / 24)
		if diff < -3 {
			continue
		}
		window := event.WindowDays
		if window <= 0 {
			window = 21
		}
		if diff > window {
			continue
		}

		score := 1
		for _, kw := range event.Keywords {
			if kw != "" && strings.Contains(nicheLower, strings.ToLower(kw)) {
				score += 3
			}
		}
		if isAbstractCommercialNiche(nicheLower) {
			score += 2
		}

		matches = append(matches, matched{
			event: event,
			score: score,
			diff:  diff,
		})
	}

	if len(matches) == 0 {
		return SeasonalContext{}
	}

	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score == matches[j].score {
			return matches[i].diff < matches[j].diff
		}
		return matches[i].score > matches[j].score
	})

	limit := 2
	if len(matches) < limit {
		limit = len(matches)
	}

	summaries := make([]string, 0, limit)
	themes := make([]string, 0, limit*4)
	for _, match := range matches[:limit] {
		ev := match.event
		summaries = append(summaries, fmt.Sprintf(
			"%s on %s with themes [%s] and stock angles [%s]",
			ev.Name,
			time.Date(now.Year(), time.Month(ev.Month), ev.Day, 0, 0, 0, 0, now.Location()).Format("2006-01-02"),
			strings.Join(ev.Themes, ", "),
			strings.Join(ev.StockAngles, ", "),
		))
		themes = append(themes, ev.PromptCues...)
	}

	return SeasonalContext{
		Summary: strings.Join(summaries, "; "),
		Themes:  uniqueSeasonalStrings(themes),
	}
}

func loadSeasonalEvents(path string) ([]SeasonalEvent, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("seasonal event path is empty")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var events []SeasonalEvent
	if err := json.Unmarshal(b, &events); err != nil {
		return nil, err
	}
	return events, nil
}

func isAbstractCommercialNiche(niche string) bool {
	return strings.Contains(niche, "abstract") ||
		strings.Contains(niche, "technology") ||
		strings.Contains(niche, "background") ||
		strings.Contains(niche, "business") ||
		strings.Contains(niche, "wellness") ||
		strings.Contains(niche, "sustainability")
}

func uniqueSeasonalStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func ApplySeasonalPromptHints(prompts []string, themes []string) []string {
	if len(themes) == 0 || len(prompts) == 0 {
		return prompts
	}
	out := make([]string, 0, len(prompts))
	for i, p := range prompts {
		pp := strings.TrimSpace(p)
		if pp == "" {
			continue
		}
		theme := themes[i%len(themes)]
		out = append(out, pp+", "+theme)
	}
	return out
}
