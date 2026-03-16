package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"ai-generator/config"
	"ai-generator/utils"
)

type ChatGPTPromptService struct {
	cfg    *config.Config
	client *http.Client
	logger *log.Logger
}

func NewChatGPTPromptService(cfg *config.Config, logger *log.Logger) *ChatGPTPromptService {
	return &ChatGPTPromptService{
		cfg: cfg,
		client: &http.Client{
			Timeout: cfg.HTTPTimeout,
		},
		logger: logger,
	}
}

func (s *ChatGPTPromptService) GetLatestNichePrompts(ctx context.Context, niche string, count int, contextHints []string) ([]string, error) {
	niche = strings.TrimSpace(niche)
	if niche == "" {
		return nil, errors.New("niche is required")
	}
	if strings.TrimSpace(s.cfg.OpenAIAPIKey) == "" {
		return nil, errors.New("OPENAI_API_KEY is required to auto-generate niche prompts")
	}
	if count <= 0 {
		count = s.cfg.NichePromptCount
	}
	seasonalCtx := BuildSeasonalContext(s.cfg.SeasonalEventPath, time.Now(), niche)
	hints := strings.TrimSpace(strings.Join(contextHints, ", "))
	if seasonalCtx.Summary != "" {
		if hints != "" {
			hints += ", "
		}
		hints += "seasonal market context: " + seasonalCtx.Summary
	}

	type chatMessage struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	payload := map[string]any{
		"model": s.cfg.OpenAIModel,
		"messages": []chatMessage{
			{
				Role: "system",
				Content: "You are a senior stock photography prompt strategist. " +
					"Return only strict JSON with schema: {\"prompts\":[\"...\"]}. " +
					"Each prompt must be commercial-safe, no logos/text/brands, no humans, Adobe Stock friendly. " +
					"Always align prompts with the requested niche and include weekly high-demand stock search intent. " +
					"When seasonal or international day context is relevant this month, adapt only if it still fits the niche and commercial stock demand. " +
					"For mystical futuristic tech object niches, use a single centered hero object only (crystal core, energy sphere, digital lotus, holographic cube, or tech artifact), isolated premium studio-style composition, dark gradient background, subtle reflective surface, and never literal caves, forests, rocks, mountains, or natural landscapes. " +
					"Never output generic abstract blobs, random primitive shapes, matte clay sculptures, monochrome gray object studies, or non-commercial conceptual experiments.",
			},
			{
				Role: "user",
				Content: fmt.Sprintf(
					"Generate %d prompts for niche: %s. Optimize for the most searched stock topics this week that still match the niche. "+
						"Use current week context date: %s. Additional hints: %s. "+
						"If a relevant international day or monthly seasonal demand is near, translate it into subtle commercial visual concepts instead of literal poster designs. Output JSON only.",
					count,
					niche,
					time.Now().Format("2006-01-02"),
					hints,
				),
			},
		},
		"temperature": 0.7,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	url := strings.TrimRight(s.cfg.OpenAIBaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.OpenAIAPIKey)
	req.Header.Set("Content-Type", "application/json")

	utils.LogFlow(s.logger, "[GPT]", "PROMPT", "requesting niche prompts niche=%s count=%d model=%s", niche, count, s.cfg.OpenAIModel)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai http %d: %s", resp.StatusCode, string(respBody))
	}

	var raw struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, err
	}
	if len(raw.Choices) == 0 {
		return nil, errors.New("openai returned no choices")
	}

	content := strings.TrimSpace(raw.Choices[0].Message.Content)
	if content == "" {
		return nil, errors.New("openai returned empty content")
	}

	type promptResponse struct {
		Prompts []string `json:"prompts"`
	}
	var parsed promptResponse
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return nil, fmt.Errorf("invalid prompt json from openai: %w", err)
	}

	out := make([]string, 0, len(parsed.Prompts))
	seen := map[string]struct{}{}
	for _, p := range parsed.Prompts {
		pp := strings.TrimSpace(p)
		if pp == "" {
			continue
		}
		if _, ok := seen[pp]; ok {
			continue
		}
		seen[pp] = struct{}{}
		out = append(out, pp)
	}

	if len(out) == 0 {
		return nil, errors.New("openai returned no valid prompts")
	}

	out = ApplySeasonalPromptHints(out, seasonalCtx.Themes)

	utils.LogSuccess(s.logger, "PROMPT", "generated niche prompts count=%d", len(out))
	return out, nil
}

func (s *ChatGPTPromptService) GetLatestNichePromptsWithTimeout(niche string, count int, contextHints []string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	return s.GetLatestNichePrompts(ctx, niche, count, contextHints)
}

func (s *ChatGPTPromptService) GenerateAdobeMetadata(ctx context.Context, seeds []MetadataSeed) ([]AdobeStockMetadata, error) {
	if len(seeds) == 0 {
		return nil, nil
	}
	if strings.TrimSpace(s.cfg.OpenAIAPIKey) == "" {
		return nil, errors.New("OPENAI_API_KEY is required for auto metadata")
	}

	type chatMessage struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type requestItem struct {
		Filename string `json:"filename"`
		Prompt   string `json:"prompt"`
		FileType string `json:"file_type"`
	}
	items := make([]requestItem, 0, len(seeds))
	for _, seed := range seeds {
		items = append(items, requestItem{
			Filename: seed.Filename,
			Prompt:   strings.TrimSpace(seed.Prompt),
			FileType: strings.TrimSpace(seed.FileType),
		})
	}
	itemsJSON, err := json.Marshal(items)
	if err != nil {
		return nil, err
	}

	payload := map[string]any{
		"model": s.cfg.OpenAIModel,
		"messages": []chatMessage{
			{
				Role: "system",
				Content: "You are an Adobe Stock metadata specialist. Return strict JSON only with schema: " +
					"{\"items\":[{\"filename\":\"...\",\"title\":\"...\",\"keywords\":[\"...\"],\"category\":\"...\"}]}. " +
					"Rules: title 5-15 words, clear commercial language, no brands, no artist names, no copyrighted characters. " +
					"keywords 15-35 terms, lowercase, comma-safe words, high intent stock search terms. " +
					"Always keep relevance to the prompt. Include no humans intent if applicable. " +
					"Every item must be distinct: avoid repeated title stems and produce varied long-tail keyword sets per file.",
			},
			{
				Role: "user",
				Content: "Generate Adobe Stock metadata for these files. " +
					"Use today's date context " + time.Now().Format("2006-01-02") + ". " +
					"Output JSON only.\n" + string(itemsJSON),
			},
		},
		"temperature": 0.3,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	url := strings.TrimRight(s.cfg.OpenAIBaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.OpenAIAPIKey)
	req.Header.Set("Content-Type", "application/json")

	utils.LogFlow(s.logger, "[GPT]", "META", "requesting adobe metadata items=%d model=%s", len(seeds), s.cfg.OpenAIModel)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai http %d: %s", resp.StatusCode, string(respBody))
	}

	var raw struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, err
	}
	if len(raw.Choices) == 0 {
		return nil, errors.New("openai returned no choices for metadata")
	}
	content := strings.TrimSpace(raw.Choices[0].Message.Content)
	if content == "" {
		return nil, errors.New("openai returned empty metadata content")
	}

	var parsed struct {
		Items []struct {
			Filename string   `json:"filename"`
			Title    string   `json:"title"`
			Keywords []string `json:"keywords"`
			Category string   `json:"category"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return nil, fmt.Errorf("invalid metadata json from openai: %w", err)
	}

	metaByFile := map[string]AdobeStockMetadata{}
	for _, item := range parsed.Items {
		filename := strings.TrimSpace(item.Filename)
		if filename == "" {
			continue
		}
		title := strings.TrimSpace(item.Title)
		if title == "" {
			title = fallbackTitle(filename, "", 0, "")
		}
		keywords := sanitizeKeywords(item.Keywords)
		if len(keywords) == 0 {
			keywords = fallbackKeywords("", 0, "")
		}
		category := strings.TrimSpace(item.Category)
		if category == "" {
			category = "Technology"
		}
		metaByFile[strings.ToLower(filename)] = AdobeStockMetadata{
			Filename: filename,
			Title:    title,
			Keywords: keywords,
			Category: category,
		}
	}

	out := make([]AdobeStockMetadata, 0, len(seeds))
	usedTitles := map[string]int{}
	for i, seed := range seeds {
		key := strings.ToLower(strings.TrimSpace(seed.Filename))
		if meta, ok := metaByFile[key]; ok {
			meta = diversifyMetadata(meta, seed, i, usedTitles)
			out = append(out, meta)
			continue
		}
		fallback := AdobeStockMetadata{
			Filename: seed.Filename,
			Title:    fallbackTitle(seed.Filename, seed.Prompt, i, seed.FileType),
			Keywords: fallbackKeywords(seed.Prompt, i, seed.FileType),
			Category: fallbackCategory(seed.FileType),
		}
		out = append(out, diversifyMetadata(fallback, seed, i, usedTitles))
	}
	utils.LogSuccess(s.logger, "META", "generated metadata items=%d", len(out))
	return out, nil
}

func sanitizeKeywords(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, k := range in {
		kk := strings.ToLower(strings.TrimSpace(k))
		kk = strings.Trim(kk, ",")
		if kk == "" {
			continue
		}
		if _, ok := seen[kk]; ok {
			continue
		}
		seen[kk] = struct{}{}
		out = append(out, kk)
		if len(out) >= 35 {
			break
		}
	}
	return out
}

func fallbackTitle(filename, prompt string, idx int, fileType string) string {
	if strings.TrimSpace(prompt) == "" {
		variants := []string{
			"Futuristic technology background for commercial stock use",
			"Minimal digital gradient backdrop for modern brand campaigns",
			"Abstract premium tech visual with clean copy space",
		}
		return variants[idx%len(variants)]
	}
	words := strings.Fields(strings.TrimSpace(prompt))
	if len(words) > 9 {
		words = words[:9]
	}
	if len(words) == 0 {
		return "Commercial stock media asset"
	}
	suffixes := []string{
		"for website hero banner",
		"for social media ad creative",
		"for modern corporate presentation",
		"for app landing page background",
	}
	if strings.EqualFold(fileType, "video") {
		suffixes = []string{
			"for cinematic stock video usage",
			"for motion background and promo reel",
			"for digital campaign video backdrop",
		}
	}
	return strings.Join(words, " ") + " " + suffixes[idx%len(suffixes)]
}

func fallbackKeywords(prompt string, idx int, fileType string) []string {
	base := []string{"technology", "futuristic", "background", "digital", "abstract", "no humans", "stock"}
	variantPools := [][]string{
		{"copy space", "banner", "hero section", "website", "clean design", "minimal"},
		{"marketing", "branding", "presentation", "corporate", "professional", "template"},
		{"gradient", "cinematic light", "high contrast", "modern", "premium", "editorial"},
		{"landing page", "ad creative", "social media", "campaign", "visual identity", "backdrop"},
	}
	if strings.EqualFold(fileType, "vector") {
		variantPools = append(variantPools, []string{"vector", "illustration", "editable", "scalable", "svg", "graphic resource"})
	}
	if strings.EqualFold(fileType, "video") {
		variantPools = append(variantPools, []string{"motion", "video background", "cinematic footage", "loop", "promo", "timeline"})
	}
	base = append(base, variantPools[idx%len(variantPools)]...)

	for _, w := range strings.Fields(strings.ToLower(prompt)) {
		clean := strings.Trim(w, ",.;:!?()[]{}\"'")
		if clean == "" {
			continue
		}
		base = append(base, clean)
		if len(base) >= 20 {
			break
		}
	}
	return sanitizeKeywords(base)
}

func diversifyMetadata(meta AdobeStockMetadata, seed MetadataSeed, idx int, usedTitles map[string]int) AdobeStockMetadata {
	meta.Filename = seed.Filename
	if strings.TrimSpace(meta.Category) == "" {
		meta.Category = fallbackCategory(seed.FileType)
	}
	if strings.TrimSpace(meta.Title) == "" {
		meta.Title = fallbackTitle(seed.Filename, seed.Prompt, idx, seed.FileType)
	}
	meta.Keywords = sanitizeKeywords(append(meta.Keywords, fallbackKeywords(seed.Prompt, idx, seed.FileType)...))

	title := strings.TrimSpace(meta.Title)
	titleNorm := normalizeTitleKey(title)
	if c := usedTitles[titleNorm]; c > 0 {
		meta.Title = title + fmt.Sprintf(" variation %d", c+1)
	}
	usedTitles[normalizeTitleKey(meta.Title)]++

	if len(meta.Keywords) < 15 {
		meta.Keywords = sanitizeKeywords(append(meta.Keywords, fallbackKeywords(seed.Prompt, idx+3, seed.FileType)...))
	}
	if len(meta.Keywords) > 49 {
		meta.Keywords = meta.Keywords[:49]
	}
	return meta
}

var titleNonWord = regexp.MustCompile(`[^a-z0-9]+`)

func normalizeTitleKey(v string) string {
	s := strings.ToLower(strings.TrimSpace(v))
	s = titleNonWord.ReplaceAllString(s, " ")
	s = strings.Join(strings.Fields(s), " ")
	return s
}

func fallbackCategory(fileType string) string {
	switch strings.ToLower(strings.TrimSpace(fileType)) {
	case "video":
		return "Video"
	case "vector":
		return "Graphic Resources"
	default:
		return "Technology"
	}
}
