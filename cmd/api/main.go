package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"snapchef/internal/api"
	"snapchef/internal/platform/gemini"
	"snapchef/internal/platform/localllm"
	"snapchef/internal/recipe"
)

// Config represents the application configuration.
type Config struct {
	GeminiAPIKey string `json:"gemini_api_key"`
	DatabaseURL  string `json:"DATABASE_URL"`
}

func main() {
	ctx := context.Background()

	// Read configuration from config.json
	configData, err := os.ReadFile("config.json")
	if err != nil {
		panic(fmt.Errorf("failed to read config.json: %w", err))
	}

	var config Config
	if err := json.Unmarshal(configData, &config); err != nil {
		panic(fmt.Errorf("failed to unmarshal config.json: %w", err))
	}

	geminiClient, err := gemini.NewClient(ctx, config.GeminiAPIKey)
	if err != nil {
		panic(fmt.Errorf("error creating gemini client: %w", err))
	}

	localLLMClient := localllm.NewClient()

	dbStore, err := recipe.NewPostgresStore(config.DatabaseURL)
	if err != nil {
		panic(fmt.Errorf("error creating postgresstore: %w", err))
	}

	handler := api.NewHandler(geminiClient, localLLMClient, dbStore)

	r := gin.Default()

	// Configure CORS middleware
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:8081"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))
		r.POST("/recipefinder", handler.Upload)
	r.POST("/v2/recipefinder", handler.UploadV2)
	r.GET("/recipes", handler.GetRecipes)
	r.GET("/recipes/:image_hash", handler.GetRecipe)
	r.GET("/image-metadata/:image_hash", handler.GetImageDescription)
		r.POST("/imageencoder", handler.UploadImage)
	r.POST("/is-food", handler.IsFood)
	r.POST("/recipe-finder-local", handler.RecipeFinderLocal)
	r.Static("/images", "./images")
	r.Run(":8080") // listen and serve on 0.0.0.0:8081
}
