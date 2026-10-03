// Copyright 2026 Retail Cortex
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/images"
	"google.golang.org/genai"
)

// geminiFilesAPI is the part of genai's client the uploader uses.
type geminiFilesAPI interface {
	Upload(ctx context.Context, r io.Reader, cfg *genai.UploadFileConfig) (*genai.File, error)
	Get(ctx context.Context, name string, cfg *genai.GetFileConfig) (*genai.File, error)
}

// Timings of the Files API: a file is kept 48 hours (Blitz uploads again
// after 47); a video is processed before it can be used.
var (
	uploadKeep   = 47 * time.Hour
	pollEvery    = 2 * time.Second
	pollFor      = 10 * time.Minute
	nowForUpload = time.Now
)

// NewGeminiUploader sends attachments too large to go inline through
// Gemini's Files API, signed in as [llm.gemini] (spec_images_011): nil
// without a Gemini API key or key command (Vertex AI has no Files API).
func NewGeminiUploader(ctx context.Context, cfg *config.Config) (images.Uploader, error) {
	g := cfg.LLM.Gemini
	if (g.Auth != "" && g.Auth != config.AuthAPIKey) || (g.APIKey == "" && g.APIKeyCommand == "") {
		return nil, nil
	}
	cc, err := geminiClientConfig(ctx, g, policyFrom(cfg.LLM))
	if err != nil {
		return nil, err
	}
	client, err := genai.NewClient(ctx, cc)
	if err != nil {
		return nil, err
	}
	return newGeminiUploader(client.Files), nil
}

// uploaded is what's kept beside a stored file once it's uploaded:
// <sha>.gemini.json.
type uploaded struct {
	URI     string    `json:"uri"`
	MIME    string    `json:"mime_type"`
	Expires time.Time `json:"expires"`
}

func newGeminiUploader(files geminiFilesAPI) images.Uploader {
	var mu sync.Mutex // one upload of a file at a time
	return func(ctx context.Context, path, mime, sha string) (*genai.FileData, error) {
		mu.Lock()
		defer mu.Unlock()
		record := filepath.Join(filepath.Dir(path), sha+".gemini.json")
		var u uploaded
		if b, err := os.ReadFile(record); err == nil && json.Unmarshal(b, &u) == nil && nowForUpload().Before(u.Expires) && u.URI != "" {
			return &genai.FileData{FileURI: u.URI, MIMEType: mime}, nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		started := nowForUpload()
		file, err := files.Upload(ctx, f, &genai.UploadFileConfig{MIMEType: mime, DisplayName: sha[:12]})
		if err != nil {
			return nil, fmt.Errorf("uploading to Gemini: %w", err)
		}
		if file, err = waitActive(ctx, files, file); err != nil {
			return nil, err
		}
		u = uploaded{URI: file.URI, MIME: mime, Expires: started.Add(uploadKeep)}
		if b, err := json.Marshal(u); err == nil {
			os.WriteFile(record, b, 0o600) // best effort: else it goes up again next time
		}
		return &genai.FileData{FileURI: file.URI, MIMEType: mime}, nil
	}
}

// waitActive waits while Gemini processes an upload (a video takes a
// while) until it can be used.
func waitActive(ctx context.Context, files geminiFilesAPI, file *genai.File) (*genai.File, error) {
	deadline := nowForUpload().Add(pollFor)
	for {
		switch file.State {
		case genai.FileStateActive, "":
			return file, nil
		case genai.FileStateFailed:
			msg := "Gemini couldn't process the file"
			if file.Error != nil && file.Error.Message != "" {
				msg += ": " + file.Error.Message
			}
			return nil, errors.New(msg)
		}
		if nowForUpload().After(deadline) {
			return nil, fmt.Errorf("the file was still being processed by Gemini after %s", pollFor)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pollEvery):
		}
		next, err := files.Get(ctx, strings.TrimSpace(file.Name), nil)
		if err != nil {
			return nil, fmt.Errorf("checking the upload: %w", err)
		}
		file = next
	}
}
