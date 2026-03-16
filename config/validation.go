package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func (c *Config) Validate() error {
	var errs []string

	if err := validatePort(c.Port); err != nil {
		errs = append(errs, err.Error())
	}

	if err := ensureDirectory("storage", c.StoragePath); err != nil {
		errs = append(errs, err.Error())
	}

	if err := ensureFileExists("prompt catalog", c.PromptCatalogPath); err != nil {
		errs = append(errs, err.Error())
	}

	if err := ensureFileExists("seasonal event", c.SeasonalEventPath); err != nil {
		errs = append(errs, err.Error())
	}

	if strings.TrimSpace(c.LeonardoBaseURL) == "" {
		errs = append(errs, "LEONARDO_BASE_URL is required")
	}

	if strings.TrimSpace(c.GenerateEndpoint) == "" {
		errs = append(errs, "LEONARDO_GENERATE_ENDPOINT is required")
	}

	if !c.MockLeonardo && len(c.LeonardoAPIKeys) == 0 {
		errs = append(errs, "LEONARDO_API_KEY or LEONARDO_API_KEYS must be set unless LEONARDO_MOCK=true")
	}

	if !c.MockLeonardo && c.LeonardoRequireModelID && strings.TrimSpace(c.LeonardoModelID) == "" {
		errs = append(errs, "LEONARDO_MODEL_ID is required when LEONARDO_REQUIRE_MODEL_ID=true and mock mode is disabled")
	}

	for _, check := range []struct {
		name      string
		value     int
		allowZero bool
	}{
		{name: "MAX_IMAGE_WORKERS", value: c.MaxImageWorkers},
		{name: "MAX_ENHANCE_WORKERS", value: c.MaxEnhanceWorkers},
		{name: "MAX_VIDEO_WORKERS", value: c.MaxVideoWorkers},
		{name: "VIDEO_QUEUE_SIZE", value: c.VideoQueueSize},
		{name: "VARIANT_MIN", value: c.VariantMin, allowZero: true},
		{name: "VARIANT_MAX", value: c.VariantMax},
		{name: "MAX_IMAGES_PER_PROMPT", value: c.MaxImagesPerPrompt},
		{name: "MAX_TOTAL_IMAGES", value: c.MaxTotalImages},
		{name: "POLL_ATTEMPTS", value: c.PollAttempts},
		{name: "RETRY_COUNT", value: c.RetryCount, allowZero: true},
		{name: "NICHE_PROMPT_COUNT", value: c.NichePromptCount},
		{name: "LEONARDO_MAX_NUM_IMAGES", value: c.LeonardoMaxBatch},
		{name: "STOCK_TARGET_WIDTH", value: c.StockTargetWidth},
		{name: "STOCK_TARGET_HEIGHT", value: c.StockTargetHeight},
		{name: "STOCK_MIN_PIXELS", value: c.StockMinPixels},
	} {
		if err := ensureIntValue(check.name, check.value, check.allowZero); err != nil {
			errs = append(errs, err.Error())
		}
	}

	if c.VariantMax < c.VariantMin {
		errs = append(errs, fmt.Sprintf("VARIANT_MAX (%d) must be >= VARIANT_MIN (%d)", c.VariantMax, c.VariantMin))
	}

	if c.MaxTotalImages < c.MaxImagesPerPrompt {
		errs = append(errs, fmt.Sprintf("MAX_TOTAL_IMAGES (%d) must be >= MAX_IMAGES_PER_PROMPT (%d)", c.MaxTotalImages, c.MaxImagesPerPrompt))
	}

	if c.VideoQueueSize < c.MaxVideoWorkers {
		errs = append(errs, fmt.Sprintf("VIDEO_QUEUE_SIZE (%d) must be >= MAX_VIDEO_WORKERS (%d)", c.VideoQueueSize, c.MaxVideoWorkers))
	}

	if c.HTTPTimeout <= 0 {
		errs = append(errs, "HTTP_TIMEOUT_SECONDS must be positive")
	}

	if c.RetryDelay <= 0 {
		errs = append(errs, "RETRY_DELAY_MS must be positive")
	}

	if c.PollInterval <= 0 {
		errs = append(errs, "POLL_INTERVAL_MS must be positive")
	}

	if len(errs) > 0 {
		return fmt.Errorf("config validation failed: %s", strings.Join(errs, "; "))
	}

	return nil
}

func validatePort(port string) error {
	port = strings.TrimSpace(port)
	if port == "" {
		return fmt.Errorf("PORT is required")
	}

	number, err := strconv.Atoi(port)
	if err != nil {
		return fmt.Errorf("PORT must be numeric: %w", err)
	}

	if number < 1 || number > 65535 {
		return fmt.Errorf("PORT must be between 1 and 65535 (got %d)", number)
	}

	return nil
}

func ensureDirectory(name, path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("%s path is required", name)
	}

	if err := os.MkdirAll(path, 0o755); err != nil {
		return fmt.Errorf("unable to ensure %s directory %q: %w", name, path, err)
	}

	return nil
}

func ensureFileExists(name, path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("%s path is required", name)
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s file %q does not exist", name, path)
		}
		return fmt.Errorf("unable to access %s file %q: %w", name, path, err)
	}

	if info.IsDir() {
		return fmt.Errorf("%s path %q is a directory; expected a file", name, path)
	}

	return nil
}

func ensureIntValue(name string, value int, allowZero bool) error {
	if value < 0 {
		return fmt.Errorf("%s must not be negative (got %d)", name, value)
	}

	if value == 0 && !allowZero {
		return fmt.Errorf("%s must be positive", name)
	}

	return nil
}
