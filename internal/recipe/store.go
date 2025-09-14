package recipe

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

// Store defines the interface for recipe data operations.
type Store interface {
	GetRecipeByImageHash(ctx context.Context, imageHash string) (*Recipe, error)
	SaveRecipe(ctx context.Context, recipe *Recipe) error
	GetImageMetadata(ctx context.Context, imageHash string) (string, error)
	SaveImageMetadata(ctx context.Context, imageHash, description string) error
	GetRecipesByCuisineOrDietaryPreference(ctx context.Context, cuisine, dietaryPreference string) ([]*Recipe, error)
	SaveImageData(ctx context.Context, imageHash, imageData string) error
	GetImageData(ctx context.Context, imageHash string) (string, error)
}

// PostgresStore implements the RecipeStore interface for PostgreSQL.
type PostgresStore struct {
	db *sqlx.DB
}

// NewPostgresStore creates a new PostgresStore.
func NewPostgresStore(dataSourceName string) (*PostgresStore, error) {
	db, err := sqlx.Connect("postgres", dataSourceName)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Create recipes table if not exists
	schema := `
	CREATE TABLE IF NOT EXISTS recipes (
		image_hash TEXT PRIMARY KEY,
		title TEXT,
		ingredients JSONB,
		instructions JSONB,
		shopping_cart JSONB,
		cuisine TEXT,
		dietary_preference TEXT,
		cooking_time TEXT,
		servings TEXT,
		image_path TEXT
	);
	`
	_, err = db.Exec(schema)
	if err != nil {
		return nil, fmt.Errorf("failed to create recipes table: %w", err)
	}

	// Create image_metadata table if not exists
	schema = `
	CREATE TABLE IF NOT EXISTS image_metadata (
		image_hash TEXT PRIMARY KEY,
		description TEXT
	);
	`
	_, err = db.Exec(schema)
	if err != nil {
		return nil, fmt.Errorf("failed to create image_metadata table: %w", err)
	}

	// Create image_data table if not exists
	schema = `
	CREATE TABLE IF NOT EXISTS image_data (
		image_hash TEXT PRIMARY KEY,
		image_data TEXT
	);
	`
	_, err = db.Exec(schema)
	if err != nil {
		return nil, fmt.Errorf("failed to create image_data table: %w", err)
	}

	return &PostgresStore{db: db}, nil
}

// GetRecipeByImageHash retrieves a recipe by its image hash.
func (s *PostgresStore) GetRecipeByImageHash(ctx context.Context, imageHash string) (*Recipe, error) {
	var r Recipe
	var ingredientsJSON, instructionsJSON, shoppingCartJSON []byte

	err := s.db.QueryRowContext(ctx, "SELECT image_hash, title, ingredients, instructions, shopping_cart, cuisine, dietary_preference, cooking_time, servings, image_path FROM recipes WHERE image_hash = $1", imageHash).Scan(
		&r.ImageHash,
		&r.Title,
		&ingredientsJSON,
		&instructionsJSON,
		&shoppingCartJSON,
		&r.Cuisine,
		&r.DietaryPreference,
		&r.CookingTime,
		&r.Servings,
		&r.ImagePath,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // Recipe not found
		}
		return nil, fmt.Errorf("failed to get recipe by hash: %w", err)
	}

	if err := json.Unmarshal(ingredientsJSON, &r.Ingredients); err != nil {
		return nil, fmt.Errorf("failed to unmarshal ingredients: %w", err)
	}
	if err := json.Unmarshal(instructionsJSON, &r.Instructions); err != nil {
		return nil, fmt.Errorf("failed to unmarshal instructions: %w", err)
	}
	if err := json.Unmarshal(shoppingCartJSON, &r.ShoppingCart); err != nil {
		return nil, fmt.Errorf("failed to unmarshal shopping cart: %w", err)
	}

	return &r, nil
}

// SaveRecipe saves a recipe to the database.
func (s *PostgresStore) SaveRecipe(ctx context.Context, recipe *Recipe) error {
	ingredientsJSON, err := json.Marshal(recipe.Ingredients)
	if err != nil {
		return fmt.Errorf("failed to marshal ingredients: %w", err)
	}
	instructionsJSON, err := json.Marshal(recipe.Instructions)
	if err != nil {
		return fmt.Errorf("failed to marshal instructions: %w", err)
	}
	shoppingCartJSON, err := json.Marshal(recipe.ShoppingCart)
	if err != nil {
		return fmt.Errorf("failed to marshal shopping cart: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		"INSERT INTO recipes (image_hash, title, ingredients, instructions, shopping_cart, cuisine, dietary_preference, cooking_time, servings, image_path) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) ON CONFLICT (image_hash) DO UPDATE SET title = $2, ingredients = $3, instructions = $4, shopping_cart = $5, cuisine = $6, dietary_preference = $7, cooking_time = $8, servings = $9, image_path = $10",
		recipe.ImageHash,
		recipe.Title,
		ingredientsJSON,
		instructionsJSON,
		shoppingCartJSON,
		recipe.Cuisine,
		recipe.DietaryPreference,
		recipe.CookingTime,
		recipe.Servings,
		recipe.ImagePath,
	)
	if err != nil {
		return fmt.Errorf("failed to save recipe: %w", err)
	}

	return nil
}

// GetImageMetadata retrieves image metadata by its image hash.
func (s *PostgresStore) GetImageMetadata(ctx context.Context, imageHash string) (string, error) {
	var description string
	err := s.db.QueryRowContext(ctx, "SELECT description FROM image_metadata WHERE image_hash = $1", imageHash).Scan(&description)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil // Metadata not found
		}
		return "", fmt.Errorf("failed to get image metadata by hash: %w", err)
	}
	return description, nil
}

// SaveImageMetadata saves image metadata to the database.
func (s *PostgresStore) SaveImageMetadata(ctx context.Context, imageHash, description string) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO image_metadata (image_hash, description) VALUES ($1, $2) ON CONFLICT (image_hash) DO UPDATE SET description = $2",
		imageHash,
		description,
	)
	if err != nil {
		return fmt.Errorf("failed to save image metadata: %w", err)
	}
	return nil
}

// GetRecipesByCuisineOrDietaryPreference retrieves recipes by cuisine or dietary preference.
func (s *PostgresStore) GetRecipesByCuisineOrDietaryPreference(ctx context.Context, cuisine, dietaryPreference string) ([]*Recipe, error) {
	var recipes []*Recipe
	var args []interface{}
	query := "SELECT image_hash, title, ingredients, instructions, shopping_cart, cuisine, dietary_preference, cooking_time, servings, image_path FROM recipes WHERE 1=1"

	paramCount := 1
	if cuisine != "" {
		query += fmt.Sprintf(" AND cuisine = $%d", paramCount)
		args = append(args, cuisine)
		paramCount++
	}
	if dietaryPreference != "" {
		query += fmt.Sprintf(" AND dietary_preference = $%d", paramCount)
		args = append(args, dietaryPreference)
		paramCount++
	}

	rows, err := s.db.QueryxContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get recipes: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var r Recipe
		var ingredientsJSON, instructionsJSON, shoppingCartJSON []byte
		err := rows.Scan(
			&r.ImageHash,
			&r.Title,
			&ingredientsJSON,
			&instructionsJSON,
			&shoppingCartJSON,
			&r.Cuisine,
			&r.DietaryPreference,
			&r.CookingTime,
			&r.Servings,
			&r.ImagePath,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan recipe row: %w", err)
		}

		if err := json.Unmarshal(ingredientsJSON, &r.Ingredients); err != nil {
			return nil, fmt.Errorf("failed to unmarshal ingredients: %w", err)
		}
		if err := json.Unmarshal(instructionsJSON, &r.Instructions); err != nil {
			return nil, fmt.Errorf("failed to unmarshal instructions: %w", err)
		}
		if err := json.Unmarshal(shoppingCartJSON, &r.ShoppingCart); err != nil {
			return nil, fmt.Errorf("failed to unmarshal shopping cart: %w", err)
		}
		recipes = append(recipes, &r)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return recipes, nil
}

// SaveImageData saves image data to the database.
func (s *PostgresStore) SaveImageData(ctx context.Context, imageHash, imageData string) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO image_data (image_hash, image_data) VALUES ($1, $2) ON CONFLICT (image_hash) DO UPDATE SET image_data = $2",
		imageHash,
		imageData,
	)
	if err != nil {
		return fmt.Errorf("failed to save image data: %w", err)
	}
	return nil
}

// GetImageData retrieves image data by its image hash.
func (s *PostgresStore) GetImageData(ctx context.Context, imageHash string) (string, error) {
	var imageData string
	err := s.db.QueryRowContext(ctx, "SELECT image_data FROM image_data WHERE image_hash = $1", imageHash).Scan(&imageData)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil // Image data not found
		}
		return "", fmt.Errorf("failed to get image data by hash: %w", err)
	}
	return imageData, nil
}
