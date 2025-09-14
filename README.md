# Snapchef

This is the backend implementation for [Pixel to Plate](https://github.com/pa1sreepada/pixel-to-plate) Frontend. It uses visual search to identify food items from an image and generates the recipe and also can integrate with a shopping cart agentic mcp server and add the recipe to the cart. This project is built using Go, PostgreSQL and leverage LM Studio for recipe generation. 

## Running the API

### Prerequisites

- Go (1.22 or later)
- PostgreSQL
- LM Studio

### Setup

1.  **Create `config.json`:** Create a file named `config.json` in the root directory of the project with your Gemini API key:

    ```json
    {
      "gemini_api_key": "YOUR_GEMINI_API_KEY"
    }
    ```
    Replace `YOUR_GEMINI_API_KEY` with your actual Gemini API key.

2.  **Set `DATABASE_URL` environment variable:** Set the `DATABASE_URL` environment variable to your PostgreSQL connection string. For example:

    ```bash
    export DATABASE_URL="postgres://user:password@host:port/database_name?sslmode=disable"
    ```
    Replace `user`, `password`, `host`, `port`, and `database_name` with your PostgreSQL credentials.

### Build and Run

1.  **Build the application:**

    ```bash
    go build ./cmd/api
    ```

2.  **Run the application:**

    ```bash
    ./snapchef
    ```
    The API will run on `http://localhost:8080`.

## API Endpoint

### `POST /upload`

Upload an image of a food item to generate a recipe.

-   **Request:** `multipart/form-data` with a `file` field containing the image.
-   **Response:** A JSON object with the ingredients and instructions for the recipe.

### Example

```bash
curl -X POST -F "file=@/path/to/your/image.jpg" http://localhost:8080/upload
```
