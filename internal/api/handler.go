package api

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nfnt/resize"

	"snapchef/internal/platform/gemini"
	"snapchef/internal/recipe"
)

// GeminiClient defines the interface for interacting with the Gemini API.
type GeminiClient interface {
	IsFoodImage(ctx context.Context, imageData []byte) (bool, string, error)
	GenerateRecipe(ctx context.Context, imageData []byte, dietaryPreference, cuisine string) (*recipe.Recipe, error)
}

// LocalLLMClient defines the interface for interacting with the Local LLM API.
type LocalLLMClient interface {
	IsFoodImage(ctx context.Context, imageData []byte) (bool, string, error)
	GenerateRecipe(ctx context.Context, imageData []byte, dietaryPreference, cuisine string) (*recipe.Recipe, error)
}

// RecipeStore defines the interface for recipe data operations.
type RecipeStore interface {
	GetRecipeByImageHash(ctx context.Context, imageHash string) (*recipe.Recipe, error)
	SaveRecipe(ctx context.Context, recipe *recipe.Recipe) error
	GetImageMetadata(ctx context.Context, imageHash string) (string, error)
	SaveImageMetadata(ctx context.Context, imageHash, description string) error
	GetRecipesByCuisineOrDietaryPreference(ctx context.Context, cuisine, dietaryPreference string) ([]*recipe.Recipe, error)
	SaveImageData(ctx context.Context, imageHash, imageData string) error
	GetImageData(ctx context.Context, imageHash string) (string, error)
}

// Handler handles HTTP requests.
type Handler struct {
	GeminiClient   GeminiClient
	LocalLLMClient LocalLLMClient
	RecipeStore    RecipeStore
}

// NewHandler creates a new Handler.
func NewHandler(geminiClient GeminiClient, localLLMClient LocalLLMClient, recipeStore RecipeStore) *Handler {
	return &Handler{GeminiClient: geminiClient, LocalLLMClient: localLLMClient, RecipeStore: recipeStore}
}

// Upload handles image uploads and generates recipes.
func (h *Handler) Upload(c *gin.Context) {
	// Source
	var file *multipart.FileHeader
	var err error
	file, err = c.FormFile("file")
	if err != nil {
		log.Printf("Error getting form file: %v", err)
		c.String(http.StatusBadRequest, fmt.Sprintf("get form err: %s", err.Error()))
		return
	}

	// Validate file extension
	allowedExtensions := map[string]bool{
		".jpeg": true,
		".jpg":  true,
		".png":  true,
	}
	extension := strings.ToLower(filepath.Ext(file.Filename))
	if !allowedExtensions[extension] {
		c.String(http.StatusBadRequest, "Invalid file type. Only JPEG, JPG, and PNG images are allowed.")
		return
	}

	dietaryPreference := c.Query("dietary_preference")
	cuisine := c.Query("cuisine")

	// Read the image file into memory
	var src multipart.File
	src, err = file.Open()
	if err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("open file err: %s", err.Error()))
		return
	}
	defer src.Close()

	var imageData []byte
	imageData, err = io.ReadAll(src)
	if err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("read image err: %s", err.Error()))
		return
	}

	// Calculate image hash
	imageHash := gemini.GenerateImageHash(imageData)

	// Create a context with a 45-second timeout for external calls
	ctx, cancel := context.WithTimeout(c.Request.Context(), 45*time.Second)
	defer cancel()

	// --- Image Validation and Metadata Handling ---
	imageDescription, err := h.RecipeStore.GetImageMetadata(ctx, imageHash)
	if err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("database error: %s", err.Error()))
		return
	}

	var isFood bool
	var geminiDescription string

	if imageDescription == "" {
		// No metadata found, call Gemini API to determine if it's food
		log.Printf("Image metadata not found in database, calling Gemini API for image hash: %s", imageHash)
		isFood, geminiDescription, err = h.GeminiClient.IsFoodImage(ctx, imageData)
		if err != nil {
			c.String(http.StatusInternalServerError, fmt.Sprintf("gemini err: %s", err.Error()))
			return
		}

		// Save the new metadata to the database
		saveErr := h.RecipeStore.SaveImageMetadata(ctx, imageHash, geminiDescription)
		if saveErr != nil {
			log.Printf("failed to save image metadata: %s", saveErr.Error())
		}
	} else {
		// Metadata found, use it to determine if it's food
		log.Printf("Image metadata found in database for image hash: %s", imageHash)
		isFood = !strings.HasPrefix(strings.ToLower(strings.TrimSpace(imageDescription)), "no")
		geminiDescription = imageDescription // Use existing description
	}

	// If not food, save to non_food_images and return
	if !isFood {
		log.Printf("Image is not food, saving to non_food_images: %s", imageHash)
		savePath, saveErr := saveNonFoodImage(imageData, imageHash, extension)
		if saveErr != nil {
			log.Printf("failed to save non-food image %s: %s", savePath, saveErr.Error())
		}
		c.JSON(http.StatusOK, gin.H{"message": "Pixel Chef says: It doesn't look like food. We're here to help you whip up amazing dishes from your ingredients. Just snap a pic of your culinary creations (or ingredients!) and let's get cooking!"})
		return
	}

	// --- If it is food, proceed with recipe generation and saving ---

	// Try to get recipe from store first (only for food images)
	recipe, err := h.RecipeStore.GetRecipeByImageHash(ctx, imageHash)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			c.String(http.StatusRequestTimeout, "Database query timed out after 2 seconds")
			return
		}
		c.String(http.StatusInternalServerError, fmt.Sprintf("database error: %s", err.Error()))
		return
	}

	if recipe != nil {
		log.Printf("Recipe found in database for image hash: %s", imageHash)
		// Recipe found in database, return it
		c.JSON(http.StatusOK, recipe)
		return
	}

	// Recipe not found in database, generate with Gemini
	log.Printf("Recipe not found in database, generating with Gemini for image hash: %s, dietaryPreference: %s, cuisine: %s", imageHash, dietaryPreference, cuisine)
	recipe, err = h.GeminiClient.GenerateRecipe(ctx, imageData, dietaryPreference, cuisine)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			c.String(http.StatusRequestTimeout, "Gemini API call timed out after 45 seconds")
			return
		}
		// This error case should ideally be caught by IsFoodImage, but as a fallback
		if errors.Is(err, gemini.ErrNotFoodImage) {
			c.String(http.StatusBadRequest, "Oops! That doesn't look like food. We're here to help you whip up amazing dishes from your ingredients. Just snap a pic of your culinary creations (or ingredients!) and let's get cooking!")
			return
		}
		c.String(http.StatusInternalServerError, fmt.Sprintf("gemini err: %s", err.Error()))
		return
	}

	// Save the image to the 'images' directory
	imagePath, err := saveImage(imageData, imageHash, extension)
	if err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("failed to save image: %s", err.Error()))
		return
	}
	recipe.ImagePath = imagePath



	// Save the new recipe to the database
	recipe.ImageHash = imageHash
	err = h.RecipeStore.SaveRecipe(ctx, recipe)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			c.String(http.StatusRequestTimeout, "Database save timed out after 2 seconds")
			return
		}
		c.String(http.StatusInternalServerError, fmt.Sprintf("failed to save recipe: %s", err.Error()))
		return
	}

	c.JSON(http.StatusOK, recipe)
}

// GetRecipes handles requests to retrieve recipes based on cuisine or dietary preference.
func (h *Handler) GetRecipes(c *gin.Context) {
	cuisine := c.Query("cuisine")
	dietaryPreference := c.Query("dietary_preference")

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	recipes, err := h.RecipeStore.GetRecipesByCuisineOrDietaryPreference(ctx, cuisine, dietaryPreference)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			c.String(http.StatusRequestTimeout, "Database query timed out after 5 seconds")
			return
		}
		c.String(http.StatusInternalServerError, fmt.Sprintf("database error: %s", err.Error()))
		return
	}

	c.JSON(http.StatusOK, recipes)
}

// GetRecipe handles requests to retrieve a single recipe by image hash.
func (h *Handler) GetRecipe(c *gin.Context) {
	imageHash := c.Param("image_hash")

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	recipe, err := h.RecipeStore.GetRecipeByImageHash(ctx, imageHash)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			c.String(http.StatusRequestTimeout, "Database query timed out after 5 seconds")
			return
		}
		c.String(http.StatusInternalServerError, fmt.Sprintf("database error: %s", err.Error()))
		return
	}

	if recipe == nil {
		c.String(http.StatusNotFound, "Recipe not found")
		return
	}

	c.JSON(http.StatusOK, recipe)
}

// GetImageDescription handles requests to retrieve image metadata description.
func (h *Handler) GetImageDescription(c *gin.Context) {
	imageHash := c.Param("image_hash")

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	description, err := h.RecipeStore.GetImageMetadata(ctx, imageHash)
	if err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("database error: %s", err.Error()))
		return
	}

	if description == "" {
		c.String(http.StatusNotFound, "Description not found for this image hash")
		return
	}

	c.JSON(http.StatusOK, gin.H{"description": description})
}

// UploadImage handles image uploads, converts to base64, and saves to the database.
func (h *Handler) UploadImage(c *gin.Context) {
	// Source
	file, err := c.FormFile("file")
	if err != nil {
		c.String(http.StatusBadRequest, fmt.Sprintf("get form err: %s", err.Error()))
		return
	}

	// Validate file extension
	allowedExtensions := map[string]bool{
		".jpeg": true,
		".jpg":  true,
		".png":  true,
	}
	extension := strings.ToLower(filepath.Ext(file.Filename))
	if !allowedExtensions[extension] {
		c.String(http.StatusBadRequest, "Invalid file type. Only JPEG, JPG, and PNG images are allowed.")
		return
	}

	// Read the image file into memory
	src, err := file.Open()
	if err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("open file err: %s", err.Error()))
		return
	}
	defer src.Close()

	imageData, err := io.ReadAll(src)
	if err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("read image err: %s", err.Error()))
		return
	}

	// Calculate image hash
	imageHash := gemini.GenerateImageHash(imageData)

	// Encode image to base64
	encodedImage := base64.StdEncoding.EncodeToString(imageData)

	// Create a context with a 45-second timeout for the database call
	ctx, cancel := context.WithTimeout(c.Request.Context(), 45*time.Second)
	defer cancel()

	// Save image data to the database
	err = h.RecipeStore.SaveImageData(ctx, imageHash, encodedImage)
	if err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("failed to save image data: %s", err.Error()))
		return
	}

	c.JSON(http.StatusOK, gin.H{"image_hash": imageHash})
}

func (h *Handler) IsFood(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.String(http.StatusBadRequest, fmt.Sprintf("get form err: %s", err.Error()))
		return
	}

	src, err := file.Open()
	if err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("open file err: %s", err.Error()))
		return
	}
	defer src.Close()

	imageData, err := io.ReadAll(src)
	if err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("read image err: %s", err.Error()))
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 45*time.Second)
	defer cancel()

	isFood, description, err := h.LocalLLMClient.IsFoodImage(ctx, imageData)
	if err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("local llm err: %s", err.Error()))
		return
	}

	c.JSON(http.StatusOK, gin.H{"is_food": isFood, "description": description})
}

func (h *Handler) RecipeFinderLocal(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.String(http.StatusBadRequest, fmt.Sprintf("get form err: %s", err.Error()))
		return
	}

	dietaryPreference := c.Query("dietary_preference")
	cuisine := c.Query("cuisine")

	src, err := file.Open()
	if err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("open file err: %s", err.Error()))
		return
	}
	defer src.Close()

	imageData, err := io.ReadAll(src)
	if err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("read image err: %s", err.Error()))
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 45*time.Second)
	defer cancel()

	recipe, err := h.LocalLLMClient.GenerateRecipe(ctx, imageData, dietaryPreference, cuisine)
	if err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("local llm err: %s", err.Error()))
		return
	}

	c.JSON(http.StatusOK, recipe)
}

func (h *Handler) UploadV2(c *gin.Context) {
	// Source
	var file *multipart.FileHeader
	var err error
	file, err = c.FormFile("file")
	if err != nil {
		log.Printf("Error getting form file: %v", err)
		c.String(http.StatusBadRequest, fmt.Sprintf("get form err: %s", err.Error()))
		return
	}

	// Validate file extension
	allowedExtensions := map[string]bool{
		".jpeg": true,
		".jpg":  true,
		".png":  true,
	}
	extension := strings.ToLower(filepath.Ext(file.Filename))
	if !allowedExtensions[extension] {
		c.String(http.StatusBadRequest, "Invalid file type. Only JPEG, JPG, and PNG images are allowed.")
		return
	}

	dietaryPreference := c.Query("dietary_preference")
	cuisine := c.Query("cuisine")

	// Read the image file into memory
	var src multipart.File
	src, err = file.Open()
	if err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("open file err: %s", err.Error()))
		return
	}
	defer src.Close()

	var imageData []byte
	imageData, err = io.ReadAll(src)
	if err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("read image err: %s", err.Error()))
		return
	}

	// Calculate image hash
	imageHash := gemini.GenerateImageHash(imageData)

	// Create a context with a 45-second timeout for external calls
	ctx, cancel := context.WithTimeout(c.Request.Context(), 45*time.Second)
	defer cancel()

	// --- Image Validation and Metadata Handling ---
	imageDescription, err := h.RecipeStore.GetImageMetadata(ctx, imageHash)
	if err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("database error: %s", err.Error()))
		return
	}

	var isFood bool
	var localLLMDescription string

	if imageDescription == "" {
		// No metadata found, call Local LLM API to determine if it's food
		log.Printf("Image metadata not found in database, calling Local LLM API for image hash: %s", imageHash)
		isFood, localLLMDescription, err = h.LocalLLMClient.IsFoodImage(ctx, imageData)
		if err != nil {
			c.String(http.StatusInternalServerError, fmt.Sprintf("local llm err: %s", err.Error()))
			return
		}

		// Save the new metadata to the database
		saveErr := h.RecipeStore.SaveImageMetadata(ctx, imageHash, localLLMDescription)
		if saveErr != nil {
			log.Printf("failed to save image metadata: %s", saveErr.Error())
		}
	} else {
		// Metadata found, use it to determine if it's food
		log.Printf("Image metadata found in database for image hash: %s", imageHash)
		isFood = !strings.HasPrefix(strings.ToLower(strings.TrimSpace(imageDescription)), "no")
		localLLMDescription = imageDescription // Use existing description
	}

	// If not food, save to non_food_images and return
	if !isFood {
		log.Printf("Image is not food, saving to non_food_images: %s", imageHash)
		savePath, saveErr := saveNonFoodImage(imageData, imageHash, extension)
		if saveErr != nil {
			log.Printf("failed to save non-food image %s: %s", savePath, saveErr.Error())
		}
		c.JSON(http.StatusOK, gin.H{"message": "Pixel Chef says: It doesn't look like food. We're here to help you whip up amazing dishes from your ingredients. Just snap a pic of your culinary creations (or ingredients!) and let's get cooking!"})
		return
	}

	// --- If it is food, proceed with recipe generation and saving ---

	// Try to get recipe from store first (only for food images)
	recipe, err := h.RecipeStore.GetRecipeByImageHash(ctx, imageHash)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			c.String(http.StatusRequestTimeout, "Database query timed out after 2 seconds")
			return
		}
		c.String(http.StatusInternalServerError, fmt.Sprintf("database error: %s", err.Error()))
		return
	}

	if recipe != nil {
		log.Printf("Recipe found in database for image hash: %s", imageHash)
		// Recipe found in database, return it
		c.JSON(http.StatusOK, recipe)
		return
	}

	// Recipe not found in database, generate with Local LLM
	log.Printf("Recipe not found in database, generating with Local LLM for image hash: %s, dietaryPreference: %s, cuisine: %s", imageHash, dietaryPreference, cuisine)
	recipe, err = h.LocalLLMClient.GenerateRecipe(ctx, imageData, dietaryPreference, cuisine)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			c.String(http.StatusRequestTimeout, "Local LLM API call timed out after 45 seconds")
			return
		}
		c.String(http.StatusInternalServerError, fmt.Sprintf("local llm err: %s", err.Error()))
		return
	}

	// Save the image to the 'images' directory
	imagePath, err := saveImage(imageData, imageHash, extension)
	if err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("failed to save image: %s", err.Error()))
		return
	}
	recipe.ImagePath = imagePath



	// Save the new recipe to the database
	recipe.ImageHash = imageHash
	err = h.RecipeStore.SaveRecipe(ctx, recipe)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			c.String(http.StatusRequestTimeout, "Database save timed out after 2 seconds")
			return
		}
		c.String(http.StatusInternalServerError, fmt.Sprintf("failed to save recipe: %s", err.Error()))
		return
	}

	c.JSON(http.StatusOK, recipe)
}

func saveImage(imageData []byte, imageHash string, originalExtension string) (string, error) {
	img, _, err := image.Decode(strings.NewReader(string(imageData)))
	if err != nil {
		return "", fmt.Errorf("failed to decode image: %w", err)
	}

	img = resize.Resize(800, 0, img, resize.Lanczos3)

	// Create the images directory if it doesn't exist
	if err := os.MkdirAll("images", 0755); err != nil {
		return "", fmt.Errorf("failed to create images directory: %w", err)
	}

	imagePath := filepath.Join("images", imageHash+originalExtension)
	out, err := os.Create(imagePath)
	if err != nil {
		return "", fmt.Errorf("failed to create image file: %w", err)
	}
	defer out.Close()

	switch originalExtension {
	case ".jpeg", ".jpg":
		err = jpeg.Encode(out, img, nil)
	case ".png":
		err = png.Encode(out, img)
	default:
		return "", fmt.Errorf("unsupported image format: %s", originalExtension)
	}

	if err != nil {
		return "", fmt.Errorf("failed to encode image: %w", err)
	}

	return imagePath, nil
}

func saveNonFoodImage(imageData []byte, imageHash string, originalExtension string) (string, error) {
	img, _, err := image.Decode(strings.NewReader(string(imageData)))
	if err != nil {
		return "", fmt.Errorf("failed to decode image: %w", err)
	}

	img = resize.Resize(800, 0, img, resize.Lanczos3)

	// Create the non_food_images directory if it doesn't exist
	if err := os.MkdirAll("images/NoneFoodImages", 0755); err != nil {
		return "", fmt.Errorf("failed to create non_food_images directory: %w", err)
	}

	imagePath := filepath.Join("images/NoneFoodImages", imageHash+originalExtension)
	out, err := os.Create(imagePath)
	if err != nil {
		return "", fmt.Errorf("failed to create image file: %w", err)
	}
	defer out.Close()

	switch originalExtension {
	case ".jpeg", ".jpg":
		err = jpeg.Encode(out, img, nil)
	case ".png":
		err = png.Encode(out, img)
	default:
		return "", fmt.Errorf("unsupported image format: %s", originalExtension)
	}

	if err != nil {
		return "", fmt.Errorf("failed to encode image: %w", err)
	}

	return imagePath, nil
}
