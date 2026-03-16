package services

import (
	"context"
	"log"
	"sync"

	"ai-generator/utils"
)

type EnhanceService struct {
	leonardo *LeonardoService
	logger   *log.Logger
}

func NewEnhanceService(leonardo *LeonardoService, logger *log.Logger) *EnhanceService {
	return &EnhanceService{
		leonardo: leonardo,
		logger:   logger,
	}
}

func (es *EnhanceService) EnhanceImages(ctx context.Context, items []GeneratedItem, maxWorkers int) ([]GeneratedItem, error) {
	if len(items) == 0 {
		utils.LogFlow(es.logger, "[UPS]", "ENHANCE", "skip empty image list")
		return items, nil
	}
	utils.LogFlow(es.logger, "[UPS]", "ENHANCE", "start items=%d workers=%d", len(items), maxWorkers)

	out := make([]GeneratedItem, len(items))
	copy(out, items)

	semaphore := make(chan struct{}, maxWorkers)
	var wg sync.WaitGroup
	var mu sync.Mutex
	failedCount := 0

	for i := range out {
		wg.Add(1)
		idx := i
		go func() {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			utils.LogFlow(es.logger, "[UPS]", "ENHANCE", "processing index=%d", idx)
			enhancedURL, err := es.leonardo.EnhanceImage(ctx, out[idx].ImageID, out[idx].URL)
			if err != nil {
				mu.Lock()
				failedCount++
				mu.Unlock()
				utils.LogError(es.logger, "ENHANCE", "index=%d failed: %v", idx, err)
				// Keep original URL if upscale fails so pipeline can continue.
				return
			}
			out[idx].URL = enhancedURL
			utils.LogSuccess(es.logger, "ENHANCE", "index=%d done", idx)
		}()
	}

	wg.Wait()
	if failedCount > 0 {
		utils.LogFlow(es.logger, "[UPS]", "ENHANCE", "completed with fallback failed=%d success=%d", failedCount, len(out)-failedCount)
		return out, nil
	}
	utils.LogSuccess(es.logger, "ENHANCE", "all items enhanced")
	return out, nil
}
