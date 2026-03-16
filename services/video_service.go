package services

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"time"

	"ai-generator/utils"
)

type VideoService struct {
	leonardo *LeonardoService
	logger   *log.Logger
}

func NewVideoService(leonardo *LeonardoService, logger *log.Logger) *VideoService {
	return &VideoService{
		leonardo: leonardo,
		logger:   logger,
	}
}

func (vs *VideoService) GenerateAndSaveVideo(prompt, jobDir string, index int, retryCount int, retryDelay time.Duration, enhance bool) (string, error) {
	utils.LogFlow(vs.logger, "[VID]", "GENERATE", "start index=%d prompt=%s", index, prompt)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	videoPrompt := prompt + ", cinematic motion, premium commercial stock video, stable camera, clean composition, no humans, no text, no logo"
	if enhance {
		videoPrompt += ", high clarity, enhanced sharpness, reduced noise"
	}
	videoURL, err := vs.leonardo.GenerateVideo(ctx, videoPrompt)
	if err != nil {
		utils.LogError(vs.logger, "VIDEO", "index=%d generation failed: %v", index, err)
		return "", err
	}

	filename := fmt.Sprintf("video_%03d.mp4", index)
	dst := filepath.Join(jobDir, filename)
	if err := utils.DownloadVideoFile(ctx, videoURL, dst, retryCount, retryDelay); err != nil {
		utils.LogError(vs.logger, "VIDEO", "index=%d save failed: %v", index, err)
		return "", err
	}
	utils.LogSuccess(vs.logger, "VIDEO", "index=%d saved=%s", index, filename)
	return filename, nil
}
