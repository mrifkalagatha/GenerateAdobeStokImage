package routes

import (
	"log"
	"net/http"

	"ai-generator/config"
	"ai-generator/controllers"

	"github.com/gin-gonic/gin"
)

func Setup(cfg *config.Config, logger *log.Logger, generateController *controllers.GenerateController) *gin.Engine {
	_ = cfg
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(gin.LoggerWithWriter(logger.Writer()))
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Accept")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	api := r.Group("/api/v1")
	{
		api.POST("/generate", generateController.Generate)
		api.GET("/generate/stream", generateController.GenerateStream)
		api.GET("/logs/stream", generateController.LogStream)
	}

	r.GET("/api/generate/stream", generateController.GenerateStream)
	r.GET("/api/logs/stream", generateController.LogStream)

	r.GET("/download/:job_id", generateController.Download)
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})
	r.Static("/FE", "./FE")
	r.GET("/", func(c *gin.Context) {
		c.File("./FE/index.html")
	})
	r.NoRoute(func(c *gin.Context) {
		if c.Request.Method == http.MethodGet {
			c.File("./FE/index.html")
			return
		}
		c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "page not found"})
	})

	return r
}
