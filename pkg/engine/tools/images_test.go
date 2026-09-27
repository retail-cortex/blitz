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

package tools

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/audit"
)

// The audit log proves which image was sent (path and SHA-256) without
// copying the picture into the log.
func TestLoadImageAuditsHashNotBytes(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Tools.ApprovalsFile = ""
	cfg.Images.Dir = t.TempDir()
	cfg.Images.MaxInputMB = 1
	reg, err := NewRegistry(cfg, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()
	logDir := t.TempDir()
	log, _ := audit.Open(logDir, nil)
	reg.SetAudit(log)

	var buf bytes.Buffer
	png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 16, 16)))
	os.WriteFile(filepath.Join(reg.Workspace().Dir(), "pic.png"), buf.Bytes(), 0o644)
	img, err := reg.LoadImage("pic.png")
	if err != nil {
		t.Fatal(err)
	}
	log.Close()
	files, _ := filepath.Glob(filepath.Join(logDir, "*.jsonl"))
	data, _ := os.ReadFile(files[0])
	s := string(data)
	if !strings.Contains(s, `"kind":"attachment"`) || !strings.Contains(s, "pic.png sha256="+img.SHA256) {
		t.Errorf("audit entry missing: %s", s)
	}
	if strings.Contains(s, base64.StdEncoding.EncodeToString(img.Data)[:24]) {
		t.Error("image bytes leaked into the audit log")
	}

	// The input limit applies before reading the whole file.
	os.WriteFile(filepath.Join(reg.Workspace().Dir(), "huge.png"), make([]byte, 2<<20), 0o644)
	if _, err := reg.LoadImage("huge.png"); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Errorf("size limit: %v", err)
	}
}
