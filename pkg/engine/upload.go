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

package engine

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"sync"
	"time"
)

// MaxUpload bounds a file uploaded with UploadFile.
const MaxUpload = 4 << 30

// uploadIdle is how long an unfinished upload is kept without a chunk.
var uploadIdle = 10 * time.Minute

// ErrNoUpload means an upload id isn't one in progress: never started,
// finished, failed, or dropped after uploadIdle without a chunk.
var ErrNoUpload = errors.New("no such upload")

// ErrUploadTooLarge means an upload went over MaxUpload.
var ErrUploadTooLarge = fmt.Errorf("file over %d GiB", MaxUpload>>30)

// An upload in progress: the hidden file beside its path that its chunks
// go to, and the hash of what came so far (the file's version when done).
// mu keeps its chunks in order.
type upload struct {
	mu        sync.Mutex
	id        string
	path, rel string
	tmp       string
	size      int64
	sum       hash.Hash
	seen      time.Time
}

// UploadFile writes a file the user dropped into the Files view in
// chunks, since a request carries at most 32 MiB (FIL-22). A call with
// id "" starts it; each call adds data; the call with done puts the file
// at p, which must not exist unless replace (a folder is never
// replaced). It returns the upload's id and, when done, the file's
// version. A failed call ends the upload; one left ten minutes without a
// chunk is dropped.
func (w *Workspace) UploadFile(ctx context.Context, p, id string, data []byte, done, replace bool) (string, string, error) {
	rel, slash, err := userPath(p)
	if err != nil {
		return "", "", err
	}
	w.dropUploads(time.Now().Add(-uploadIdle))
	u, err := w.uploadFor(id, slash, rel)
	if err != nil {
		return "", "", err
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.size+int64(len(data)) > MaxUpload {
		w.endUpload(u, true)
		return "", "", fmt.Errorf("%s: %w", slash, ErrUploadTooLarge)
	}
	if err := w.files().UserAppend(u.tmp, data); err != nil {
		w.endUpload(u, true)
		return "", "", err
	}
	u.size += int64(len(data))
	u.sum.Write(data)
	if !done {
		return u.id, "", nil
	}
	err = w.files().UserFinishUpload(ctx, u.tmp, u.rel, replace)
	w.endUpload(u, err != nil)
	if err != nil {
		return "", "", err
	}
	w.userEdited(slash, "created")
	return u.id, hex.EncodeToString(u.sum.Sum(nil)), nil
}

// uploadFor is the upload id, which must be path slash's, or a new one
// when id is "".
func (w *Workspace) uploadFor(id, slash, rel string) (*upload, error) {
	now := time.Now()
	if id != "" {
		w.uploadMu.Lock()
		defer w.uploadMu.Unlock()
		u, ok := w.uploads[id]
		if !ok || u.path != slash {
			return nil, fmt.Errorf("%w: %s", ErrNoUpload, id)
		}
		u.seen = now
		return u, nil
	}
	tmp, err := w.files().UserCreateUpload(rel)
	if err != nil {
		return nil, err
	}
	var b [12]byte
	_, _ = rand.Read(b[:])
	u := &upload{id: hex.EncodeToString(b[:]), path: slash, rel: rel, tmp: tmp, sum: sha256.New(), seen: now}
	w.uploadMu.Lock()
	if w.uploads == nil {
		w.uploads = map[string]*upload{}
	}
	w.uploads[u.id] = u
	w.uploadMu.Unlock()
	return u, nil
}

// endUpload forgets u, removing its hidden file when discard.
func (w *Workspace) endUpload(u *upload, discard bool) {
	w.uploadMu.Lock()
	delete(w.uploads, u.id)
	w.uploadMu.Unlock()
	if discard {
		w.files().UserDiscardUpload(u.tmp)
	}
}

// dropUploads discards the uploads last added to before cutoff; the zero
// time drops them all.
func (w *Workspace) dropUploads(cutoff time.Time) {
	w.uploadMu.Lock()
	var stale []*upload
	for id, u := range w.uploads {
		if cutoff.IsZero() || u.seen.Before(cutoff) {
			stale = append(stale, u)
			delete(w.uploads, id)
		}
	}
	w.uploadMu.Unlock()
	for _, u := range stale {
		w.files().UserDiscardUpload(u.tmp)
	}
}
