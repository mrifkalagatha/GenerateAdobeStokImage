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
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"ai-generator/config"
	"ai-generator/utils"
)

type LeonardoService struct {
	cfg    *config.Config
	client *http.Client
	logger *log.Logger
	mu     sync.Mutex
	keyIdx int
}

type apiError struct {
	StatusCode int
	Body       string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("http %d: %s", e.StatusCode, e.Body)
}

func (e *apiError) IsQuotaOrRateLimit() bool {
	lb := strings.ToLower(e.Body)
	if e.StatusCode == 429 {
		return true
	}
	return strings.Contains(lb, "insufficient_quota") ||
		strings.Contains(lb, "not enough api tokens") ||
		strings.Contains(lb, "api tokens") ||
		strings.Contains(lb, "quota") ||
		strings.Contains(lb, "rate limit") ||
		strings.Contains(lb, "too many requests")
}

func NewLeonardoService(cfg *config.Config, logger *log.Logger) *LeonardoService {
	return &LeonardoService{
		cfg: cfg,
		client: &http.Client{
			Timeout: cfg.HTTPTimeout,
		},
		logger: logger,
	}
}

func (s *LeonardoService) GenerateImage(ctx context.Context, prompt string, count int) ([]GeneratedItem, error) {
	return s.GenerateImageWithOptions(ctx, prompt, count, GenerateOptions{})
}

func (s *LeonardoService) GenerateImageWithOptions(ctx context.Context, prompt string, count int, opts GenerateOptions) ([]GeneratedItem, error) {
	if s.cfg.MockLeonardo {
		utils.LogFlow(s.logger, "[AI]", "MOCK", "generate image prompt=%s count=%d", prompt, count)
		items := make([]GeneratedItem, 0, count)
		for i := 0; i < count; i++ {
			items = append(items, GeneratedItem{
				ImageID: fmt.Sprintf("mock-image-%d", time.Now().UnixNano()),
				URL:     fmt.Sprintf("mock://image/%d_%d.png", time.Now().UnixNano(), i+1),
			})
		}
		return items, nil
	}

	maxBatch := s.cfg.LeonardoMaxBatch
	if maxBatch < 1 {
		maxBatch = 1
	}

	all := make([]GeneratedItem, 0, count)
	remaining := count
	for remaining > 0 {
		batch := remaining
		if batch > maxBatch {
			batch = maxBatch
		}
		utils.LogFlow(s.logger, "[AI]", "AI-GEN", "batch request size=%d remaining=%d", batch, remaining)

		items, err := s.generateImageBatch(ctx, prompt, batch, opts)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		remaining -= len(items)
		if len(items) == 0 {
			return nil, errors.New("leonardo returned zero image assets for batch")
		}
	}

	if len(all) > count {
		return all[:count], nil
	}
	return all, nil
}

func (s *LeonardoService) generateImageBatch(ctx context.Context, prompt string, count int, opts GenerateOptions) ([]GeneratedItem, error) {
	finalPrompt := buildPromptForMode(prompt, opts.PassthroughPrompt, opts.StrictSingleObject)
	width, height := resolveAspectDimensions(opts.AspectRatio, finalPrompt)
	negPrompt := adobeStockNegativePrompt(opts.PassthroughPrompt, opts.StrictSingleObject)
	modelID := strings.TrimSpace(opts.ModelID)
	if modelID == "" {
		modelID = strings.TrimSpace(s.cfg.LeonardoModelID)
	}
	styleUUID := strings.TrimSpace(opts.StyleUUID)
	if styleUUID == "" {
		styleUUID = strings.TrimSpace(s.cfg.LeonardoStyleUUID)
	}
	if s.cfg.LeonardoRequireModelID && modelID == "" {
		return nil, errors.New("model_id is required (set request.model_id or LEONARDO_MODEL_ID)")
	}
	utils.LogFlow(s.logger, "[AI]", "MODEL", "using model_id=%s style_uuid=%s aspect=%dx%d", nonEmptyOr(modelID, "-"), nonEmptyOr(styleUUID, "-"), width, height)

	payloads := []map[string]any{
		s.buildGeneratePayload(finalPrompt, negPrompt, count, width, height, s.cfg.LeonardoDefaultAlchemy, opts),
	}
	if !opts.PassthroughPrompt {
		payloads = append(payloads,
			s.buildGeneratePayload(finalPrompt, negPrompt, count, width, height, false, opts),
			s.buildGeneratePayload(finalPrompt, negPrompt, count, 1280, 720, false, opts),
		)
	}

	var respMap map[string]any
	var err error
	for i, payload := range payloads {
		utils.LogFlow(s.logger, "[AI]", "AI-GEN", "preset[%d] alchemy=%v size=%vx%v", i+1, payload["alchemy"], payload["width"], payload["height"])
		respMap, err = s.callJSONWithRetry(ctx, http.MethodPost, s.cfg.GenerateEndpoint, payload)
		if err == nil {
			break
		}

		var apiErr *apiError
		if errors.As(err, &apiErr) && apiErr.IsQuotaOrRateLimit() && strings.Contains(strings.ToLower(apiErr.Body), "not enough api tokens") {
			utils.LogError(s.logger, "AI-GEN", "preset[%d] too expensive, trying lower-cost preset", i+1)
			continue
		}
		return nil, err
	}
	if err != nil {
		return nil, err
	}

	assets := extractImageAssets(respMap)
	if len(assets) >= count {
		utils.LogSuccess(s.logger, "AI-GEN", "direct assets received=%d", len(assets))
		return assets[:count], nil
	}

	generationID := extractGenerationID(respMap)
	if generationID != "" {
		utils.LogFlow(s.logger, "[AI]", "AI-GEN", "generation id=%s, polling for result", generationID)
		polledAssets, err := s.pollGenerationAssets(ctx, generationID)
		if err != nil {
			return nil, err
		}
		if len(polledAssets) == 0 {
			return nil, errors.New("leonardo generation completed without image assets")
		}
		if len(polledAssets) > count {
			utils.LogSuccess(s.logger, "AI-GEN", "poll complete assets=%d", len(polledAssets))
			return polledAssets[:count], nil
		}
		utils.LogSuccess(s.logger, "AI-GEN", "poll complete assets=%d", len(polledAssets))
		return polledAssets, nil
	}

	if len(assets) == 0 {
		return nil, fmt.Errorf("leonardo returned no image assets and no generation id: %+v", respMap)
	}
	return assets, nil
}

func (s *LeonardoService) buildGeneratePayload(prompt, negativePrompt string, count, width, height int, alchemy bool, opts GenerateOptions) map[string]any {
	payload := map[string]any{
		"alchemy":         alchemy,
		"ultra":           false,
		"width":           width,
		"height":          height,
		"num_images":      count,
		"prompt":          prompt,
		"negative_prompt": negativePrompt,
	}
	styleUUID := strings.TrimSpace(opts.StyleUUID)
	if styleUUID == "" {
		styleUUID = strings.TrimSpace(s.cfg.LeonardoStyleUUID)
	}
	if styleUUID != "" {
		payload["styleUUID"] = styleUUID
	}
	modelID := strings.TrimSpace(opts.ModelID)
	if modelID == "" {
		modelID = strings.TrimSpace(s.cfg.LeonardoModelID)
	}
	if modelID != "" {
		payload["modelId"] = modelID
	}
	if opts.Seed > 0 {
		payload["seed"] = opts.Seed
	}
	return payload
}

func nonEmptyOr(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func (s *LeonardoService) EnhanceImage(ctx context.Context, imageID string, imageURL string) (string, error) {
	if s.cfg.MockLeonardo {
		utils.LogFlow(s.logger, "[AI]", "MOCK", "enhance image id=%s url=%s", imageID, imageURL)
		return fmt.Sprintf("mock://enhanced/%d.png", time.Now().UnixNano()), nil
	}

	if strings.TrimSpace(imageID) == "" {
		return "", errors.New("image ID not found from generation response; unable to call upscale endpoint")
	}

	// Different Leonardo deployments may use different field names.
	// Try common payload shapes until one is accepted.
	payloadCandidates := []map[string]any{
		{"id": imageID},
		{"imageId": imageID},
		{"image_id": imageID},
		{"generatedImageId": imageID},
		{"generated_image_id": imageID},
		{"init_generation_image_id": imageID},
	}

	var lastErr error
	for _, payload := range payloadCandidates {
		payloadKey := firstMapKey(payload)
		utils.LogFlow(s.logger, "[AI]", "AI-UPS", "trying payload key=%s image_id=%s", payloadKey, imageID)

		respMap, err := s.callJSONWithRetry(ctx, http.MethodPost, s.cfg.UpscaleEndpoint, payload)
		if err != nil {
			lastErr = err
			utils.LogError(s.logger, "AI-UPS", "payload key=%s rejected: %v", payloadKey, err)
			continue
		}

		assets := extractImageAssets(respMap)
		if len(assets) > 0 && assets[0].URL != "" {
			utils.LogSuccess(s.logger, "AI-UPS", "enhance completed id=%s", imageID)
			return assets[0].URL, nil
		}

		urls := extractURLs(respMap)
		if len(urls) > 0 {
			utils.LogSuccess(s.logger, "AI-UPS", "enhance completed id=%s", imageID)
			return urls[0], nil
		}

		jobID := extractGenerationID(respMap)
		if jobID != "" {
			utils.LogFlow(s.logger, "[AI]", "AI-UPS", "payload key=%s accepted with async job=%s", payloadKey, jobID)
			polledAssets, err := s.pollGenerationAssets(ctx, jobID)
			if err != nil {
				return "", err
			}
			if len(polledAssets) > 0 && polledAssets[0].URL != "" {
				utils.LogSuccess(s.logger, "AI-UPS", "enhance completed from async job=%s", jobID)
				return polledAssets[0].URL, nil
			}
			return "", fmt.Errorf("upscale job=%s completed without image url", jobID)
		}

		upscaleJobID := extractUpscaleJobID(respMap)
		if upscaleJobID != "" {
			// Leonardo accepted upscale request but did not return final URL yet.
			// To keep the pipeline stable, fallback to original image URL.
			utils.LogFlow(s.logger, "[UPS]", "AI-UPS", "upscale accepted job=%s but result not ready, using original image", upscaleJobID)
			if strings.TrimSpace(imageURL) != "" {
				return imageURL, nil
			}
			return "", nil
		}

		// Accepted payload but no immediate URL and no async job id.
		utils.LogError(s.logger, "AI-UPS", "accepted payload key=%s but no url/job id in response=%+v", payloadKey, respMap)
		return "", fmt.Errorf("upscale accepted but no result fields returned for key=%s", payloadKey)
	}

	if lastErr != nil {
		return "", lastErr
	}
	return "", errors.New("leonardo returned no enhanced image url")
}

func (s *LeonardoService) GenerateVideo(ctx context.Context, prompt string) (string, error) {
	if s.cfg.MockLeonardo {
		utils.LogFlow(s.logger, "[AI]", "MOCK", "generate video prompt=%s", prompt)
		return fmt.Sprintf("mock://video/%d.mp4", time.Now().UnixNano()), nil
	}

	endpoints := uniqueStrings([]string{
		strings.TrimSpace(s.cfg.VideoEndpoint),
		"/generations-text-to-video",
		"/generations-motion-svd",
	})

	payloads := []map[string]any{
		{
			"prompt": prompt,
		},
		{
			"prompt":      prompt,
			"num_videos":  1,
			"width":       1280,
			"height":      720,
			"isPublic":    false,
			"privateMode": true,
		},
	}

	var lastErr error
	for _, endpoint := range endpoints {
		if strings.TrimSpace(endpoint) == "" {
			continue
		}
		for i, payload := range payloads {
			utils.LogFlow(s.logger, "[AI]", "AI-VID", "request endpoint=%s payload_variant=%d", endpoint, i+1)
			respMap, err := s.callJSONWithRetry(ctx, http.MethodPost, endpoint, payload)
			if err != nil {
				var apiErr *apiError
				if errors.As(err, &apiErr) && apiErr.StatusCode == 404 {
					utils.LogError(s.logger, "AI-VID", "endpoint not found=%s, trying fallback", endpoint)
					lastErr = err
					break
				}
				lastErr = err
				continue
			}

			videoURLs := extractVideoURLs(respMap)
			if len(videoURLs) > 0 {
				utils.LogSuccess(s.logger, "AI-VID", "video url received endpoint=%s", endpoint)
				return videoURLs[0], nil
			}

			generationID := extractGenerationID(respMap)
			if generationID != "" {
				utils.LogFlow(s.logger, "[AI]", "AI-VID", "generation id=%s, polling video result", generationID)
				videoURLs, err := s.pollVideoURLs(ctx, generationID)
				if err != nil {
					lastErr = err
					continue
				}
				if len(videoURLs) > 0 {
					utils.LogSuccess(s.logger, "AI-VID", "video url polled endpoint=%s", endpoint)
					return videoURLs[0], nil
				}
			}
			lastErr = errors.New("leonardo returned no video url")
		}
	}

	if lastErr != nil {
		return "", lastErr
	}
	return "", errors.New("leonardo returned no video url")
}

func (s *LeonardoService) callJSONWithRetry(ctx context.Context, method, endpoint string, payload any) (map[string]any, error) {
	keys := s.apiKeys()
	if len(keys) == 0 {
		return nil, errors.New("LEONARDO_API_KEY or LEONARDO_API_KEYS is required")
	}

	var lastErr error
	startIdx := s.currentKeyIndex()
	for attempt := 1; attempt <= s.cfg.RetryCount; attempt++ {
		for k := 0; k < len(keys); k++ {
			idx := (startIdx + k) % len(keys)
			apiKey := keys[idx]

			utils.LogFlow(s.logger, "[AI]", "HTTP", "request attempt=%d key=%d/%d method=%s endpoint=%s", attempt, idx+1, len(keys), method, endpoint)
			respMap, err := s.callJSON(ctx, method, endpoint, payload, apiKey)
			if err == nil {
				s.setKeyIndex(idx)
				utils.LogSuccess(s.logger, "HTTP", "request success endpoint=%s key_index=%d", endpoint, idx+1)
				return respMap, nil
			}

			var apiErr *apiError
			if errors.As(err, &apiErr) {
				if apiErr.IsQuotaOrRateLimit() {
					utils.LogError(s.logger, "HTTP", "key limit reached key_index=%d endpoint=%s switching key", idx+1, endpoint)
					lastErr = err
					continue
				}
				if apiErr.StatusCode >= 400 && apiErr.StatusCode < 500 && apiErr.StatusCode != 408 {
					utils.LogError(s.logger, "HTTP", "non-retryable status=%d endpoint=%s err=%v", apiErr.StatusCode, endpoint, err)
					return nil, err
				}
			}

			lastErr = err
			utils.LogError(s.logger, "HTTP", "request failed attempt=%d endpoint=%s err=%v", attempt, endpoint, err)
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(s.cfg.RetryDelay):
		}
	}
	return nil, fmt.Errorf("leonardo call failed after retries/keys: %w", lastErr)
}

func (s *LeonardoService) callJSON(ctx context.Context, method, endpoint string, payload any, apiKey string) (map[string]any, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	url := s.cfg.LeonardoBaseURL
	if !strings.HasPrefix(endpoint, "http") {
		url = strings.TrimRight(s.cfg.LeonardoBaseURL, "/") + path.Clean("/"+endpoint)
	} else {
		url = endpoint
	}

	var req *http.Request
	if method == http.MethodGet {
		req, err = http.NewRequestWithContext(ctx, method, url, nil)
	} else {
		req, err = http.NewRequestWithContext(ctx, method, url, bytes.NewBuffer(body))
	}
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

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
		return nil, &apiError{
			StatusCode: resp.StatusCode,
			Body:       string(respBody),
		}
	}

	var out map[string]any
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("invalid json response: %w", err)
	}
	return out, nil
}

func (s *LeonardoService) pollGenerationAssets(ctx context.Context, generationID string) ([]GeneratedItem, error) {
	endpoint := strings.ReplaceAll(s.cfg.GenerationGetPath, "{id}", generationID)
	if endpoint == s.cfg.GenerationGetPath {
		endpoint = strings.TrimRight(s.cfg.GenerateEndpoint, "/") + "/" + generationID
	}

	var lastResp map[string]any
	for i := 1; i <= s.cfg.PollAttempts; i++ {
		utils.LogFlow(s.logger, "[AI]", "POLL", "generation=%s attempt=%d/%d", generationID, i, s.cfg.PollAttempts)
		respMap, err := s.callJSONWithRetry(ctx, http.MethodGet, endpoint, map[string]any{})
		if err != nil {
			return nil, fmt.Errorf("poll generation %s failed: %w", generationID, err)
		}
		lastResp = respMap

		assets := extractImageAssets(respMap)
		if len(assets) > 0 {
			utils.LogSuccess(s.logger, "POLL", "generation=%s assets=%d", generationID, len(assets))
			return assets, nil
		}

		status := strings.ToUpper(extractStatus(respMap))
		if status == "FAILED" || status == "ERROR" {
			utils.LogError(s.logger, "POLL", "generation=%s status=%s", generationID, status)
			return nil, fmt.Errorf("leonardo generation failed id=%s status=%s", generationID, status)
		}

		utils.LogFlow(s.logger, "[AI]", "POLL", "generation=%s status=%s waiting=%s", generationID, status, s.cfg.PollInterval)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(s.cfg.PollInterval):
		}
	}

	return nil, fmt.Errorf("poll timeout for generation id=%s last_response=%+v", generationID, lastResp)
}

func (s *LeonardoService) apiKeys() []string {
	if len(s.cfg.LeonardoAPIKeys) > 0 {
		return s.cfg.LeonardoAPIKeys
	}
	if strings.TrimSpace(s.cfg.LeonardoAPIKey) == "" {
		return nil
	}
	return []string{s.cfg.LeonardoAPIKey}
}

func (s *LeonardoService) currentKeyIndex() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.keyIdx
}

func (s *LeonardoService) setKeyIndex(i int) {
	s.mu.Lock()
	s.keyIdx = i
	s.mu.Unlock()
}

func extractGenerationID(data any) string {
	switch v := data.(type) {
	case map[string]any:
		for k, value := range v {
			lk := strings.ToLower(k)
			if lk == "generationid" || lk == "generation_id" {
				if s, ok := value.(string); ok && s != "" {
					return s
				}
			}
			if nested := extractGenerationID(value); nested != "" {
				return nested
			}
		}
	case []any:
		for _, item := range v {
			if nested := extractGenerationID(item); nested != "" {
				return nested
			}
		}
	}
	return ""
}

func extractUpscaleJobID(data any) string {
	switch v := data.(type) {
	case map[string]any:
		if job, ok := v["sdUpscaleJob"]; ok {
			if jm, ok := job.(map[string]any); ok {
				if id, ok := jm["id"].(string); ok && strings.TrimSpace(id) != "" {
					return id
				}
			}
		}
		for _, value := range v {
			if nested := extractUpscaleJobID(value); nested != "" {
				return nested
			}
		}
	case []any:
		for _, item := range v {
			if nested := extractUpscaleJobID(item); nested != "" {
				return nested
			}
		}
	}
	return ""
}

func extractStatus(data any) string {
	switch v := data.(type) {
	case map[string]any:
		for k, value := range v {
			lk := strings.ToLower(k)
			if lk == "status" || lk == "state" {
				if s, ok := value.(string); ok {
					return s
				}
			}
			if nested := extractStatus(value); nested != "" {
				return nested
			}
		}
	case []any:
		for _, item := range v {
			if nested := extractStatus(item); nested != "" {
				return nested
			}
		}
	}
	return ""
}

func extractImageAssets(data any) []GeneratedItem {
	items := make([]GeneratedItem, 0)
	var walk func(v any)

	walk = func(v any) {
		switch val := v.(type) {
		case map[string]any:
			id, hasID := pickString(val, "id", "imageId", "image_id", "generatedImageId", "generated_image_id")
			url, hasURL := pickString(val, "url", "image_url", "imageUrl")
			if hasURL {
				item := GeneratedItem{URL: url}
				if hasID {
					item.ImageID = id
				}
				items = append(items, item)
			}

			for _, inner := range val {
				walk(inner)
			}
		case []any:
			for _, inner := range val {
				walk(inner)
			}
		}
	}

	walk(data)
	items = uniqueAsset(items)
	return items
}

func pickString(m map[string]any, keys ...string) (string, bool) {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return s, true
			}
		}
	}
	return "", false
}

func extractURLs(data any) []string {
	out := make([]string, 0)
	assets := extractImageAssets(data)
	for _, a := range assets {
		if a.URL != "" {
			out = append(out, a.URL)
		}
	}
	return uniqueStrings(out)
}

func extractVideoURLs(data any) []string {
	out := make([]string, 0)
	var walk func(v any)
	walk = func(v any) {
		switch val := v.(type) {
		case map[string]any:
			for k, inner := range val {
				lk := strings.ToLower(k)
				if lk == "url" || lk == "video_url" || lk == "videourl" || lk == "mp4url" || lk == "mp4_url" {
					if s, ok := inner.(string); ok && strings.TrimSpace(s) != "" {
						if isLikelyVideoURL(s) {
							out = append(out, s)
						}
					}
				}
				walk(inner)
			}
		case []any:
			for _, inner := range val {
				walk(inner)
			}
		case string:
			s := strings.TrimSpace(val)
			if s != "" && isLikelyVideoURL(s) {
				out = append(out, s)
			}
		}
	}
	walk(data)
	return uniqueStrings(out)
}

func isLikelyVideoURL(s string) bool {
	raw := strings.TrimSpace(s)
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u == nil {
		return false
	}
	if (u.Scheme != "http" && u.Scheme != "https") || strings.TrimSpace(u.Host) == "" {
		return false
	}
	l := strings.ToLower(raw)
	if strings.Contains(l, ".jpg") || strings.Contains(l, ".jpeg") || strings.Contains(l, ".png") || strings.Contains(l, ".webp") || strings.Contains(l, ".gif") {
		return false
	}
	if strings.Contains(l, "thumbnail") || strings.Contains(l, "preview-image") {
		return false
	}
	return strings.Contains(l, ".mp4") || strings.Contains(l, "/video") || strings.Contains(l, "motion")
}

func (s *LeonardoService) pollVideoURLs(ctx context.Context, generationID string) ([]string, error) {
	endpoint := strings.ReplaceAll(s.cfg.GenerationGetPath, "{id}", generationID)
	if endpoint == s.cfg.GenerationGetPath {
		endpoint = strings.TrimRight(s.cfg.GenerateEndpoint, "/") + "/" + generationID
	}

	var lastResp map[string]any
	for i := 1; i <= s.cfg.PollAttempts; i++ {
		respMap, err := s.callJSONWithRetry(ctx, http.MethodGet, endpoint, map[string]any{})
		if err != nil {
			return nil, fmt.Errorf("poll video %s failed: %w", generationID, err)
		}
		lastResp = respMap

		urls := extractVideoURLs(respMap)
		if len(urls) > 0 {
			return urls, nil
		}

		status := strings.ToUpper(extractStatus(respMap))
		if status == "FAILED" || status == "ERROR" {
			return nil, fmt.Errorf("leonardo video generation failed id=%s status=%s", generationID, status)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(s.cfg.PollInterval):
		}
	}
	return nil, fmt.Errorf("video poll timeout id=%s last_response=%+v", generationID, lastResp)
}

func uniqueAsset(in []GeneratedItem) []GeneratedItem {
	seen := make(map[string]struct{}, len(in))
	out := make([]GeneratedItem, 0, len(in))
	for _, item := range in {
		key := item.ImageID + "|" + item.URL
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func firstMapKey(m map[string]any) string {
	for k := range m {
		return k
	}
	return ""
}

func buildAdobeStockPrompt(base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "commercial stock photo"
	}
	suffix := "adobe stock ready, commercial safe, sharp details, object-focused render, physically plausible material, clean composition, generous copy space, isolated central subject, single floating hero object, premium studio-like setup, dark luxury gradient background, subtle reflective surface, high-detail object silhouette, no humans, no people, no logos, no brands, no text"
	return base + ", " + suffix
}

func adobeStockNegativePrompt(passthroughPrompt bool, strictSingleObject bool) string {
	base := []string{
		"human", "person", "people", "face", "hands", "body",
		"logo", "watermark", "text", "trademark", "brand name",
		"blurry", "noise", "artifacts", "low quality", "jpeg artifacts",
	}
	if strictSingleObject {
		base = append(base, "multiple objects", "object cluster", "duplicated subject", "fragmented subject", "collage", "kaleidoscope", "mosaic")
	}
	if !passthroughPrompt {
		base = append(base, "no subject", "flat 2d graphic", "over-minimal abstract wallpaper")
	}
	return strings.Join(base, ", ")
}

func buildPromptForMode(base string, passthroughPrompt bool, strictSingleObject bool) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return ""
	}
	if passthroughPrompt {
		if !strings.Contains(strings.ToLower(base), "no humans") {
			base += ", no humans"
		}
		if strictSingleObject {
			base += ", single object only, exactly one main subject, no duplicated objects, no object fragments, no cluster"
		}
		return base
	}
	return buildAdobeStockPrompt(base)
}

func resolveAspectDimensions(aspectRatio, prompt string) (int, int) {
	ar := strings.TrimSpace(strings.ToLower(aspectRatio))
	switch ar {
	case "16:9":
		return 1280, 720
	case "1:1":
		return 1024, 1024
	case "4:5":
		return 1024, 1280
	case "9:16":
		return 864, 1536
	case "3:2":
		return 1440, 960
	}

	p := strings.ToLower(prompt)
	switch {
	case strings.Contains(p, "1:1 framing") || strings.Contains(p, "1:1 aspect ratio"):
		return 1024, 1024
	case strings.Contains(p, "4:5 framing") || strings.Contains(p, "4:5 aspect ratio"):
		return 1024, 1280
	case strings.Contains(p, "9:16 framing") || strings.Contains(p, "9:16 aspect ratio"):
		return 864, 1536
	case strings.Contains(p, "3:2 framing") || strings.Contains(p, "3:2 aspect ratio"):
		return 1440, 960
	default:
		return 1280, 720
	}
}
