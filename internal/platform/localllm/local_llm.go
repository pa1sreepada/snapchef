package localllm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"snapchef/internal/recipe"
)

// Client represents a client for the local LLM.
type Client struct {
	httpClient *http.Client
	apiURL     string
}

// NewClient creates a new client for the local LLM.
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{},
		apiURL:     "http://localhost:1234/v1/chat/completions",
	}
}

// Request represents the request body for the local LLM.
type Request struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature"`
	MaxTokens   int       `json:"max_tokens"`
}

// Message represents a message in the request.
type Message struct {
	Role    string    `json:"role"`
	Content []Content `json:"content"`
}

// Content represents the content of a message.
type Content struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

// ImageURL represents the image URL in the content.
type ImageURL struct {
	URL string `json:"url"`
}

// Response represents the response from the local LLM.
type Response struct {
	Choices []Choice `json:"choices"`
}

// Choice represents a choice in the response.
type Choice struct {
	Message ResponseMessage `json:"message"`
}

// ResponseMessage represents a message in the response.
type ResponseMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// GenerateContent sends a request to the local LLM and returns the response.
func (c *Client) GenerateContent(ctx context.Context, text string, imageData string) (string, error) {
	reqBody := Request{
		Model: "gemma-3-12b-it:2",
		Messages: []Message{
			{
				Role: "user",
				Content: []Content{
					{
						Type: "text",
						Text: text,
					},
					{
						Type: "image_url",
						ImageURL: &ImageURL{
							URL: "data:image/jpeg;base64," + imageData,
						},
					},
				},
			},
		},
		Temperature: 1,
		MaxTokens:   1024,
	}

	reqBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL, bytes.NewBuffer(reqBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("received non-OK status code: %d", resp.StatusCode)
	}

	var llmResp Response
	if err := json.NewDecoder(resp.Body).Decode(&llmResp); err != nil {
		return "", fmt.Errorf("failed to decode response body: %w", err)
	}

	if len(llmResp.Choices) > 0 {
		fmt.Printf("LLM Response: %s\n", llmResp.Choices[0].Message.Content)
		return llmResp.Choices[0].Message.Content, nil
	}

	return "", fmt.Errorf("no content found in response")
}

func (c *Client) IsFoodImage(ctx context.Context, imageData []byte) (bool, string, error) {
	prompt := "Analyze the provided image. If it contains food, return a brief recipe description. If not, respond with 'NO' followed by a 5-word description of the image content."
	encodedImage := base64.StdEncoding.EncodeToString(imageData)
	responseText, err := c.GenerateContent(ctx, prompt, encodedImage)
	if err != nil {
		return false, "", fmt.Errorf("failed to generate content: %w", err)
	}

	if len(responseText) > 2 && responseText[:2] == "No" {
		return false, responseText, nil
	}

	return true, responseText, nil
}

func (c *Client) GenerateRecipe(ctx context.Context, imageData []byte, dietaryPreference, cuisine string) (*recipe.Recipe, error) {
	prompt := "I need a recipe for the food item in this image. Please return a single, clean JSON object with the following keys and data types: 'title' (string), 'cuisine' (string), 'dietary_preference' (string), 'cooking_time' (string), 'servings' (string), 'ingredients' (map of ingredient names to quantities), 'instructions' (array of strings), and 'shopping_cart' (map of ingredient names to quantities). .The JSON response should be clean and not contain any markdown formatting."
	if dietaryPreference != "" {
		prompt += fmt.Sprintf(" The recipe should be %s.", dietaryPreference)
	}
	if cuisine != "" {
		prompt += fmt.Sprintf(" The cuisine should be %s.", cuisine)
	}

	encodedImage := base64.StdEncoding.EncodeToString(imageData)
	responseText, err := c.GenerateContent(ctx, prompt, encodedImage)
	if err != nil {
		return nil, fmt.Errorf("failed to generate content: %w", err)
	}

	// Clean up the response text
	cleanedResponse := strings.TrimPrefix(responseText, "```json")
	cleanedResponse = strings.TrimSuffix(cleanedResponse, "```")
	cleanedResponse = strings.TrimSpace(cleanedResponse)

	var r recipe.Recipe
	if err := json.Unmarshal([]byte(cleanedResponse), &r); err != nil {
		return nil, fmt.Errorf("failed to unmarshal recipe from response: %w", err)
	}

	return &r, nil
}
