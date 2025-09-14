package recipe

import (
	"encoding/json"
	"strings"
)

// Recipe represents the structure of the generated recipe
type Recipe struct {
	ImageHash         string            `json:"image_hash" db:"image_hash"`
	Title             string            `json:"title" db:"title"`
	Ingredients       map[string]string `json:"ingredients"`
	Instructions      []string          `json:"instructions"`
	ShoppingCart      map[string]string `json:"shopping_cart"`
	Cuisine           string            `json:"cuisine" db:"cuisine"`
	DietaryPreference string            `json:"dietary_preference" db:"dietary_preference"`
	CookingTime       string            `json:"cooking_time" db:"cooking_time"`
	Servings          string            `json:"servings" db:"servings"`
	ImagePath         string            `json:"image_path" db:"image_path"`
}

// UnmarshalJSON implements the json.Unmarshaler interface for Recipe.
func (r *Recipe) UnmarshalJSON(data []byte) error {
	type Alias Recipe // Create an alias to avoid infinite recursion
	aux := &struct {
		Cuisine           string `json:"cuisine"`
		DietaryPreference string `json:"dietary_preference"`
		*Alias
	}{
		Alias: (*Alias)(r),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	r.Cuisine = strings.ToLower(aux.Cuisine)
	r.DietaryPreference = strings.ToLower(aux.DietaryPreference)

	return nil
}
