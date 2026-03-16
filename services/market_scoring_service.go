package services

import (
	"sort"
	"strings"
)

type PromptScore struct {
	Prompt string `json:"prompt"`
	Score  int    `json:"score"`
	Reason string `json:"reason"`
}

type MarketScoringService struct{}

func NewMarketScoringService() *MarketScoringService {
	return &MarketScoringService{}
}

func (s *MarketScoringService) RankPrompts(niche string, prompts []string, seasonal SeasonalContext) []PromptScore {
	niche = strings.ToLower(strings.TrimSpace(niche))
	out := make([]PromptScore, 0, len(prompts))
	for _, prompt := range prompts {
		pp := strings.TrimSpace(prompt)
		if pp == "" {
			continue
		}
		score, reasons := s.scorePrompt(niche, strings.ToLower(pp), seasonal)
		out = append(out, PromptScore{
			Prompt: pp,
			Score:  score,
			Reason: strings.Join(reasons, "; "),
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].Prompt < out[j].Prompt
		}
		return out[i].Score > out[j].Score
	})
	return out
}

func (s *MarketScoringService) scorePrompt(niche string, prompt string, seasonal SeasonalContext) (int, []string) {
	score := 0
	reasons := make([]string, 0, 8)

	if niche != "" && hasAny(prompt, strings.Fields(niche)...) {
		score += 25
		reasons = append(reasons, "strong niche match")
	}
	if hasAny(prompt, "abstract", "technology", "background", "business", "wellness", "sustainability", "innovation") {
		score += 10
		reasons = append(reasons, "high-demand stock topic")
	}
	if hasAny(prompt, "copy space", "negative space", "clean composition", "center composition", "minimal") {
		score += 12
		reasons = append(reasons, "usable commercial composition")
	}
	if hasAny(prompt, "commercial safe", "adobe stock", "no logo", "no text", "no watermark", "no humans") {
		score += 12
		reasons = append(reasons, "brand-safe stock compliance")
	}
	if hasAny(prompt, "cinematic lighting", "soft glow", "premium", "ultra detailed", "clean render", "studio lighting") {
		score += 10
		reasons = append(reasons, "premium visual quality")
	}
	if hasAny(prompt, "leadership", "wellbeing", "sustainability", "earth", "water", "happiness", "health") {
		score += 8
		reasons = append(reasons, "commercial campaign relevance")
	}
	if seasonal.Summary != "" && hasAny(prompt, seasonal.Themes...) {
		score += 15
		reasons = append(reasons, "seasonal demand fit")
	}
	if hasAny(prompt, "single object center", "symmetrical", "balanced geometric layout", "landscape horizon") {
		score += 8
		reasons = append(reasons, "strong stock composition pattern")
	}
	if hasAny(prompt, "single isolated floating hero object", "premium luxury studio scene", "dark clean gradient backdrop", "subtle reflective surface") {
		score += 18
		reasons = append(reasons, "hero object stock profile match")
	}
	if hasAny(prompt, "crystal core", "energy sphere", "digital lotus", "holographic cube", "tech artifact") {
		score += 12
		reasons = append(reasons, "proven object-family fit")
	}
	if hasAny(prompt, "single centered hero object", "isolated subject", "luxury gradient background", "commercial stock background") {
		score += 10
		reasons = append(reasons, "high-conversion hero object pattern")
	}
	if hasAny(prompt, "forest", "cave", "rocks", "mountain", "terrain", "landscape scene", "stone arch") {
		score -= 35
		reasons = append(reasons, "scene too literal for premium hero stock object")
	}
	if hasAny(prompt, "multiple objects", "crowd", "complex scene", "environment-heavy") {
		score -= 20
		reasons = append(reasons, "composition too busy")
	}
	if hasAny(prompt, "amorphous", "blob", "primitive shape", "matte sculpture", "monochrome gray", "minimalist clay object") {
		score -= 30
		reasons = append(reasons, "low-conversion abstract object style")
	}

	if score == 0 {
		reasons = append(reasons, "base prompt")
	}
	return score, reasons
}

func hasAny(text string, values ...string) bool {
	for _, value := range values {
		v := strings.ToLower(strings.TrimSpace(value))
		if v == "" {
			continue
		}
		if strings.Contains(text, v) {
			return true
		}
	}
	return false
}
