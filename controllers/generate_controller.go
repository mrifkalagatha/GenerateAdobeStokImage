package controllers

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"ai-generator/config"
	"ai-generator/services"
	"ai-generator/utils"
	"ai-generator/workers"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type GenerateController struct {
	cfg                 *config.Config
	logger              *log.Logger
	promptService       *services.PromptService
	chatGPTPromptSource *services.ChatGPTPromptService
	promptCatalog       *services.PromptCatalogService
	marketScoring       *services.MarketScoringService
	leonardo            *services.LeonardoService
	enhanceService      *services.EnhanceService
	videoService        *services.VideoService
	videoPool           *workers.AsyncWorkerPool
}

type generateRequest struct {
	Prompt             PromptInput `json:"prompt"`
	File               string      `json:"file"`
	AutoMetadata       bool        `json:"auto_metadata"`
	Enhance            bool        `json:"enhance"`
	Video              bool        `json:"video"`
	Generate           int         `json:"generate"`
	Debug              bool        `json:"debug"`
	AutoPrompt         bool        `json:"auto_prompt"`
	StrictSingleObject bool        `json:"strict_single_object"`
	ModelID            string      `json:"model_id"`
	StyleUUID          string      `json:"style_uuid"`
	AspectRatio        string      `json:"aspect_ratio"`
	Seed               int         `json:"seed"`
	VariationStrength  string      `json:"variation_strength"`
}

type PromptInput []string

func (p *PromptInput) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "" || raw == "null" {
		*p = nil
		return nil
	}
	if strings.HasPrefix(raw, "[") {
		var arr []string
		if err := json.Unmarshal(data, &arr); err != nil {
			return err
		}
		out := make([]string, 0, len(arr))
		for _, v := range arr {
			vv := strings.TrimSpace(v)
			if vv != "" {
				out = append(out, vv)
			}
		}
		*p = out
		return nil
	}
	var single string
	if err := json.Unmarshal(data, &single); err != nil {
		return err
	}
	single = strings.TrimSpace(single)
	if single == "" {
		*p = nil
		return nil
	}
	*p = []string{single}
	return nil
}

const (
	fileTypeImage  = "image"
	fileTypeVideo  = "video"
	fileTypeVector = "vector"
)

type generateResponseData struct {
	JobID       string `json:"job_id"`
	FileType    string `json:"file_type"`
	TotalPrompt int    `json:"total_prompt"`
	TotalImage  int    `json:"total_image"`
	TotalFile   int    `json:"total_file"`
	ZipFile     string `json:"zip_file"`
}

type promptBatch struct {
	Prompt string
	Count  int
}

type streamSink struct {
	log      func(string)
	progress func(current, total int, stage string)
	complete func(message string, data any)
	fail     func(message string)
}

type similarityGuard struct {
	threshold int
	hashes    []uint64
	mu        sync.Mutex
}

func newSimilarityGuard(threshold int) *similarityGuard {
	if threshold <= 0 {
		threshold = 6
	}
	return &similarityGuard{
		threshold: threshold,
		hashes:    make([]uint64, 0, 256),
	}
}

func (sg *similarityGuard) Accept(path string) (bool, int, error) {
	h, err := utils.AverageHash64(path)
	if err != nil {
		return false, 0, err
	}
	sg.mu.Lock()
	defer sg.mu.Unlock()

	minDist := 64
	for _, existing := range sg.hashes {
		d := utils.HammingDistance64(h, existing)
		if d < minDist {
			minDist = d
		}
		if d <= sg.threshold {
			return false, d, nil
		}
	}
	sg.hashes = append(sg.hashes, h)
	if len(sg.hashes) == 1 {
		minDist = 64
	}
	return true, minDist, nil
}

func NewGenerateController(
	cfg *config.Config,
	logger *log.Logger,
	promptService *services.PromptService,
	chatGPTPromptSource *services.ChatGPTPromptService,
	promptCatalog *services.PromptCatalogService,
	marketScoring *services.MarketScoringService,
	leonardo *services.LeonardoService,
	enhanceService *services.EnhanceService,
	videoService *services.VideoService,
	videoPool *workers.AsyncWorkerPool,
) *GenerateController {
	return &GenerateController{
		cfg:                 cfg,
		logger:              logger,
		promptService:       promptService,
		chatGPTPromptSource: chatGPTPromptSource,
		promptCatalog:       promptCatalog,
		marketScoring:       marketScoring,
		leonardo:            leonardo,
		enhanceService:      enhanceService,
		videoService:        videoService,
		videoPool:           videoPool,
	}
}

func (gc *GenerateController) Generate(c *gin.Context) {
	var req generateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.LogError(gc.logger, "GENERATE", "invalid request body: %v", err)
		utils.Error(c, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	if err := gc.validateRequest(req); err != nil {
		utils.LogError(gc.logger, "GENERATE", "validation failed: %v", err)
		utils.Error(c, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	jobTimeout := gc.cfg.HTTPTimeout * 30
	if jobTimeout < 10*time.Minute {
		jobTimeout = 10 * time.Minute
	}
	jobCtx, cancel := context.WithTimeout(context.Background(), jobTimeout)
	defer cancel()

	resp, err := gc.executeGenerate(jobCtx, c.ClientIP(), req, nil)
	if err != nil {
		utils.Error(c, http.StatusBadGateway, "failed to generate files", err.Error())
		return
	}
	utils.Success(c, "generation completed", resp)
}

func (gc *GenerateController) GenerateStream(c *gin.Context) {
	payload := c.Query("payload")
	if strings.TrimSpace(payload) == "" {
		utils.Error(c, http.StatusBadRequest, "invalid request", "missing payload query")
		return
	}

	var req generateRequest
	if err := json.Unmarshal([]byte(payload), &req); err != nil {
		utils.Error(c, http.StatusBadRequest, "invalid payload json", err.Error())
		return
	}
	if err := gc.validateRequest(req); err != nil {
		utils.Error(c, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		utils.Error(c, http.StatusInternalServerError, "stream unsupported", "response writer has no flusher")
		return
	}

	var writeMu sync.Mutex
	writeEvent := func(event string, data any) bool {
		b, _ := json.Marshal(data)
		writeMu.Lock()
		defer writeMu.Unlock()
		if _, err := c.Writer.Write([]byte("event: " + event + "\n")); err != nil {
			return false
		}
		if _, err := c.Writer.Write([]byte("data: " + string(b) + "\n\n")); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	sink := &streamSink{
		log: func(message string) {
			_ = writeEvent("log", gin.H{"type": "log", "message": message})
		},
		progress: func(current, total int, stage string) {
			percent := 0
			if total > 0 {
				percent = (current * 100) / total
			}
			_ = writeEvent("progress", gin.H{
				"type":    "progress",
				"current": current,
				"total":   total,
				"percent": percent,
				"stage":   stage,
			})
		},
		complete: func(message string, data any) {
			_ = writeEvent("complete", gin.H{"type": "complete", "message": message, "data": data})
		},
		fail: func(message string) {
			_ = writeEvent("error", gin.H{"type": "error", "message": message})
		},
	}

	_, logCh, unsubscribe := utils.SubscribeLogs(512)
	defer unsubscribe()

	stopForward := make(chan struct{})
	go func() {
		for {
			select {
			case <-stopForward:
				return
			case <-c.Request.Context().Done():
				return
			case line, ok := <-logCh:
				if !ok {
					return
				}
				clean := utils.StripANSI(line)
				if strings.TrimSpace(clean) == "" {
					continue
				}
				sink.log(clean)
			}
		}
	}()
	defer close(stopForward)

	heartbeat := time.NewTicker(10 * time.Second)
	defer heartbeat.Stop()

	go func() {
		for {
			select {
			case <-stopForward:
				return
			case <-c.Request.Context().Done():
				return
			case <-heartbeat.C:
				if !writeEvent("ping", gin.H{"type": "ping", "ts": time.Now().UnixMilli()}) {
					return
				}
			}
		}
	}()

	sink.log("initializing pipeline...")
	jobTimeout := gc.cfg.HTTPTimeout * 30
	if jobTimeout < 10*time.Minute {
		jobTimeout = 10 * time.Minute
	}
	jobCtx, cancel := context.WithTimeout(context.Background(), jobTimeout)
	defer cancel()

	resp, err := gc.executeGenerate(jobCtx, c.ClientIP(), req, sink)
	if err != nil {
		sink.fail(err.Error())
		return
	}
	sink.complete("generation completed", resp)
}

func (gc *GenerateController) LogStream(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		utils.Error(c, http.StatusInternalServerError, "stream unsupported", "response writer has no flusher")
		return
	}

	_, logCh, unsubscribe := utils.SubscribeLogs(1024)
	defer unsubscribe()
	_, eventCh, unsubscribeEvents := utils.SubscribeEvents(256)
	defer unsubscribeEvents()

	writeEvent := func(event string, data any) bool {
		b, _ := json.Marshal(data)
		if _, err := c.Writer.Write([]byte("event: " + event + "\n")); err != nil {
			return false
		}
		if _, err := c.Writer.Write([]byte("data: " + string(b) + "\n\n")); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-heartbeat.C:
			if !writeEvent("ping", gin.H{"type": "ping", "ts": time.Now().UnixMilli()}) {
				return
			}
		case event, ok := <-eventCh:
			if !ok {
				return
			}
			if !writeEvent(event.Type, event) {
				return
			}
		case line, ok := <-logCh:
			if !ok {
				return
			}
			clean := utils.StripANSI(line)
			if strings.TrimSpace(clean) == "" {
				continue
			}
			if !writeEvent("log", gin.H{"type": "log", "message": clean}) {
				return
			}
		}
	}
}

func (gc *GenerateController) executeGenerate(ctx context.Context, clientIP string, req generateRequest, sink *streamSink) (generateResponseData, error) {
	utils.LogFlow(gc.logger, "[REQ]", "GENERATE", "incoming request from=%s", clientIP)

	emitLog := func(msg string) {
		utils.PublishEvent(utils.StreamEvent{
			Type:    "log",
			Message: msg,
		})
		if sink != nil && sink.log != nil {
			sink.log(msg)
		}
	}
	emitProgress := func(cur, total int, stage string) {
		percent := 0
		if total > 0 {
			percent = (cur * 100) / total
		}
		utils.PublishEvent(utils.StreamEvent{
			Type:    "progress",
			Current: cur,
			Total:   total,
			Percent: percent,
			Stage:   stage,
		})
		if sink != nil && sink.progress != nil {
			sink.progress(cur, total, stage)
		}
	}

	if req.Debug {
		utils.LogFlow(gc.logger, "[REQ]", "DEBUG", "debug mode enabled: forcing low-cost run")
		req.Generate = 1
		req.Enhance = true
		req.StrictSingleObject = true
		emitLog("debug mode enabled")
	}
	selectedFileType := normalizeFileType(req)
	emitLog(fmt.Sprintf("selected file type: %s", selectedFileType))

	perPromptLimit := gc.maxPerPromptLimit()
	requiredPrompts := promptCountForTotal(req.Generate, perPromptLimit)
	if requiredPrompts < 1 {
		requiredPrompts = 1
	}
	manualPrompts := uniquePromptList([]string(req.Prompt))
	basePrompt := ""
	if len(manualPrompts) > 0 {
		basePrompt = strings.TrimSpace(manualPrompts[0])
	}
	promptList := make([]string, 0, requiredPrompts)

	if req.AutoPrompt {
		emitLog("researching next-week stock prompt trends...")
		hints := []string{
			"next week stock demand",
			"upcoming international day relevance",
			"adobe stock commercial background and hero image",
			"single centered hero object, no humans",
		}
		prompts, err := gc.chatGPTPromptSource.GetLatestNichePrompts(ctx, basePrompt, requiredPrompts, hints)
		if err != nil {
			catalogPrompts, ferr := gc.promptCatalog.GetPrompts(basePrompt, requiredPrompts)
			if ferr != nil {
				return generateResponseData{}, fmt.Errorf("failed to fetch auto prompt from chatgpt: %w", err)
			}
			promptList = catalogPrompts
			emitLog("chatgpt limit detected, using local prompt catalog")
		} else {
			seasonalCtx := services.BuildSeasonalContext(gc.cfg.SeasonalEventPath, time.Now(), basePrompt)
			ranked := gc.marketScoring.RankPrompts(basePrompt, prompts, seasonalCtx)
			promptList = topRankedPrompts(ranked, requiredPrompts)
			for i, rankedPrompt := range ranked {
				if i >= 3 {
					break
				}
				emitLog(fmt.Sprintf("market score=%d prompt=%s", rankedPrompt.Score, rankedPrompt.Prompt))
			}
			emitLog(fmt.Sprintf("prompt list ready: %d prompts", len(promptList)))
		}
	} else {
		promptList = append(promptList, manualPrompts...)
	}

	// Always keep user prompt as primary seed so output stays close to direct Leonardo result.
	if basePrompt != "" && len(promptList) == 0 {
		promptList = append([]string{basePrompt}, promptList...)
	}
	promptList = uniquePromptList(promptList)
	if len(promptList) > requiredPrompts {
		promptList = promptList[:requiredPrompts]
	}
	if selectedFileType == fileTypeVector {
		promptList = vectorizePromptList(promptList)
		emitLog("vector mode enabled: applying vector-oriented prompt cues")
	}

	usePromptHardening := req.AutoPrompt
	if len(promptList) > 0 && usePromptHardening {
		promptList = gc.promptService.OptimizeCommercialPrompts(basePrompt, nil, promptList, req.StrictSingleObject)
		emitLog(fmt.Sprintf("prompt hardening applied: %d prompts", len(promptList)))
	} else if len(promptList) > 0 {
		emitLog("prompt passthrough mode enabled (manual prompt)")
	}

	batches, buildErr := gc.buildPromptBatches(promptList, req.Generate, perPromptLimit)
	if buildErr != nil {
		return generateResponseData{}, buildErr
	}
	if len(batches) == 0 {
		return generateResponseData{}, errors.New("failed to build prompt batches")
	}
	variationStrength := normalizeVariationStrength(req.VariationStrength)
	batches = diversifyPromptBatches(batches, variationStrength)
	batches = explodeBatchesToSingles(batches, variationStrength)
	emitLog(fmt.Sprintf("prompt variation applied: %d unique image jobs (%s)", len(batches), variationStrength))

	emitLog("sending request to leonardo ai...")
	emitProgress(0, req.Generate, "start")

	jobID := "job_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:12]
	jobDir, err := utils.BuildJobDir(gc.cfg.StoragePath, jobID, time.Now())
	if err != nil {
		return generateResponseData{}, fmt.Errorf("failed to create job directory: %w", err)
	}

	savedCount := 0
	metadataSeeds := make([]services.MetadataSeed, 0, req.Generate)
	switch selectedFileType {
	case fileTypeVideo:
		emitLog("generating videos...")
		videoSeed := promptsToVideoItems(batches, req.Generate)
		videoFiles, videoErr := gc.generateVideos(ctx, videoSeed, jobDir, req.Enhance)
		if videoErr != nil {
			return generateResponseData{}, fmt.Errorf("failed to generate videos: %w", videoErr)
		}
		savedCount = len(videoFiles)
		metadataSeeds = append(metadataSeeds, videoFiles...)
		emitLog(fmt.Sprintf("video generated=%d", savedCount))
	case fileTypeImage, fileTypeVector:
		genOpts := services.GenerateOptions{
			PassthroughPrompt:  !usePromptHardening,
			StrictSingleObject: req.StrictSingleObject,
			ModelID:            strings.TrimSpace(req.ModelID),
			StyleUUID:          strings.TrimSpace(req.StyleUUID),
			AspectRatio:        strings.TrimSpace(req.AspectRatio),
			Seed:               req.Seed,
		}
		generatedImages, genErr := gc.generateImages(ctx, batches, req.Generate, genOpts, sink)
		if genErr != nil {
			return generateResponseData{}, genErr
		}

		if req.Enhance {
			emitLog("enhancing images...")
			enhanced, err := gc.enhanceService.EnhanceImages(ctx, generatedImages, gc.cfg.MaxEnhanceWorkers)
			if err != nil {
				return generateResponseData{}, fmt.Errorf("failed to enhance images: %w", err)
			}
			generatedImages = enhanced
		}

		simGuard := newSimilarityGuard(6)
		if selectedFileType == fileTypeVector {
			emitLog("vectorizing files...")
			vectorFiles, saveErr := gc.saveGeneratedVectors(ctx, generatedImages, jobDir, simGuard)
			err = saveErr
			if err != nil {
				return generateResponseData{}, fmt.Errorf("failed to save vector files: %w", err)
			}
			savedCount = len(vectorFiles)
			metadataSeeds = append(metadataSeeds, vectorFiles...)
		} else {
			emitLog("saving files...")
			imageFiles, saveErr := gc.saveGeneratedImages(ctx, generatedImages, jobDir, simGuard)
			err = saveErr
			if err != nil {
				return generateResponseData{}, fmt.Errorf("failed to save generated files: %w", err)
			}
			savedCount = len(imageFiles)
			metadataSeeds = append(metadataSeeds, imageFiles...)
		}

		// Auto-refill when similar files are rejected by similarity guard.
		for pass := 1; savedCount < req.Generate && pass <= 3; pass++ {
			shortfall := req.Generate - savedCount
			emitLog(fmt.Sprintf("similarity refill pass=%d shortfall=%d", pass, shortfall))
			refillBatches, refillErr := gc.buildPromptBatches(promptList, shortfall, perPromptLimit)
			if refillErr != nil {
				return generateResponseData{}, refillErr
			}
			if len(refillBatches) == 0 {
				break
			}
			for i := range refillBatches {
				refillBatches[i].Prompt += fmt.Sprintf(", refill pass %d", pass)
			}
			refillBatches = diversifyPromptBatches(refillBatches, variationStrength)
			refillBatches = explodeBatchesToSingles(refillBatches, variationStrength)
			refillItems, refillGenErr := gc.generateImages(ctx, refillBatches, shortfall, genOpts, sink)
			if refillGenErr != nil {
				return generateResponseData{}, refillGenErr
			}

			if selectedFileType == fileTypeVector {
				newFiles, saveErr := gc.saveGeneratedVectors(ctx, refillItems, jobDir, simGuard)
				if saveErr != nil {
					return generateResponseData{}, fmt.Errorf("failed to save vector refill files: %w", saveErr)
				}
				savedCount += len(newFiles)
				metadataSeeds = append(metadataSeeds, newFiles...)
			} else {
				newFiles, saveErr := gc.saveGeneratedImages(ctx, refillItems, jobDir, simGuard)
				if saveErr != nil {
					return generateResponseData{}, fmt.Errorf("failed to save refill files: %w", saveErr)
				}
				savedCount += len(newFiles)
				metadataSeeds = append(metadataSeeds, newFiles...)
			}
		}
	default:
		return generateResponseData{}, fmt.Errorf("unsupported file type: %s", selectedFileType)
	}

	if req.AutoMetadata {
		emitLog("generating adobe stock metadata...")
		metadata, metaErr := gc.chatGPTPromptSource.GenerateAdobeMetadata(ctx, metadataSeeds)
		if metaErr != nil {
			emitLog("metadata via chatgpt failed, using fallback metadata")
			metadata = fallbackMetadata(metadataSeeds)
		}
		csvPath := filepath.Join(jobDir, "adobe_stock_metadata.csv")
		if err := writeAdobeMetadataCSV(csvPath, metadata); err != nil {
			return generateResponseData{}, fmt.Errorf("failed to write metadata csv: %w", err)
		}
		emitLog("metadata ready: adobe_stock_metadata.csv")
	}

	emitLog("compressing zip...")
	zipName := fmt.Sprintf("AdobeStock_%s_(%d).zip", time.Now().Format("20060102_150405"), savedCount)
	zipPath := filepath.Join(jobDir, zipName)
	if err := utils.ZipFolder(jobDir, zipPath); err != nil {
		return generateResponseData{}, fmt.Errorf("failed to create zip: %w", err)
	}

	emitProgress(savedCount, req.Generate, "completed")
	resp := generateResponseData{
		JobID:       jobID,
		FileType:    selectedFileType,
		TotalPrompt: len(batches),
		TotalImage:  savedCount,
		TotalFile:   savedCount,
		ZipFile:     "/download/" + jobID,
	}
	utils.PublishEvent(utils.StreamEvent{
		Type:    "complete",
		Message: "generation completed",
		Current: savedCount,
		Total:   savedCount,
		Percent: 100,
		Stage:   "completed",
		ZipFile: resp.ZipFile,
		JobID:   resp.JobID,
	})
	return resp, nil
}

func (gc *GenerateController) Download(c *gin.Context) {
	jobID := c.Param("job_id")
	if strings.TrimSpace(jobID) == "" {
		utils.LogError(gc.logger, "DOWNLOAD", "empty job_id")
		utils.Error(c, http.StatusBadRequest, "invalid job id", "job_id is required")
		return
	}

	zipPath, err := utils.FindZipByJobID(gc.cfg.StoragePath, jobID)
	if err != nil {
		utils.LogError(gc.logger, "DOWNLOAD", "job=%s zip not found: %v", jobID, err)
		utils.Error(c, http.StatusNotFound, "zip file not found", err.Error())
		return
	}

	cleanup := c.Query("cleanup") == "1" || strings.EqualFold(c.Query("cleanup"), "true")

	f, err := os.Open(zipPath)
	if err != nil {
		utils.LogError(gc.logger, "DOWNLOAD", "job=%s open zip failed: %v", jobID, err)
		utils.Error(c, http.StatusInternalServerError, "failed to open zip", err.Error())
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		utils.LogError(gc.logger, "DOWNLOAD", "job=%s stat zip failed: %v", jobID, err)
		utils.Error(c, http.StatusInternalServerError, "failed to read zip info", err.Error())
		return
	}

	filename := filepath.Base(zipPath)
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	c.Header("Content-Length", fmt.Sprintf("%d", info.Size()))
	c.Status(http.StatusOK)

	if _, err := io.Copy(c.Writer, f); err != nil {
		utils.LogError(gc.logger, "DOWNLOAD", "job=%s transfer failed: %v", jobID, err)
		return
	}

	utils.LogSuccess(gc.logger, "DOWNLOAD", "job=%s served zip=%s", jobID, zipPath)

	if cleanup {
		jobDir := filepath.Dir(zipPath)
		if err := os.RemoveAll(jobDir); err != nil {
			utils.LogError(gc.logger, "CLEANUP", "job=%s cleanup failed: %v", jobID, err)
			return
		}
		utils.LogSuccess(gc.logger, "CLEANUP", "job=%s storage cleaned", jobID)
	}
}

func (gc *GenerateController) validateRequest(req generateRequest) error {
	prompts := uniquePromptList([]string(req.Prompt))
	if len(prompts) == 0 {
		return errors.New("prompt must not be empty (string or array)")
	}
	if len(prompts) > 10 {
		return errors.New("prompt array max is 10")
	}
	if req.Generate <= 0 {
		return errors.New("generate must be greater than 0")
	}
	if req.Generate > gc.cfg.MaxTotalImages {
		return fmt.Errorf("generate exceeds total limit (%d)", gc.cfg.MaxTotalImages)
	}
	if strings.TrimSpace(req.VariationStrength) != "" && !isValidVariationStrength(req.VariationStrength) {
		return errors.New("variation_strength must be one of: low, medium, high")
	}
	selectedFileType := normalizeFileType(req)
	if selectedFileType != fileTypeImage && selectedFileType != fileTypeVideo && selectedFileType != fileTypeVector {
		return errors.New("file must be one of: image, video, vector")
	}
	if strings.TrimSpace(req.File) == "" {
		req.File = selectedFileType
	}
	selectedModelID := strings.TrimSpace(req.ModelID)
	if selectedModelID == "" {
		selectedModelID = strings.TrimSpace(gc.cfg.LeonardoModelID)
	}
	if selectedFileType != fileTypeVideo && gc.cfg.LeonardoRequireModelID && selectedModelID == "" {
		return errors.New("model_id is required (set request.model_id or LEONARDO_MODEL_ID) to avoid fallback model")
	}
	return nil
}

func (gc *GenerateController) generateImages(ctx context.Context, batches []promptBatch, totalTarget int, opts services.GenerateOptions, sink *streamSink) ([]services.GeneratedItem, error) {
	type result struct {
		items []services.GeneratedItem
		err   error
	}

	results := make(chan result, len(batches))
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, gc.cfg.MaxImageWorkers)

	for batchIdx, batch := range batches {
		wg.Add(1)
		b := batch
		idx := batchIdx
		go func() {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			utils.LogFlow(gc.logger, "[IMG]", "GENERATE", "prompt=%s count=%d", b.Prompt, b.Count)
			localOpts := opts
			if localOpts.Seed > 0 {
				localOpts.Seed = localOpts.Seed + idx + 1
			}
			assets, err := gc.leonardo.GenerateImageWithOptions(ctx, b.Prompt, b.Count, localOpts)
			if err != nil {
				utils.LogError(gc.logger, "GENERATE", "prompt failed: %s err=%v", b.Prompt, err)
				results <- result{err: fmt.Errorf("prompt '%s': %w", b.Prompt, err)}
				return
			}
			utils.LogSuccess(gc.logger, "GENERATE", "prompt done: %s assets=%d", b.Prompt, len(assets))

			items := make([]services.GeneratedItem, 0, len(assets))
			for _, asset := range assets {
				items = append(items, services.GeneratedItem{
					Prompt:  b.Prompt,
					ImageID: asset.ImageID,
					URL:     asset.URL,
				})
			}
			results <- result{items: items}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	all := make([]services.GeneratedItem, 0)
	var firstErr error
	current := 0

	for r := range results {
		if r.err != nil && firstErr == nil {
			firstErr = r.err
			continue
		}
		all = append(all, r.items...)
		current += len(r.items)
		percent := 0
		if totalTarget > 0 {
			percent = (current * 100) / totalTarget
		}
		utils.PublishEvent(utils.StreamEvent{
			Type:    "progress",
			Current: current,
			Total:   totalTarget,
			Percent: percent,
			Stage:   "generating_images",
		})
		if sink != nil && sink.progress != nil {
			sink.progress(current, totalTarget, "generating_images")
		}
		if sink != nil && sink.log != nil {
			sink.log(fmt.Sprintf("image batch completed: %d/%d (%d%%)", current, totalTarget, percent))
		}
	}

	if len(all) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return all, nil
}

func (gc *GenerateController) maxPerPromptLimit() int {
	limit := gc.cfg.MaxImagesPerPrompt
	if limit <= 0 {
		limit = 10
	}
	if limit > 10 {
		limit = 10
	}
	return limit
}

func promptCountForTotal(total, maxPerPrompt int) int {
	if total <= 0 || maxPerPrompt <= 0 {
		return 0
	}
	n := total / maxPerPrompt
	if total%maxPerPrompt != 0 {
		n++
	}
	return n
}

func topRankedPrompts(scores []services.PromptScore, count int) []string {
	if count <= 0 {
		count = len(scores)
	}
	out := make([]string, 0, count)
	for _, item := range scores {
		if strings.TrimSpace(item.Prompt) == "" {
			continue
		}
		out = append(out, item.Prompt)
		if len(out) >= count {
			break
		}
	}
	return out
}

func (gc *GenerateController) buildPromptBatches(prompts []string, total, maxPerPrompt int) ([]promptBatch, error) {
	clean := make([]string, 0, len(prompts))
	for _, p := range prompts {
		pp := strings.TrimSpace(p)
		if pp != "" {
			clean = append(clean, pp)
		}
	}
	if len(clean) == 0 || total <= 0 || maxPerPrompt <= 0 {
		return nil, nil
	}
	clean = uniquePromptList(clean)

	maxPossible := len(clean) * maxPerPrompt
	if total > maxPossible {
		return nil, fmt.Errorf("generate=%d exceeds max possible with unique prompts (%d prompts x %d each = %d). add more prompts or reduce generate", total, len(clean), maxPerPrompt, maxPossible)
	}

	promptCount := len(clean)
	base := total / promptCount
	rem := total % promptCount
	counts := make([]int, promptCount)
	for i := 0; i < promptCount; i++ {
		counts[i] = base
		if i < rem {
			counts[i]++
		}
	}

	out := make([]promptBatch, 0, promptCount)
	for i, p := range clean {
		n := counts[i]
		if n <= 0 {
			continue
		}
		out = append(out, promptBatch{
			Prompt: p,
			Count:  n,
		})
	}
	return out, nil
}

func diversifyPromptBatches(in []promptBatch, strength string) []promptBatch {
	if len(in) == 0 {
		return in
	}
	out := make([]promptBatch, 0, len(in))
	for i, b := range in {
		out = append(out, promptBatch{
			Prompt: diversifyPrompt(b.Prompt, i, strength),
			Count:  b.Count,
		})
	}
	return out
}

func diversifyPrompt(base string, idx int, strength string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return base
	}
	strength = normalizeVariationStrength(strength)

	cameras := []string{
		"front three-quarter view",
		"eye-level product shot",
		"isometric view",
		"low-angle hero framing",
		"top-down editorial frame",
	}
	lenses := []string{
		"35mm lens look",
		"50mm lens look",
		"85mm lens look",
		"macro lens detail look",
	}
	lights := []string{
		"soft cinematic rim light",
		"controlled studio key light",
		"neon cyan-violet edge lighting",
		"subtle volumetric haze lighting",
	}
	compositions := []string{
		"centered hero composition",
		"asymmetric negative-space composition",
		"rule-of-thirds composition",
		"balanced geometric composition",
	}
	depths := []string{
		"sharp subject focus with soft background falloff",
		"high micro-detail surface texture",
		"clean foreground-background separation",
		"precise silhouette readability",
	}
	moods := []string{
		"calm premium mood",
		"high-tech editorial mood",
		"futuristic cinematic mood",
		"luxury minimal mood",
	}
	materials := []string{
		"glass and polished metal material behavior",
		"translucent crystal material response",
		"clean reflective surface physics",
		"subtle emissive energy accents",
	}

	parts := []string{base}
	switch strength {
	case "low":
		parts = append(parts,
			cameras[idx%len(cameras)],
			compositions[idx%len(compositions)],
			fmt.Sprintf("variation set %d", idx+1),
		)
	case "high":
		parts = append(parts,
			cameras[idx%len(cameras)],
			lenses[idx%len(lenses)],
			lights[idx%len(lights)],
			compositions[idx%len(compositions)],
			depths[idx%len(depths)],
			moods[idx%len(moods)],
			materials[idx%len(materials)],
			fmt.Sprintf("variation set %d high-diversity", idx+1),
		)
	default: // medium
		parts = append(parts,
			cameras[idx%len(cameras)],
			lenses[idx%len(lenses)],
			lights[idx%len(lights)],
			compositions[idx%len(compositions)],
			depths[idx%len(depths)],
			fmt.Sprintf("variation set %d", idx+1),
		)
	}
	return strings.Join(parts, ", ")
}

func normalizeVariationStrength(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "low":
		return "low"
	case "high":
		return "high"
	default:
		return "medium"
	}
}

func isValidVariationStrength(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "low", "medium", "high":
		return true
	default:
		return false
	}
}

func explodeBatchesToSingles(in []promptBatch, strength string) []promptBatch {
	if len(in) == 0 {
		return in
	}
	out := make([]promptBatch, 0)
	run := 0
	for _, b := range in {
		n := b.Count
		if n < 1 {
			n = 1
		}
		for i := 0; i < n; i++ {
			run++
			out = append(out, promptBatch{
				Prompt: instancePromptVariation(b.Prompt, i, run, strength),
				Count:  1,
			})
		}
	}
	return out
}

func instancePromptVariation(base string, localIdx, globalIdx int, strength string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return base
	}
	strength = normalizeVariationStrength(strength)
	microAngles := []string{
		"micro angle shift 3 degrees",
		"micro angle shift 7 degrees",
		"micro angle shift 12 degrees",
		"slightly elevated camera position",
		"slightly lowered camera position",
	}
	lightTweaks := []string{
		"key light intensity variation",
		"rim light softness variation",
		"shadow depth variation",
		"highlight rolloff variation",
	}
	frameTweaks := []string{
		"subject scale 28 percent frame",
		"subject scale 34 percent frame",
		"subject scale 40 percent frame",
		"negative space emphasis",
	}
	parts := []string{base}
	parts = append(parts, microAngles[globalIdx%len(microAngles)])
	if strength != "low" {
		parts = append(parts, lightTweaks[(globalIdx+localIdx)%len(lightTweaks)])
	}
	if strength == "high" {
		parts = append(parts, frameTweaks[(globalIdx+2*localIdx)%len(frameTweaks)])
	}
	parts = append(parts, fmt.Sprintf("unique render variant %d", globalIdx))
	return strings.Join(parts, ", ")
}

func (gc *GenerateController) saveGeneratedImages(ctx context.Context, items []services.GeneratedItem, jobDir string, sg *similarityGuard) ([]services.MetadataSeed, error) {
	if len(items) == 0 {
		return nil, errors.New("no images to save")
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	saved := make([]services.MetadataSeed, 0, len(items))

	semaphore := make(chan struct{}, gc.cfg.MaxImageWorkers)
	for i, item := range items {
		wg.Add(1)
		idx := i + 1
		it := item
		go func() {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			filename := utils.EnsureJPGPath(fmt.Sprintf("image_%03d.jpg", idx))
			dst := filepath.Join(jobDir, filename)
			if err := utils.DownloadOrCreateFile(ctx, it.URL, dst, gc.cfg.RetryCount, gc.cfg.RetryDelay); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("save %s failed: %w", filename, err)
				}
				mu.Unlock()
				utils.LogError(gc.logger, "SAVE", "file=%s err=%v", filename, err)
				return
			}
			if err := utils.NormalizeToStockJPEG(dst, gc.cfg.StockTargetWidth, gc.cfg.StockTargetHeight, gc.cfg.StockMinPixels); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("normalize %s failed: %w", filename, err)
				}
				mu.Unlock()
				utils.LogError(gc.logger, "SAVE", "normalize file=%s err=%v", filename, err)
				return
			}
			if sg != nil {
				ok, dist, err := sg.Accept(dst)
				if err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = fmt.Errorf("similarity hash %s failed: %w", filename, err)
					}
					mu.Unlock()
					_ = os.Remove(dst)
					return
				}
				if !ok {
					utils.LogError(gc.logger, "SIM", "rejected similar file=%s distance=%d threshold=%d", filename, dist, sg.threshold)
					_ = os.Remove(dst)
					return
				}
			}

			mu.Lock()
			saved = append(saved, services.MetadataSeed{
				Filename: filename,
				Prompt:   it.Prompt,
				FileType: fileTypeImage,
			})
			mu.Unlock()
			utils.LogFlow(gc.logger, "[IO]", "SAVE", "saved file=%s", filename)
		}()
	}

	wg.Wait()

	if len(saved) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return saved, nil
}

func (gc *GenerateController) saveGeneratedVectors(ctx context.Context, items []services.GeneratedItem, jobDir string, sg *similarityGuard) ([]services.MetadataSeed, error) {
	if len(items) == 0 {
		return nil, errors.New("no images to vectorize")
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	saved := make([]services.MetadataSeed, 0, len(items))
	semaphore := make(chan struct{}, gc.cfg.MaxImageWorkers)

	for i, item := range items {
		wg.Add(1)
		idx := i + 1
		it := item
		go func() {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			tmpRaster := filepath.Join(jobDir, fmt.Sprintf("vector_src_%03d.png", idx))
			if err := utils.DownloadOrCreateFile(ctx, it.URL, tmpRaster, gc.cfg.RetryCount, gc.cfg.RetryDelay); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("vector source %03d failed: %w", idx, err)
				}
				mu.Unlock()
				utils.LogError(gc.logger, "VECTOR", "download source index=%d err=%v", idx, err)
				return
			}

			filename := fmt.Sprintf("vector_%03d.svg", idx)
			dst := filepath.Join(jobDir, filename)
			if err := utils.ConvertImageToSVG(tmpRaster, dst); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("vectorize %03d failed: %w", idx, err)
				}
				mu.Unlock()
				utils.LogError(gc.logger, "VECTOR", "vectorize index=%d err=%v", idx, err)
				_ = os.Remove(tmpRaster)
				return
			}
			if sg != nil {
				ok, dist, err := sg.Accept(tmpRaster)
				if err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = fmt.Errorf("similarity hash vector src %03d failed: %w", idx, err)
					}
					mu.Unlock()
					_ = os.Remove(tmpRaster)
					_ = os.Remove(dst)
					return
				}
				if !ok {
					utils.LogError(gc.logger, "SIM", "rejected similar vector=%s distance=%d threshold=%d", filename, dist, sg.threshold)
					_ = os.Remove(tmpRaster)
					_ = os.Remove(dst)
					return
				}
			}
			_ = os.Remove(tmpRaster)

			mu.Lock()
			saved = append(saved, services.MetadataSeed{
				Filename: filename,
				Prompt:   it.Prompt,
				FileType: fileTypeVector,
			})
			mu.Unlock()
			utils.LogFlow(gc.logger, "[VEC]", "SAVE", "saved file=%s", filename)
		}()
	}

	wg.Wait()
	if len(saved) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return saved, nil
}

func (gc *GenerateController) generateVideos(ctx context.Context, images []services.GeneratedItem, jobDir string, enhance bool) ([]services.MetadataSeed, error) {
	if len(images) == 0 {
		return nil, nil
	}

	type result struct {
		file services.MetadataSeed
		err  error
	}
	results := make(chan result, len(images))
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, gc.cfg.MaxVideoWorkers)

	for i, item := range images {
		wg.Add(1)
		index := i + 1
		videoPrompt := item.Prompt
		if strings.TrimSpace(videoPrompt) == "" {
			videoPrompt = "commercial futuristic technology hero object, no humans"
		}
		go func() {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			filename, err := gc.videoService.GenerateAndSaveVideo(videoPrompt, jobDir, index, gc.cfg.RetryCount, gc.cfg.RetryDelay, enhance)
			if err != nil {
				results <- result{err: err}
				return
			}
			results <- result{
				file: services.MetadataSeed{
					Filename: filename,
					Prompt:   videoPrompt,
					FileType: fileTypeVideo,
				},
			}
		}()
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	saved := make([]services.MetadataSeed, 0, len(images))
	var firstErr error
	for r := range results {
		if r.err != nil && firstErr == nil {
			firstErr = r.err
			continue
		}
		if r.err == nil {
			saved = append(saved, r.file)
		}
	}
	if len(saved) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return saved, nil
}

func uniquePromptList(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, p := range in {
		pp := strings.TrimSpace(p)
		if pp == "" {
			continue
		}
		key := strings.ToLower(pp)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, pp)
	}
	return out
}

func normalizeFileType(req generateRequest) string {
	fileType := strings.ToLower(strings.TrimSpace(req.File))
	if fileType != "" {
		return fileType
	}
	if req.Video {
		return fileTypeVideo
	}
	return fileTypeImage
}

func vectorizePromptList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, p := range in {
		pp := strings.TrimSpace(p)
		if pp == "" {
			continue
		}
		if !strings.Contains(strings.ToLower(pp), "vector") {
			pp += ", vector illustration style, clean flat shapes, crisp edges, smooth gradients, isolated subject, no photorealism"
		}
		out = append(out, pp)
	}
	return out
}

func promptsToVideoItems(batches []promptBatch, total int) []services.GeneratedItem {
	out := make([]services.GeneratedItem, 0, total)
	for _, b := range batches {
		for i := 0; i < b.Count; i++ {
			out = append(out, services.GeneratedItem{Prompt: b.Prompt})
			if len(out) >= total {
				return out
			}
		}
	}
	return out
}

func fallbackMetadata(seeds []services.MetadataSeed) []services.AdobeStockMetadata {
	out := make([]services.AdobeStockMetadata, 0, len(seeds))
	titleSuffix := []string{
		"for modern marketing",
		"for premium branding",
		"for digital campaign",
		"for website hero",
		"for presentation cover",
	}
	keywordPools := [][]string{
		{"technology", "digital", "abstract", "background", "clean", "no humans"},
		{"marketing", "branding", "template", "copy space", "modern", "minimal"},
		{"cinematic", "gradient", "high contrast", "professional", "editorial", "premium"},
		{"website", "landing page", "social media", "ad creative", "corporate", "banner"},
	}
	for i, seed := range seeds {
		title := "Commercial stock asset generated from AI prompt"
		promptWords := strings.Fields(strings.TrimSpace(seed.Prompt))
		if len(promptWords) > 0 {
			n := 7
			if len(promptWords) < n {
				n = len(promptWords)
			}
			title = strings.Join(promptWords[:n], " ") + " " + titleSuffix[i%len(titleSuffix)]
		}
		kw := make([]string, 0, 32)
		kw = append(kw, keywordPools[i%len(keywordPools)]...)
		kw = append(kw, strings.ToLower(seed.FileType))
		for _, w := range strings.Fields(strings.ToLower(seed.Prompt)) {
			clean := strings.Trim(w, ",.;:!?()[]{}\"'")
			if clean == "" {
				continue
			}
			kw = append(kw, clean)
			if len(kw) >= 35 {
				break
			}
		}
		kw = uniquePromptList(kw)
		out = append(out, services.AdobeStockMetadata{
			Filename: seed.Filename,
			Title:    strings.TrimSpace(title),
			Keywords: kw,
			Category: fallbackCategory(seed.FileType),
		})
	}
	return out
}

func writeAdobeMetadataCSV(path string, items []services.AdobeStockMetadata) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	if err := w.Write([]string{"Filename", "Title", "Keywords", "Category"}); err != nil {
		return err
	}
	for _, item := range items {
		keywords := strings.Join(item.Keywords, ", ")
		if err := w.Write([]string{item.Filename, item.Title, keywords, item.Category}); err != nil {
			return err
		}
	}
	return w.Error()
}

func fallbackCategory(fileType string) string {
	switch strings.ToLower(strings.TrimSpace(fileType)) {
	case fileTypeVideo:
		return "Video"
	case fileTypeVector:
		return "Graphic Resources"
	default:
		return "Technology"
	}
}
