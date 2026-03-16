package config

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Port                   string
	StoragePath            string
	LeonardoAPIKey         string
	LeonardoAPIKeys        []string
	LeonardoBaseURL        string
	GenerateEndpoint       string
	UpscaleEndpoint        string
	VideoEndpoint          string
	GenerationGetPath      string
	LeonardoStyleUUID      string
	LeonardoModelID        string
	LeonardoDefaultAlchemy bool
	LeonardoRequireModelID bool
	HTTPTimeout            time.Duration
	RetryCount             int
	RetryDelay             time.Duration
	PollAttempts           int
	PollInterval           time.Duration
	MaxImageWorkers        int
	MaxEnhanceWorkers      int
	MaxVideoWorkers        int
	VideoQueueSize         int
	VariantMin             int
	VariantMax             int
	MaxImagesPerPrompt     int
	MockLeonardo           bool
	OpenAIAPIKey           string
	OpenAIBaseURL          string
	OpenAIModel            string
	NichePromptCount       int
	LeonardoMaxBatch       int
	StockTargetWidth       int
	StockTargetHeight      int
	StockMinPixels         int
	MaxTotalImages         int
	PromptCatalogPath      string
	SeasonalEventPath      string
}

func Load() *Config {
	_ = godotenv.Load()

	primaryKey := strings.TrimSpace(os.Getenv("LEONARDO_API_KEY"))
	multiKeys := parseCSVEnv("LEONARDO_API_KEYS")
	if primaryKey != "" {
		multiKeys = append([]string{primaryKey}, multiKeys...)
	}
	multiKeys = uniqueNonEmpty(multiKeys)

	return &Config{
		Port:                   getEnv("PORT", "8080"),
		StoragePath:            getEnv("STORAGE_PATH", "storage"),
		LeonardoAPIKey:         primaryKey,
		LeonardoAPIKeys:        multiKeys,
		LeonardoBaseURL:        getEnv("LEONARDO_BASE_URL", "https://cloud.leonardo.ai/api/rest/v1"),
		GenerateEndpoint:       getEnv("LEONARDO_GENERATE_ENDPOINT", "/generations"),
		UpscaleEndpoint:        getEnv("LEONARDO_UPSCALE_ENDPOINT", "/variations/upscale"),
		VideoEndpoint:          getEnv("LEONARDO_VIDEO_ENDPOINT", "/generations-text-to-video"),
		GenerationGetPath:      getEnv("LEONARDO_GENERATION_GET_PATH", "/generations/{id}"),
		LeonardoStyleUUID:      getEnv("LEONARDO_STYLE_UUID", ""),
		LeonardoModelID:        getEnv("LEONARDO_MODEL_ID", ""),
		LeonardoDefaultAlchemy: getEnvBool("LEONARDO_DEFAULT_ALCHEMY", false),
		LeonardoRequireModelID: getEnvBool("LEONARDO_REQUIRE_MODEL_ID", true),
		HTTPTimeout:            time.Duration(getEnvInt("HTTP_TIMEOUT_SECONDS", 60)) * time.Second,
		RetryCount:             getEnvInt("RETRY_COUNT", 3),
		RetryDelay:             time.Duration(getEnvInt("RETRY_DELAY_MS", 1200)) * time.Millisecond,
		PollAttempts:           getEnvInt("POLL_ATTEMPTS", 20),
		PollInterval:           time.Duration(getEnvInt("POLL_INTERVAL_MS", 2000)) * time.Millisecond,
		MaxImageWorkers:        getEnvInt("MAX_IMAGE_WORKERS", 5),
		MaxEnhanceWorkers:      getEnvInt("MAX_ENHANCE_WORKERS", 5),
		MaxVideoWorkers:        getEnvInt("MAX_VIDEO_WORKERS", 2),
		VideoQueueSize:         getEnvInt("VIDEO_QUEUE_SIZE", 200),
		VariantMin:             getEnvInt("VARIANT_MIN", 3),
		VariantMax:             getEnvInt("VARIANT_MAX", 5),
		MaxImagesPerPrompt:     getEnvInt("MAX_IMAGES_PER_PROMPT", 10),
		MockLeonardo:           getEnvBool("LEONARDO_MOCK", false),
		OpenAIAPIKey:           os.Getenv("OPENAI_API_KEY"),
		OpenAIBaseURL:          getEnv("OPENAI_BASE_URL", "https://api.openai.com/v1"),
		OpenAIModel:            getEnv("OPENAI_MODEL", "gpt-4o-mini"),
		NichePromptCount:       getEnvInt("NICHE_PROMPT_COUNT", 8),
		LeonardoMaxBatch:       getEnvInt("LEONARDO_MAX_NUM_IMAGES", 2),
		StockTargetWidth:       getEnvInt("STOCK_TARGET_WIDTH", 3840),
		StockTargetHeight:      getEnvInt("STOCK_TARGET_HEIGHT", 2160),
		StockMinPixels:         getEnvInt("STOCK_MIN_PIXELS", 4000000),
		MaxTotalImages:         getEnvInt("MAX_TOTAL_IMAGES", 500),
		PromptCatalogPath:      getEnv("PROMPT_CATALOG_PATH", "data/prompt_catalog.json"),
		SeasonalEventPath:      getEnv("SEASONAL_EVENT_PATH", "data/seasonal_events.json"),
	}
}

func getEnv(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}

func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func getEnvBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func parseCSVEnv(key string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		pp := strings.TrimSpace(p)
		if pp != "" {
			out = append(out, pp)
		}
	}
	return out
}

func uniqueNonEmpty(in []string) []string {
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
