package cfst

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// LoadCache reads cached results from a JSON file.
func LoadCache(path string) ([]Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var results []Result
	err = json.Unmarshal(data, &results)
	if err != nil {
		return nil, err
	}
	return results, nil
}

// SaveCache writes results to a JSON file.
func SaveCache(path string, results []Result) error {
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		err := os.MkdirAll(dir, 0o755)
		if err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
