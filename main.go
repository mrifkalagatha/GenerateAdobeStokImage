package main

import (
	"io"
	"log"
	"os"

	"ai-generator/config"
	"ai-generator/controllers"
	"ai-generator/routes"
	"ai-generator/services"
	"ai-generator/utils"
	"ai-generator/workers"
)

func main() {
	cfg := config.Load()
	loggerWriter := io.MultiWriter(os.Stdout, utils.NewLogBusWriter())
	logger := log.New(loggerWriter, "[ai-generator] ", log.LstdFlags|log.Lshortfile)
	if err := cfg.Validate(); err != nil {
		logger.Fatalf("config validation failed: %v", err)
	}
	utils.LogFlow(logger, "[SYS]", "BOOT", "service starting on port=%s storage=%s", cfg.Port, cfg.StoragePath)

	leonardoService := services.NewLeonardoService(cfg, logger)
	promptService := services.NewPromptService(logger)
	chatGPTPromptService := services.NewChatGPTPromptService(cfg, logger)
	promptCatalogService := services.NewPromptCatalogService(cfg.PromptCatalogPath, cfg.SeasonalEventPath)
	marketScoringService := services.NewMarketScoringService()
	enhanceService := services.NewEnhanceService(leonardoService, logger)
	videoService := services.NewVideoService(leonardoService, logger)

	videoPool := workers.NewAsyncWorkerPool(cfg.MaxVideoWorkers, cfg.VideoQueueSize, logger)
	videoPool.Start()
	defer videoPool.Stop()
	utils.LogFlow(logger, "[WRK]", "BOOT", "video worker pool started workers=%d queue=%d", cfg.MaxVideoWorkers, cfg.VideoQueueSize)

	generateController := controllers.NewGenerateController(
		cfg,
		logger,
		promptService,
		chatGPTPromptService,
		promptCatalogService,
		marketScoringService,
		leonardoService,
		enhanceService,
		videoService,
		videoPool,
	)

	r := routes.Setup(cfg, logger, generateController)
	utils.LogSuccess(logger, "BOOT", "http server initialized")
	if err := r.Run(":" + cfg.Port); err != nil {
		utils.LogError(logger, "BOOT", "server failed: %v", err)
		logger.Fatalf("server failed: %v", err)
	}
}
