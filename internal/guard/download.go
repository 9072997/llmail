package guard

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/jpennington/llmail/internal/config"
)

const (
	hfRepo  = "gravitee-io/Llama-Prompt-Guard-2-22M-onnx"
	hfBase  = "https://huggingface.co/" + hfRepo + "/resolve/main/"
	modelID = "prompt-guard-22m"
)

var requiredFiles = []string{
	"model.onnx",
	"tokenizer.json",
	"config.json",
	"special_tokens_map.json",
}

func DefaultModelPath() string {
	return filepath.Join(config.DataDir(), "models", modelID)
}

// ModelReady returns true if all required model files are present at path.
func ModelReady(path string) bool {
	if path == "" {
		path = DefaultModelPath()
	}
	for _, f := range requiredFiles {
		if _, err := os.Stat(filepath.Join(path, f)); err != nil {
			return false
		}
	}
	return true
}

// DownloadModel downloads the Prompt Guard 2 22M ONNX model from HuggingFace.
func DownloadModel(dest string, progress func(file string)) error {
	if dest == "" {
		dest = DefaultModelPath()
	}
	if err := os.MkdirAll(dest, 0755); err != nil {
		return fmt.Errorf("creating model directory: %w", err)
	}

	for _, file := range requiredFiles {
		target := filepath.Join(dest, file)
		if _, err := os.Stat(target); err == nil {
			continue // already downloaded
		}

		if progress != nil {
			progress(file)
		}

		url := hfBase + file
		if err := downloadFile(url, target); err != nil {
			return fmt.Errorf("downloading %s: %w", file, err)
		}
	}

	return nil
}

func downloadFile(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}

	tmp := dest + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}

	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}

	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}

	return os.Rename(tmp, dest)
}
