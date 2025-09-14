package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"snapchef/internal/api"
	"snapchef/internal/platform/gemini"
	"snapchef/internal/recipe"
)

// mockGeminiClient is a mock of the Gemini client.
type mockGeminiClient struct {
	returnError               error
	receivedDietaryPreference string
	receivedCuisine           string
}

// GenerateRecipe mocks the GenerateRecipe method.
func (m *mockGeminiClient) GenerateRecipe(ctx context.Context, imageData []byte, dietaryPreference, cuisine string) (*recipe.Recipe, error) {
	m.receivedDietaryPreference = dietaryPreference
	m.receivedCuisine = cuisine
	if m.returnError != nil {
		return nil, m.returnError
	}
	// Create a mock recipe
	return &recipe.Recipe{
		Title:        "Mock Recipe Title",
		Ingredients:  map[string]string{"Flour": "2 cups"},
		Instructions: []string{"Mix ingredients"},
		ShoppingCart: map[string]string{"Flour": "2 cups"},
	}, nil
}

// SetError sets the error to be returned by GenerateRecipe.
func (m *mockGeminiClient) SetError(err error) {
	m.returnError = err
}

// IsFoodImage mocks the IsFoodImage method.
func (m *mockGeminiClient) IsFoodImage(ctx context.Context, imageData []byte) (bool, string, error) {
	if m.returnError != nil {
		return false, "", m.returnError
	}
	return true, "mock gemini description", nil
}

// mockLocalLLMClient is a mock of the Local LLM client.
type mockLocalLLMClient struct {
	returnError               error
	receivedDietaryPreference string
	receivedCuisine           string
}

// IsFoodImage mocks the IsFoodImage method.
func (m *mockLocalLLMClient) IsFoodImage(ctx context.Context, imageData []byte) (bool, string, error) {
	if m.returnError != nil {
		return false, "", m.returnError
	}
	return true, "mock local description", nil
}

// GenerateRecipe mocks the GenerateRecipe method.
func (m *mockLocalLLMClient) GenerateRecipe(ctx context.Context, imageData []byte, dietaryPreference, cuisine string) (*recipe.Recipe, error) {
	m.receivedDietaryPreference = dietaryPreference
	m.receivedCuisine = cuisine
	if m.returnError != nil {
		return nil, m.returnError
	}
	// Create a mock recipe
	return &recipe.Recipe{
		Title:        "Mock Local Recipe Title",
		Ingredients:  map[string]string{"Sugar": "1 cup"},
		Instructions: []string{"Stir well"},
		ShoppingCart: map[string]string{"Sugar": "1 cup"},
	}, nil
}

// mockRecipeStore is a mock of the RecipeStore.
type mockRecipeStore struct {
	returnError               error
	receivedDietaryPreference string
	receivedCuisine           string
}

// mockRecipeStore is a mock of the RecipeStore.
type mockRecipeStore struct {
	recipes   map[string]*recipe.Recipe
	getError  error
	saveError error
	metadata  map[string]string
}

// NewMockRecipeStore creates a new mockRecipeStore.
func NewMockRecipeStore() *mockRecipeStore {
	return &mockRecipeStore{recipes: make(map[string]*recipe.Recipe), metadata: make(map[string]string)}
}

// GetRecipeByImageHash mocks the GetRecipeByImageHash method.
func (m *mockRecipeStore) GetRecipeByImageHash(ctx context.Context, imageHash string) (*recipe.Recipe, error) {
	if m.getError != nil {
		return nil, m.getError
	}
	return m.recipes[imageHash], nil
}

// SaveRecipe mocks the SaveRecipe method.
func (m *mockRecipeStore) SaveRecipe(ctx context.Context, r *recipe.Recipe) error {
	if m.saveError != nil {
		return m.saveError
	}
	m.recipes[r.ImageHash] = r
	return nil
}

// GetImageMetadata mocks the GetImageMetadata method.
func (m *mockRecipeStore) GetImageMetadata(ctx context.Context, imageHash string) (string, error) {
	return m.metadata[imageHash], nil
}

// SaveImageMetadata mocks the SaveImageMetadata method.
func (m *mockRecipeStore) SaveImageMetadata(ctx context.Context, imageHash, description string) error {
	m.metadata[imageHash] = description
	return nil
}

// GetRecipesByCuisineOrDietaryPreference mocks the GetRecipesByCuisineOrDietaryPreference method.
func (m *mockRecipeStore) GetRecipesByCuisineOrDietaryPreference(ctx context.Context, cuisine, dietaryPreference string) ([]*recipe.Recipe, error) {
	var filteredRecipes []*recipe.Recipe
	for _, r := range m.recipes {
		matchCuisine := (cuisine == "" || r.Cuisine == cuisine)
		matchDietaryPreference := (dietaryPreference == "" || r.DietaryPreference == dietaryPreference)
		if matchCuisine && matchDietaryPreference {
			filteredRecipes = append(filteredRecipes, r)
		}
	}
	return filteredRecipes, nil
}

func TestUpload(t *testing.T) {
	// Set up Gin in test mode
	gin.SetMode(gin.TestMode)

	// Create a new Gin router
	r := gin.Default()

	// Create mocks
	mockGeminiClient := &mockGeminiClient{}
	mockRecipeStore := NewMockRecipeStore()

	// Create a new handler with the mocks
	mockLocalLLMClient := &mockLocalLLMClient{}
	handler := api.NewHandler(mockGeminiClient, mockLocalLLMClient, mockRecipeStore)

	// Register the upload route
	r.POST("/recipefinder", handler.Upload)

	// Create a dummy image file
	file, err := os.CreateTemp("", "test-*.png")
	assert.NoError(t, err)
	defer os.Remove(file.Name())

	// Read image data
	imageData, err := os.ReadFile(file.Name())
	assert.NoError(t, err)

	// Calculate image hash
	imageHash := gemini.GenerateImageHash(imageData)

	// Create a new multipart writer
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", file.Name())
	assert.NoError(t, err)

	// Copy the dummy image data to the multipart writer
	_, err = io.Copy(part, bytes.NewReader(imageData))
	assert.NoError(t, err)
	writer.Close()

	// Create a new HTTP request
	req := httptest.NewRequest(http.MethodPost, "/recipefinder", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Create a new response recorder
	rr := httptest.NewRecorder()

	// Serve the HTTP request
	r.ServeHTTP(rr, req)

	// Assert the response status code
	assert.Equal(t, http.StatusOK, rr.Code)

	// Decode the response body
	var recipe recipe.Recipe
	err = json.Unmarshal(rr.Body.Bytes(), &recipe)
	assert.NoError(t, err)

	// Assert the recipe content
	assert.Equal(t, "2 cups", recipe.Ingredients["Flour"])
	assert.Equal(t, "Mix ingredients", recipe.Instructions[0])
	assert.Equal(t, "2 cups", recipe.ShoppingCart["Flour"])
	assert.Equal(t, "Mock Recipe Title", recipe.Title)

	// Assert that the recipe was saved to the store
	storedRecipe, err := mockRecipeStore.GetRecipeByImageHash(context.Background(), imageHash)
	assert.NoError(t, err)
	assert.NotNil(t, storedRecipe)
	assert.Equal(t, recipe.Ingredients, storedRecipe.Ingredients)
}

func TestUpload_NotFoodImage(t *testing.T) {
	// Set up Gin in test mode
	gin.SetMode(gin.TestMode)

	// Create a new Gin router
	r := gin.Default()

	// Create a mock Gemini client that returns ErrNotFoodImage
	mockGeminiClient := &mockGeminiClient{}
	mockGeminiClient.SetError(gemini.ErrNotFoodImage)

	// Create a mock recipe store
	mockRecipeStore := NewMockRecipeStore()

	// Create a new handler with the mocks
	mockLocalLLMClient := &mockLocalLLMClient{}
	handler := api.NewHandler(mockGeminiClient, mockLocalLLMClient, mockRecipeStore)

	// Register the upload route
	r.POST("/recipefinder", handler.Upload)

	// Create a dummy image file
	file, err := os.CreateTemp("", "test-*.png")
	assert.NoError(t, err)
	defer os.Remove(file.Name())

	// Read image data
	imageData, err := os.ReadFile(file.Name())
	assert.NoError(t, err)

	// Create a new multipart writer
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", file.Name())
	assert.NoError(t, err)

	// Copy the dummy image data to the multipart writer
	_, err = io.Copy(part, bytes.NewReader(imageData))
	assert.NoError(t, err)
	writer.Close()

	// Create a new HTTP request
	req := httptest.NewRequest(http.MethodPost, "/recipefinder", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Create a new response recorder
	rr := httptest.NewRecorder()

	// Serve the HTTP request
	r.ServeHTTP(rr, req)

	// Assert the response status code
	assert.Equal(t, http.StatusBadRequest, rr.Code)

	// Assert the response body
	assert.Equal(t, "We need to find something delicious! Please provide an image full of food.", rr.Body.String())
}

func TestUpload_RecipeFoundInStore(t *testing.T) {
	// Set up Gin in test mode
	gin.SetMode(gin.TestMode)

	// Create a new Gin router
	r := gin.Default()

	// Create mocks
	mockGeminiClient := &mockGeminiClient{}
	mockRecipeStore := NewMockRecipeStore()

	// Create a dummy image file
	file, err := os.CreateTemp("", "test-*.png")
	assert.NoError(t, err)
	defer os.Remove(file.Name())

	// Read image data
	imageData, err := os.ReadFile(file.Name())
	assert.NoError(t, err)

	// Calculate image hash
	imageHash := gemini.GenerateImageHash(imageData)

	// Pre-populate the store with a recipe for this image hash
	existingRecipe := &recipe.Recipe{
		ImageHash:    imageHash,
		Title:        "Existing Recipe Title",
		Ingredients:  map[string]string{"Water": "1 cup"},
		Instructions: []string{"Boil water"},
		ShoppingCart: map[string]string{"Water": "1 cup"},
	}
	mockRecipeStore.SaveRecipe(context.Background(), existingRecipe)

	// Create a new handler with the mocks
	mockLocalLLMClient := &mockLocalLLMClient{}
	handler := api.NewHandler(mockGeminiClient, mockLocalLLMClient, mockRecipeStore)

	// Register the upload route
	r.POST("/recipefinder", handler.Upload)

	// Create a new multipart writer
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", file.Name())
	assert.NoError(t, err)

	// Copy the dummy image data to the multipart writer
	_, err = io.Copy(part, bytes.NewReader(imageData))
	assert.NoError(t, err)
	writer.Close()

	// Create a new HTTP request
	req := httptest.NewRequest(http.MethodPost, "/recipefinder", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Create a new response recorder
	rr := httptest.NewRecorder()

	// Serve the HTTP request
	r.ServeHTTP(rr, req)

	// Assert the response status code
	assert.Equal(t, http.StatusOK, rr.Code)

	// Decode the response body
	var returnedRecipe recipe.Recipe
	err = json.Unmarshal(rr.Body.Bytes(), &returnedRecipe)
	assert.NoError(t, err)

	// Assert that the returned recipe is the one from the store
	assert.Equal(t, existingRecipe.Ingredients, returnedRecipe.Ingredients)
	assert.Equal(t, existingRecipe.Instructions, returnedRecipe.Instructions)
	assert.Equal(t, existingRecipe.ShoppingCart, returnedRecipe.ShoppingCart)
}

func TestUpload_DietaryPreference(t *testing.T) {
	// Set up Gin in test mode
	gin.SetMode(gin.TestMode)

	// Create a new Gin router
	r := gin.Default()

	// Create mocks
	mockGeminiClient := &mockGeminiClient{}
	mockRecipeStore := NewMockRecipeStore()

	// Create a new handler with the mocks
	mockLocalLLMClient := &mockLocalLLMClient{}
	handler := api.NewHandler(mockGeminiClient, mockLocalLLMClient, mockRecipeStore)

	// Register the upload route
	r.POST("/recipefinder", handler.Upload)

	// Create a dummy image file
	file, err := os.CreateTemp("", "test-*.png")
	assert.NoError(t, err)
	defer os.Remove(file.Name())

	// Read image data
	imageData, err := os.ReadFile(file.Name())
	assert.NoError(t, err)

	// Create a new multipart writer
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", file.Name())
	assert.NoError(t, err)

	// Copy the dummy image data to the multipart writer
	_, err = io.Copy(part, bytes.NewReader(imageData))
	assert.NoError(t, err)
	writer.Close()

	// Create a new HTTP request with dietary preference
	req := httptest.NewRequest(http.MethodPost, "/recipefinder?dietary_preference=vegetarian", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Create a new response recorder
	rr := httptest.NewRecorder()

	// Serve the HTTP request
	r.ServeHTTP(rr, req)

	// Assert the response status code
	assert.Equal(t, http.StatusOK, rr.Code)

	// Assert that the dietary preference was passed to the Gemini client
	assert.Equal(t, "vegetarian", mockGeminiClient.receivedDietaryPreference)
}

func TestUpload_Cuisine(t *testing.T) {
	// Set up Gin in test mode
	gin.SetMode(gin.TestMode)

	// Create a new Gin router
	r := gin.Default()

	// Create mocks
	mockGeminiClient := &mockGeminiClient{}
	mockRecipeStore := NewMockRecipeStore()

	// Create a new handler with the mocks
	mockLocalLLMClient := &mockLocalLLMClient{}
	handler := api.NewHandler(mockGeminiClient, mockLocalLLMClient, mockRecipeStore)

	// Register the upload route
	r.POST("/recipefinder", handler.Upload)

	// Create a dummy image file
	file, err := os.CreateTemp("", "test-*.png")
	assert.NoError(t, err)
	defer os.Remove(file.Name())

	// Read image data
	imageData, err := os.ReadFile(file.Name())
	assert.NoError(t, err)

	// Create a new multipart writer
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", file.Name())
	assert.NoError(t, err)

	// Copy the dummy image data to the multipart writer
	_, err = io.Copy(part, bytes.NewReader(imageData))
	assert.NoError(t, err)
	writer.Close()

	// Create a new HTTP request with cuisine
	req := httptest.NewRequest(http.MethodPost, "/recipefinder?cuisine=italian", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Create a new response recorder
	rr := httptest.NewRecorder()

	// Serve the HTTP request
	r.ServeHTTP(rr, req)

	// Assert the response status code
	assert.Equal(t, http.StatusOK, rr.Code)

	// Assert that the cuisine was passed to the Gemini client
	assert.Equal(t, "italian", mockGeminiClient.receivedCuisine)
}

func TestGetRecipes(t *testing.T) {
	// Set up Gin in test mode
	gin.SetMode(gin.TestMode)

	// Create a new Gin router
	r := gin.Default()

	// Create mocks
	mockGeminiClient := &mockGeminiClient{}
	mockRecipeStore := NewMockRecipeStore()

	// Pre-populate the store with some recipes
	mockRecipeStore.SaveRecipe(context.Background(), &recipe.Recipe{
		ImageHash:         "hash1",
		Title:             "Recipe 1",
		Cuisine:           "Italian",
		DietaryPreference: "Vegetarian",
		Ingredients:       map[string]string{"Tomato": "1"},
		Instructions:      []string{"Chop tomato"},
		ShoppingCart:      map[string]string{"Tomato": "1"},
	})
	mockRecipeStore.SaveRecipe(context.Background(), &recipe.Recipe{
		ImageHash:         "hash2",
		Title:             "Recipe 2",
		Cuisine:           "Mexican",
		DietaryPreference: "Vegan",
		Ingredients:       map[string]string{"Avocado": "1"},
		Instructions:      []string{"Mash avocado"},
		ShoppingCart:      map[string]string{"Avocado": "1"},
	})
	mockRecipeStore.SaveRecipe(context.Background(), &recipe.Recipe{
		ImageHash:         "hash3",
		Title:             "Recipe 3",
		Cuisine:           "Italian",
		DietaryPreference: "",
		Ingredients:       map[string]string{"Pasta": "1"},
		Instructions:      []string{"Boil pasta"},
		ShoppingCart:      map[string]string{"Pasta": "1"},
	})

	// Create a new handler with the mocks
	mockLocalLLMClient := &mockLocalLLMClient{}
	handler := api.NewHandler(mockGeminiClient, mockLocalLLMClient, mockRecipeStore)

	// Register the GET recipes route
	r.GET("/recipes", handler.GetRecipes)

	// Test case 1: Get all recipes
	req := httptest.NewRequest(http.MethodGet, "/recipes", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	var recipes []recipe.Recipe
	json.Unmarshal(rr.Body.Bytes(), &recipes)
	assert.Len(t, recipes, 3)

	// Test case 2: Get Italian recipes
	req = httptest.NewRequest(http.MethodGet, "/recipes?cuisine=Italian", nil)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	json.Unmarshal(rr.Body.Bytes(), &recipes)
	assert.Len(t, recipes, 2)
	assert.Equal(t, "Recipe 1", recipes[0].Title)
	assert.Equal(t, "Recipe 3", recipes[1].Title)

	// Test case 3: Get Vegetarian recipes
	req = httptest.NewRequest(http.MethodGet, "/recipes?dietary_preference=Vegetarian", nil)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	json.Unmarshal(rr.Body.Bytes(), &recipes)
	assert.Len(t, recipes, 1)
	assert.Equal(t, "Recipe 1", recipes[0].Title)

	// Test case 4: Get Italian Vegan recipes (should be none)
	req = httptest.NewRequest(http.MethodGet, "/recipes?cuisine=Italian&dietary_preference=Vegan", nil)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	json.Unmarshal(rr.Body.Bytes(), &recipes)
	assert.Len(t, recipes, 0)
}
