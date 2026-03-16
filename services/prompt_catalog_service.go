package services

import (
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"
	"time"
)

type PromptCatalogEntry struct {
	ID           int    `json:"id"`
	Title        string `json:"title"`
	Prompt       string `json:"prompt"`
	Niche        string `json:"niche"`
	Composition  string `json:"composition"`
	ColorPalette string `json:"color_palette"`
	Complexity   string `json:"complexity"`
}

type PromptCatalogService struct {
	path         string
	seasonalPath string
}

func NewPromptCatalogService(path string, seasonalPath string) *PromptCatalogService {
	return &PromptCatalogService{
		path:         path,
		seasonalPath: seasonalPath,
	}
}

func (s *PromptCatalogService) GetPrompts(niche string, count int) ([]string, error) {
	if count <= 0 {
		return nil, errors.New("count must be > 0")
	}
	entries, err := s.load()
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, errors.New("prompt catalog is empty")
	}

	nicheTokens := tokenize(niche)
	type scored struct {
		entry PromptCatalogEntry
		score int
	}
	scoredList := make([]scored, 0, len(entries))
	for _, e := range entries {
		score := similarityScore(nicheTokens, tokenize(e.Niche+" "+e.Title+" "+e.Prompt))
		scoredList = append(scoredList, scored{entry: e, score: score})
	}
	sort.SliceStable(scoredList, func(i, j int) bool {
		if scoredList[i].score == scoredList[j].score {
			return scoredList[i].entry.ID < scoredList[j].entry.ID
		}
		return scoredList[i].score > scoredList[j].score
	})

	out := make([]string, 0, count)
	for _, s := range scoredList {
		p := strings.TrimSpace(s.entry.Prompt)
		if p == "" {
			continue
		}
		out = append(out, p)
		if len(out) >= count {
			return out, nil
		}
	}
	// If count is bigger than catalog size, cycle.
	i := 0
	for len(out) < count {
		out = append(out, out[i%len(out)])
		i++
	}
	seasonalCtx := BuildSeasonalContext(s.seasonalPath, time.Now(), niche)
	return ApplySeasonalPromptHints(out, seasonalCtx.Themes), nil
}

func (s *PromptCatalogService) load() ([]PromptCatalogEntry, error) {
	b, err := os.ReadFile(s.path)
	if err != nil {
		return nil, err
	}
	var entries []PromptCatalogEntry
	if err := json.Unmarshal(b, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func tokenize(in string) map[string]struct{} {
	out := map[string]struct{}{}
	fields := strings.Fields(strings.ToLower(in))
	for _, f := range fields {
		f = strings.TrimSpace(strings.Trim(f, ",.;:!?'\"()[]{}"))
		if len(f) < 3 {
			continue
		}
		out[f] = struct{}{}
	}
	return out
}

func similarityScore(a, b map[string]struct{}) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	score := 0
	for k := range a {
		if _, ok := b[k]; ok {
			score++
		}
	}
	return score
}
