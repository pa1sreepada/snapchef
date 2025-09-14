package gemini

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"

	"snapchef/internal/recipe"
)

// ErrNotFoodImage is returned when the image does not contain food.
var ErrNotFoodImage = fmt.Errorf("image does not contain food")

// Client is a client for the Gemini API.
type Client struct {
	model *genai.GenerativeModel
}

// NewClient creates a new Gemini client.
func NewClient(ctx context.Context, apiKey string) (*Client, error) {
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, err
	}
	return &Client{model: client.GenerativeModel("gemini-1.5-flash")}, nil
}

// GenerateImageHash calculates the SHA256 hash of the image data.
func GenerateImageHash(imageData []byte) string {
	hash := sha256.Sum256(imageData)
	return hex.EncodeToString(hash[:])
}

// IsFoodImage checks if the given image contains food and returns a description.
func (c *Client) IsFoodImage(ctx context.Context, imageData []byte) (bool, string, error) {
	prompt := []genai.Part{
		genai.ImageData("png", imageData),
		// genai.Text("Does this image contain food? If yes, provide a brief description of the receipe. If no, just respond with 'NO' followed by a very short description of the image."),
		genai.Text("Analyze the provided image. If it contains food, return a brief recipe description. If not, respond with 'NO' followed by a 5-word description of the image content."),
	}

	resp, err := c.model.GenerateContent(ctx, prompt...)
	if err != nil {
		return false, "", err
	}

	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return false, "", fmt.Errorf("empty response from Gemini for food check")
	}

	text, ok := resp.Candidates[0].Content.Parts[0].(genai.Text)
	if !ok {
		return false, "", fmt.Errorf("unexpected response format from Gemini for food check")
	}

	response := strings.ToLower(strings.TrimSpace(string(text)))
	if strings.HasPrefix(response, "no") {
		return false, string(text), nil // Return the actual response text as description
	}
	return true, string(text), nil
}

// GenerateRecipe generates a recipe from an image.
func (c *Client) GenerateRecipe(ctx context.Context, imageData []byte, dietaryPreference, cuisine string) (*recipe.Recipe, error) {
	// First, validate if the image contains food
	isFood, _, err := c.IsFoodImage(ctx, imageData)
	if err != nil {
		return nil, fmt.Errorf("failed to check if image is food: %w", err)
	}
	if !isFood {
		return nil, ErrNotFoodImage
	}

	// Build the prompt with optional dietary preferences and cuisine
	// Original -- promptText := "Generate a recipe based on the food item in this image. The response should be a JSON object with four keys: 'title', 'cuisine', 'dietary_preference', 'ingredients', 'instructions', and 'shopping_cart'. 'title' should be a string, 'cuisine' should be a string, 'dietary_preference' should be a list of strings,'ingredients' should be a map of ingredient names to their quantities, 'instructions' should be an array of strings, and 'shopping_cart' should be a map of ingredient names to their quantities. The JSON response should be clean and not contain any markdown formatting (e.g., ```json).";
	promptText := "I need a recipe for the food item in this image. Please return a single, clean JSON object with the following keys and data types: 'title' (string), 'cuisine' (string), 'dietary_preference' (string), 'cooking_time' (string), 'servings' (string), 'ingredients' (map of ingredient names to quantities), 'instructions' (array of strings), and 'shopping_cart' (map of ingredient names to quantities). .The JSON response should be clean and not contain any markdown formatting (e.g., ```json)."

	if dietaryPreference != "" {
		promptText += fmt.Sprintf(" The recipe should be %s.", dietaryPreference)
	}
	if cuisine != "" {
		promptText += fmt.Sprintf(" The recipe should be %s cuisine.", cuisine)
	}

	prompt := []genai.Part{
		genai.ImageData("png", imageData),
		genai.Text(promptText),
	}

	resp, err := c.model.GenerateContent(ctx, prompt...)
	if err != nil {
		return nil, err
	}

	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("empty response from Gemini")
	}

	jsonString, ok := resp.Candidates[0].Content.Parts[0].(genai.Text)
	if !ok {
		return nil, fmt.Errorf("unexpected response format from Gemini")
	}

	// Extract the JSON from the response, which might be wrapped in markdown
	startIndex := strings.Index(string(jsonString), "{")
	endIndex := strings.LastIndex(string(jsonString), "}")

	if startIndex == -1 || endIndex == -1 || startIndex > endIndex {
		return nil, fmt.Errorf("could not find JSON object in response: %s", jsonString)
	}

	cleanJSON := string(jsonString)[startIndex : endIndex+1]

	// Unmarshal the JSON into a Recipe struct
	var r recipe.Recipe
	if err := json.Unmarshal([]byte(cleanJSON), &r); err != nil {
		return nil, fmt.Errorf("failed to unmarshal recipe JSON: %w. Raw response: %s", err, cleanJSON)
	}

	r.Cuisine = cuisine
	r.DietaryPreference = dietaryPreference

	return &r, nil
}
