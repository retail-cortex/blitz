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
	"cmp"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/retail-cortex/blitz/pkg/images"
	"google.golang.org/genai"
)

// Pictures' errors (spec_058_pictures).
var (
	ErrNoPictureSession = errors.New("no such picture session")
	ErrNoPicture        = errors.New("no such picture")
	ErrPictureRequest   = errors.New("the model doesn't take that")
)

// DefaultPicturesFolder is where pictures are saved without [images] folder.
const DefaultPicturesFolder = "pictures"

// pictureHistoryBytes caps the pictures a conversation sends back (a
// variable, for tests).
var pictureHistoryBytes = 40 << 20

// referenceTokens are about what a picture costs as input, for estimates.
const referenceTokens = 560

// PictureModel is a model Pictures offers: what it can do, its price per
// million tokens (Gemini) and whether it's the [images] model.
type PictureModel struct {
	Ref           string // provider/model
	Info          images.Model
	InputPerMTok  float64
	OutputPerMTok float64
	Default       bool
	// Organization: through the organization's service (fewer controls).
	Organization bool
}

// PictureRef is a reference picture for a turn: a workspace file (Path),
// a picture of the session (Picture), or one added from the computer
// (Data); with its role.
type PictureRef struct {
	Path    string
	Picture string
	Data    []byte
	MIME    string
	Name    string
	Role    string
}

// PictureRequest is one turn asked of Pictures.
type PictureRequest struct {
	// Session is the session to add to ("" for a new one).
	Session      string
	Model        string
	Prompt       string
	References   []PictureRef
	Count        int
	AspectRatio  string
	Size         string
	Thinking     string
	Search       bool
	SearchImages bool
	Avoid        string
	People       string
	Seed         int32
	UseSeed      bool
	Enhance      bool
	Format       string
	Quality      int
	// StartFrom is the session's picture the turn refines ("" for none).
	StartFrom string
}

// PictureSession is a Pictures session: its turns, in order.
type PictureSession struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Workspace string        `json:"workspace"`
	Created   time.Time     `json:"created"`
	Updated   time.Time     `json:"updated"`
	Turns     []PictureTurn `json:"turns"`
}

// PictureTurn is one turn: what was asked, what came, and what it cost.
type PictureTurn struct {
	ID          string                  `json:"id"`
	At          time.Time               `json:"at"`
	Model       string                  `json:"model"`
	Kind        string                  `json:"kind"`
	Prompt      string                  `json:"prompt"`
	References  []PictureTurnRef        `json:"references,omitempty"`
	Count       int                     `json:"count,omitempty"`
	AspectRatio string                  `json:"aspect_ratio,omitempty"`
	Size        string                  `json:"size,omitempty"`
	Thinking    string                  `json:"thinking,omitempty"`
	Search      string                  `json:"search,omitempty"`
	Avoid       string                  `json:"avoid,omitempty"`
	People      string                  `json:"people,omitempty"`
	Seed        *int32                  `json:"seed,omitempty"`
	Format      string                  `json:"format,omitempty"`
	StartFrom   string                  `json:"start_from,omitempty"`
	Pictures    []PictureFile           `json:"pictures,omitempty"`
	Text        string                  `json:"text,omitempty"`
	Thoughts    string                  `json:"thoughts,omitempty"`
	Sources     []runtime.PictureSource `json:"sources,omitempty"`
	Problem     string                  `json:"problem,omitempty"`
	CostUSD     float64                 `json:"cost_usd,omitempty"`
	// Answer is the model's turn with its pictures' data left out (they're
	// the Pictures, in order), for the conversation (Gemini).
	Answer *genai.Content `json:"answer,omitempty"`
}

// PictureTurnRef is a reference as kept: its copy in the session, where it
// came from, and its role.
type PictureTurnRef struct {
	File string `json:"file"`
	MIME string `json:"mime"`
	Name string `json:"name"`
	Path string `json:"path,omitempty"`
	Role string `json:"role,omitempty"`
}

// PictureFile is a picture made, and where it was saved in the workspace.
// A deleted one keeps its place (the model's turn counts its pictures in
// order) without its file.
type PictureFile struct {
	ID      string   `json:"id"`
	File    string   `json:"file"`
	MIME    string   `json:"mime"`
	Saved   []string `json:"saved,omitempty"`
	Deleted bool     `json:"deleted,omitempty"`
}

// newPictureMaker is the picture maker for a model (a variable, for tests).
var newPictureMaker = runtime.NewPictureMaker

// pictureLocks keeps one turn at a time per session.
var pictureLocks sync.Map

// picturesDir is where the workspace's picture sessions are kept: with
// the service's sessions, by workspace (PIC-50).
func (w *Workspace) picturesDir() string {
	sum := sha256.Sum256([]byte(w.Dir()))
	return filepath.Join(w.StorageDir(), "pictures", hex.EncodeToString(sum[:8]))
}

func newPictureID(prefix string) string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

var pictureIDRE = regexp.MustCompile(`^[a-z]{1,4}-[0-9a-f]{12}$`)

// pictureSessionDir is session id's folder, checked.
func (w *Workspace) pictureSessionDir(id string) (string, error) {
	if !pictureIDRE.MatchString(id) {
		return "", fmt.Errorf("%w: %q", ErrNoPictureSession, id)
	}
	return filepath.Join(w.picturesDir(), id), nil
}

// PictureModels are the models Pictures offers (PIC-04): the [images]
// model first (whichever it is), then the catalog's with the Gemini
// provider's credentials.
func (w *Workspace) PictureModels(ctx context.Context) []PictureModel {
	var out []PictureModel
	seen := map[string]bool{}
	add := func(ref string, def bool) {
		if seen[ref] {
			return
		}
		m, err := newPictureMaker(ctx, w.cfg, ref)
		if err != nil {
			return
		}
		seen[ref] = true
		pm := PictureModel{Ref: m.Model(), Info: m.Info(), Default: def, Organization: strings.HasPrefix(m.Model(), "enterprise/")}
		prices := config.DefaultPricingAt(specNow())
		maps.Copy(prices, w.cfg.Pricing)
		if p, ok := config.PriceFor(prices, m.Info().ID); ok {
			pm.InputPerMTok, pm.OutputPerMTok = p.InputPerMTok, p.OutputPerMTok
		}
		out = append(out, pm)
	}
	if ref := strings.TrimSpace(w.cfg.Images.Model); ref != "" {
		add(ref, true)
	}
	for _, m := range images.Models() {
		add(m.ProviderOf()+"/"+m.ID, len(out) == 0)
	}
	return out
}

// PictureSessions are the workspace's picture sessions, newest first.
func (w *Workspace) PictureSessions() ([]PictureSession, error) {
	entries, err := os.ReadDir(w.picturesDir())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []PictureSession
	for _, e := range entries {
		if s, err := w.PictureSession(e.Name()); err == nil {
			out = append(out, s)
		}
	}
	slices.SortFunc(out, func(a, b PictureSession) int { return b.Updated.Compare(a.Updated) })
	return out, nil
}

// PictureSession is session id.
func (w *Workspace) PictureSession(id string) (PictureSession, error) {
	dir, err := w.pictureSessionDir(id)
	if err != nil {
		return PictureSession{}, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "session.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return PictureSession{}, fmt.Errorf("%w: %s", ErrNoPictureSession, id)
	}
	if err != nil {
		return PictureSession{}, err
	}
	var s PictureSession
	if err := json.Unmarshal(data, &s); err != nil {
		return PictureSession{}, fmt.Errorf("picture session %s: %w", id, err)
	}
	return s, nil
}

func (w *Workspace) savePictureSession(s PictureSession) error {
	dir, err := w.pictureSessionDir(s.ID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, "session.json.tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "session.json"))
}

// DeletePictureSession deletes session id and its pictures (not what was
// saved into the workspace).
func (w *Workspace) DeletePictureSession(id string) error {
	dir, err := w.pictureSessionDir(id)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("%w: %s", ErrNoPictureSession, id)
	}
	return os.RemoveAll(dir)
}

// RenamePictureSession names session id.
func (w *Workspace) RenamePictureSession(id, name string) (PictureSession, error) {
	lock := w.pictureLock(id)
	lock.Lock()
	defer lock.Unlock()
	s, err := w.PictureSession(id)
	if err != nil {
		return PictureSession{}, err
	}
	if name = strings.TrimSpace(name); name != "" {
		s.Name = name
	}
	return s, w.savePictureSession(s)
}

func (w *Workspace) pictureLock(id string) *sync.Mutex {
	l, _ := pictureLocks.LoadOrStore(w.Dir()+"\x00"+id, &sync.Mutex{})
	return l.(*sync.Mutex)
}

// ReadPicture is a picture of session id: one it made, or a reference kept
// with it (by its file name).
func (w *Workspace) ReadPicture(id, picture string) ([]byte, string, error) {
	s, err := w.PictureSession(id)
	if err != nil {
		return nil, "", err
	}
	dir, _ := w.pictureSessionDir(id)
	for _, t := range s.Turns {
		for _, p := range t.Pictures {
			if (p.ID == picture || p.File == picture) && !p.Deleted {
				data, err := os.ReadFile(filepath.Join(dir, p.File))
				return data, p.MIME, err
			}
		}
		for _, r := range t.References {
			if r.File == picture {
				data, err := os.ReadFile(filepath.Join(dir, r.File))
				return data, r.MIME, err
			}
		}
	}
	return nil, "", fmt.Errorf("%w: %s", ErrNoPicture, picture)
}

// findPicture is session s's picture id, with its turn.
func findPicture(s PictureSession, id string) (*PictureTurn, *PictureFile) {
	for i := range s.Turns {
		for j := range s.Turns[i].Pictures {
			if s.Turns[i].Pictures[j].ID == id && !s.Turns[i].Pictures[j].Deleted {
				return &s.Turns[i], &s.Turns[i].Pictures[j]
			}
		}
	}
	return nil, nil
}

// pictureRequest is r as the catalog checks it.
func pictureRequest(r PictureRequest, refs []PictureTurnRef, sizes []int) images.Request {
	out := images.Request{Prompt: r.Prompt, Count: r.Count, AspectRatio: r.AspectRatio, Size: r.Size, Thinking: r.Thinking, Search: r.Search, SearchImages: r.SearchImages,
		Avoid: r.Avoid, People: r.People, Seed: r.UseSeed, Enhance: r.Enhance, Format: r.Format, Quality: r.Quality}
	for i, ref := range refs {
		out.References = append(out.References, images.Reference{Role: ref.Role, MIME: ref.MIME, Size: sizes[i]})
	}
	return out
}

// PictureEstimate is what a turn would cost, about.
type PictureEstimate struct {
	CostUSD float64
	Priced  bool
}

// MakePicture makes r's turn (spec_058 §6): checked against the model's
// catalog entry first (PIC-62), sent with the session's conversation for a
// Gemini model (PIC-41), kept with the session (PIC-50). A turn the model
// answers without a picture is kept too, with its Problem. With estimate,
// it says only what the turn would cost (PIC-70) and keeps nothing.
func (w *Workspace) MakePicture(ctx context.Context, r PictureRequest, estimate bool) (PictureSession, PictureEstimate, error) {
	maker, err := newPictureMaker(ctx, w.cfg, r.Model)
	if err != nil {
		return PictureSession{}, PictureEstimate{}, err
	}
	info := maker.Info()
	var s PictureSession
	if r.Session != "" {
		lock := w.pictureLock(r.Session)
		lock.Lock()
		defer lock.Unlock()
		if s, err = w.PictureSession(r.Session); err != nil {
			return PictureSession{}, PictureEstimate{}, err
		}
	} else {
		now := specNow()
		s = PictureSession{ID: newPictureID("pic-"), Name: pictureName(r.Prompt), Workspace: w.Dir(), Created: now, Updated: now}
	}
	refs, datas, err := w.pictureRefs(s, r)
	if err != nil {
		return PictureSession{}, PictureEstimate{}, err
	}
	sizes := make([]int, len(datas))
	for i, d := range datas {
		sizes[i] = len(d)
	}
	if problems := info.Check(pictureRequest(r, refs, sizes)); len(problems) > 0 {
		return PictureSession{}, PictureEstimate{}, fmt.Errorf("%w: %s", ErrPictureRequest, strings.Join(problems, "; "))
	}
	call := runtime.PictureCall{Prompt: r.Prompt, Count: max(r.Count, 1), AspectRatio: r.AspectRatio, Size: r.Size, Thinking: r.Thinking, Search: r.Search, SearchImages: r.SearchImages,
		Avoid: r.Avoid, People: r.People, Enhance: r.Enhance, Format: r.Format, Quality: r.Quality}
	if r.UseSeed {
		seed := r.Seed
		call.Seed = &seed
	}
	for i, ref := range refs {
		call.References = append(call.References, tools.Reference{MIME: ref.MIME, Data: datas[i]})
	}
	if info.Kind == images.KindGemini {
		call.History = w.pictureHistory(s)
		call.User = pictureUserTurn(r.Prompt, refs, datas)
	}
	if estimate {
		return s, w.estimatePicture(maker, call), nil
	}
	dir, _ := w.pictureSessionDir(s.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return PictureSession{}, PictureEstimate{}, err
	}
	for i := range refs {
		if err := os.WriteFile(filepath.Join(dir, refs[i].File), datas[i], 0o600); err != nil {
			return PictureSession{}, PictureEstimate{}, err
		}
	}
	turn := PictureTurn{ID: newPictureID("t-"), At: specNow(), Model: maker.Model(), Kind: info.Kind, Prompt: r.Prompt, References: refs, Count: call.Count,
		AspectRatio: r.AspectRatio, Size: r.Size, Thinking: r.Thinking, Avoid: r.Avoid, People: r.People, Seed: call.Seed, Format: r.Format, StartFrom: r.StartFrom}
	if r.Search {
		turn.Search = map[bool]string{true: "image", false: "web"}[r.SearchImages]
	}
	ans, makeErr := maker.Make(ctx, call)
	if ans != nil {
		turn.Text, turn.Thoughts, turn.Sources = strings.TrimSpace(ans.Text), strings.TrimSpace(ans.Thoughts), ans.Sources
		for _, pic := range ans.Pictures {
			f := PictureFile{ID: newPictureID("p-"), MIME: pic.MIME}
			f.File = f.ID + pictureExt(pic.MIME)
			if err := os.WriteFile(filepath.Join(dir, f.File), pic.Data, 0o600); err != nil {
				return PictureSession{}, PictureEstimate{}, err
			}
			turn.Pictures = append(turn.Pictures, f)
		}
		turn.Answer = withoutPictureData(ans.Content)
		switch {
		case info.PricedPerPicture():
			turn.CostUSD = info.PerPicture * float64(len(ans.Pictures))
		case ans.Usage != nil:
			turn.CostUSD = w.engine.EstimateUsage(maker.Model(), ans.Usage).CostUSD
			w.engine.RecordImage(ctx, s.ID, maker.Model(), ans.Usage)
		}
		turn.CostUSD += info.SearchPerCall * float64(ans.Searches)
	}
	if makeErr != nil {
		if ans == nil {
			return PictureSession{}, PictureEstimate{}, makeErr
		}
		turn.Problem = makeErr.Error()
	}
	s.Turns = append(s.Turns, turn)
	s.Updated = turn.At
	if err := w.savePictureSession(s); err != nil {
		return PictureSession{}, PictureEstimate{}, err
	}
	return s, PictureEstimate{CostUSD: turn.CostUSD, Priced: true}, nil
}

// keepToolPictures keeps what generate_image drew as a Pictures session of
// one turn (spec_058 PIC-81): its prompt, settings and references, and its
// pictures marked saved where the tool wrote them, so Open in Pictures
// (PIC-54) refines them there. The model's answer isn't kept, so a Gemini
// session's next turn starts from the picture (Start from this) rather than
// continuing a conversation it never had. "" when it can't be kept.
func (w *Workspace) keepToolPictures(_ context.Context, k tools.KeptPictures) string {
	if len(k.Pictures) == 0 {
		return ""
	}
	now := specNow()
	s := PictureSession{ID: newPictureID("pic-"), Name: pictureName(k.Prompt), Workspace: w.Dir(), Created: now, Updated: now}
	dir, err := w.pictureSessionDir(s.ID)
	if err != nil {
		return ""
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ""
	}
	_, name := runtime.ParseModelRef(k.Model, "")
	kind := images.KindGemini
	if m, ok := images.Lookup(name); ok {
		kind = m.Kind
	}
	turn := PictureTurn{ID: newPictureID("t-"), At: now, Model: k.Model, Kind: kind, Prompt: k.Prompt, Count: len(k.Pictures), AspectRatio: k.AspectRatio, Size: k.Size}
	for i, ref := range k.References {
		r := PictureTurnRef{File: newPictureID("r-") + pictureExt(ref.MIME), MIME: ref.MIME, Role: images.RoleObject}
		if i < len(k.RefPaths) {
			r.Path, r.Name = k.RefPaths[i], path.Base(k.RefPaths[i])
		}
		if err := os.WriteFile(filepath.Join(dir, r.File), ref.Data, 0o600); err != nil {
			return ""
		}
		turn.References = append(turn.References, r)
	}
	for i, p := range k.Pictures {
		f := PictureFile{ID: newPictureID("p-"), MIME: p.MIME}
		f.File = f.ID + pictureExt(p.MIME)
		if err := os.WriteFile(filepath.Join(dir, f.File), p.Data, 0o600); err != nil {
			return ""
		}
		if i < len(k.Paths) {
			f.Saved = []string{k.Paths[i]}
		}
		turn.Pictures = append(turn.Pictures, f)
	}
	s.Turns = []PictureTurn{turn}
	if err := w.savePictureSession(s); err != nil {
		return ""
	}
	return s.ID
}

// pictureRefs reads r's references: a workspace file (its rules hold), a
// picture of the session, or data from the computer; StartFrom first, as
// the picture to change.
func (w *Workspace) pictureRefs(s PictureSession, r PictureRequest) ([]PictureTurnRef, [][]byte, error) {
	in := r.References
	if r.StartFrom != "" {
		in = append([]PictureRef{{Picture: r.StartFrom, Role: images.RoleObject}}, in...)
	}
	var refs []PictureTurnRef
	var datas [][]byte
	for i, ref := range in {
		var data []byte
		var name, mime string
		switch {
		case ref.Picture != "":
			d, m, err := w.ReadPicture(s.ID, ref.Picture)
			if err != nil {
				return nil, nil, err
			}
			data, mime, name = d, m, ref.Picture
		case ref.Path != "":
			rel, _, err := userPath(ref.Path)
			if err != nil {
				return nil, nil, err
			}
			d, _, err := w.files().UserReadFile(rel, images.MaxReferenceBytes+1)
			if err != nil {
				return nil, nil, err
			}
			data, name = d, path.Base(filepath.ToSlash(ref.Path))
		default:
			data, mime, name = ref.Data, ref.MIME, cmp.Or(ref.Name, fmt.Sprintf("reference-%d", i+1))
		}
		if mime == "" || mime == "application/octet-stream" {
			mime = http.DetectContentType(data)
		}
		if i := strings.IndexByte(mime, ';'); i >= 0 {
			mime = mime[:i]
		}
		id := newPictureID("r-")
		refs = append(refs, PictureTurnRef{File: id + pictureExt(mime), MIME: mime, Name: name, Path: ref.Path, Role: ref.Role})
		datas = append(datas, data)
	}
	return refs, datas, nil
}

// pictureUserTurn is the user's turn for a Gemini model: the prompt, then
// each reference after a line naming its role (the API has none: PIC-60).
func pictureUserTurn(prompt string, refs []PictureTurnRef, datas [][]byte) *genai.Content {
	parts := []*genai.Part{{Text: prompt}}
	counts := map[string]int{}
	for i, ref := range refs {
		counts[ref.Role]++
		label := fmt.Sprintf("Reference %d:", i+1)
		switch ref.Role {
		case images.RoleCharacter:
			label = fmt.Sprintf("Character reference %d (keep this person or creature the same):", counts[ref.Role])
		case images.RoleObject:
			label = fmt.Sprintf("Object reference %d (keep this thing faithful):", counts[ref.Role])
		case images.RoleStyle:
			label = fmt.Sprintf("Style reference %d (take its look):", counts[ref.Role])
		}
		parts = append(parts, &genai.Part{Text: label}, &genai.Part{InlineData: &genai.Blob{MIMEType: ref.MIME, Data: datas[i]}})
	}
	return genai.NewContentFromParts(parts, genai.RoleUser)
}

// pictureHistory is the session's conversation for a Gemini model (PIC-41):
// each earlier Gemini turn that answered, its user turn and the model's
// with their pictures put back; the oldest dropped first past the budget.
func (w *Workspace) pictureHistory(s PictureSession) []*genai.Content {
	dir, _ := w.pictureSessionDir(s.ID)
	var turns [][]*genai.Content
	var sizes []int
	for _, t := range s.Turns {
		if t.Kind != images.KindGemini || t.Answer == nil {
			continue
		}
		size := 0
		var datas [][]byte
		for _, ref := range t.References {
			d, _ := os.ReadFile(filepath.Join(dir, ref.File))
			datas = append(datas, d)
			size += len(d)
		}
		user := pictureUserTurn(t.Prompt, t.References, datas)
		answer := &genai.Content{Role: genai.RoleModel}
		pic := 0
		for _, part := range t.Answer.Parts {
			p := *part
			if p.InlineData != nil {
				// A draft the model drew while thinking isn't kept (nor one of
				// the turn's pictures): it goes back without it.
				if p.Thought || pic >= len(t.Pictures) {
					continue
				}
				f := t.Pictures[pic]
				pic++
				if f.Deleted {
					continue
				}
				d, err := os.ReadFile(filepath.Join(dir, f.File))
				if err != nil || len(d) == 0 {
					continue
				}
				p.InlineData = &genai.Blob{MIMEType: p.InlineData.MIMEType, Data: d}
				size += len(d)
			}
			answer.Parts = append(answer.Parts, &p)
		}
		turns = append(turns, []*genai.Content{user, answer})
		sizes = append(sizes, size)
	}
	total := 0
	for _, n := range sizes {
		total += n
	}
	for len(turns) > 0 && total > pictureHistoryBytes {
		total -= sizes[0]
		turns, sizes = turns[1:], sizes[1:]
	}
	var out []*genai.Content
	for _, t := range turns {
		out = append(out, t...)
	}
	return out
}

// withoutPictureData is the model's turn with its pictures' data left out
// (kept as files), for the session.
func withoutPictureData(c *genai.Content) *genai.Content {
	if c == nil {
		return nil
	}
	out := &genai.Content{Role: c.Role}
	for _, part := range c.Parts {
		if part == nil {
			continue
		}
		p := *part
		if p.InlineData != nil {
			if p.Thought {
				continue // a draft drawn while thinking: not one of the pictures
			}
			p.InlineData = &genai.Blob{MIMEType: p.InlineData.MIMEType}
		}
		out.Parts = append(out.Parts, &p)
	}
	return out
}

// estimatePicture is what call would cost, about (PIC-70): Imagen's price
// per picture; for Gemini the input (text, references, history) and the
// pictures' output tokens at the model's prices.
func (w *Workspace) estimatePicture(m runtime.PictureMaker, call runtime.PictureCall) PictureEstimate {
	info := m.Info()
	if info.PricedPerPicture() {
		return PictureEstimate{CostUSD: info.PerPicture * float64(max(call.Count, 1)), Priced: info.PerPicture > 0}
	}
	in := images.EstimateTokens(call.Prompt) + referenceTokens*len(call.References)
	for _, c := range call.History {
		for _, p := range c.Parts {
			switch {
			case p.InlineData != nil:
				in += referenceTokens
			default:
				in += images.EstimateTokens(p.Text)
			}
		}
	}
	u := w.engine.EstimateUsage(m.Model(), &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: int32(in), CandidatesTokenCount: int32(info.OutputTokens(call.Size) + 200)})
	if call.Search {
		u.CostUSD += info.SearchPerCall
	}
	return PictureEstimate{CostUSD: u.CostUSD, Priced: u.Priced}
}

// pictureExt is a picture's extension for its type.
func pictureExt(mime string) string {
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	}
	return ".png"
}

var nameWordsRE = regexp.MustCompile(`[^\p{L}\p{N}]+`)

// pictureName is a session's name from its first prompt.
func pictureName(prompt string) string {
	words := strings.Fields(strings.TrimSpace(prompt))
	if len(words) > 8 {
		words = append(words[:8], "…")
	}
	return cmp.Or(strings.Join(words, " "), "Pictures")
}

// pictureSlug is a file name's start from a prompt: its first words,
// lowercase, joined by -.
func pictureSlug(prompt string) string {
	words := strings.Fields(strings.ToLower(nameWordsRE.ReplaceAllString(prompt, " ")))
	if len(words) > 5 {
		words = words[:5]
	}
	return cmp.Or(strings.Join(words, "-"), "picture")
}

// PicturesFolder is where pictures are saved: [images] folder, else
// pictures.
func (w *Workspace) PicturesFolder() string {
	return cmp.Or(strings.Trim(strings.TrimSpace(w.cfg.Images.Folder), "/"), DefaultPicturesFolder)
}

// SavePicture writes session id's picture into the workspace (PIC-51):
// to p when given (a folder, ending in /, takes a name), else to folder (or
// [images] folder, else pictures/), named from its prompt and numbered,
// never overwriting. It returns the path written.
func (w *Workspace) SavePicture(ctx context.Context, id, picture, p string) (string, error) {
	lock := w.pictureLock(id)
	lock.Lock()
	defer lock.Unlock()
	s, err := w.PictureSession(id)
	if err != nil {
		return "", err
	}
	turn, pic := findPicture(s, picture)
	if pic == nil {
		return "", fmt.Errorf("%w: %s", ErrNoPicture, picture)
	}
	dir, _ := w.pictureSessionDir(id)
	data, err := os.ReadFile(filepath.Join(dir, pic.File))
	if err != nil {
		return "", err
	}
	ext := pictureExt(pic.MIME)
	folder, name := w.PicturesFolder(), ""
	if p = strings.TrimSpace(filepath.ToSlash(p)); p != "" {
		if strings.HasSuffix(p, "/") {
			folder = strings.TrimSuffix(p, "/")
		} else {
			folder, name = path.Dir(p), path.Base(p)
			if path.Ext(name) == "" {
				name += ext
			}
		}
	}
	if folder == "." {
		folder = ""
	}
	rel := ""
	exists := func(rel string) bool { _, err := w.files().UserStat(filepath.FromSlash(rel)); return err == nil }
	if name != "" {
		rel = path.Join(folder, name)
		if exists(rel) {
			return "", fmt.Errorf("%s: %w", rel, fs.ErrExist)
		}
	} else {
		base := pictureSlug(turn.Prompt)
		for n := 1; ; n++ {
			rel = path.Join(folder, fmt.Sprintf("%s-%d%s", base, n, ext))
			if !exists(rel) {
				break
			}
		}
	}
	if folder != "" {
		if err := w.files().UserMkdir(filepath.FromSlash(folder)); err != nil && !errors.Is(err, fs.ErrExist) {
			return "", err
		}
	}
	err = w.files().UserWriteFile(ctx, filepath.FromSlash(rel), data, func(_ []byte, exists bool) error {
		if exists {
			return fmt.Errorf("%s: %w", rel, fs.ErrExist)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	pic.Saved = append(pic.Saved, rel)
	s.Updated = specNow()
	return rel, w.savePictureSession(s)
}

// DeletePicture removes a picture from session id (its saved copies stay).
func (w *Workspace) DeletePicture(id, picture string) (PictureSession, error) {
	lock := w.pictureLock(id)
	lock.Lock()
	defer lock.Unlock()
	s, err := w.PictureSession(id)
	if err != nil {
		return PictureSession{}, err
	}
	_, pic := findPicture(s, picture)
	if pic == nil {
		return PictureSession{}, fmt.Errorf("%w: %s", ErrNoPicture, picture)
	}
	dir, _ := w.pictureSessionDir(id)
	_ = os.Remove(filepath.Join(dir, pic.File))
	pic.Deleted = true
	return s, w.savePictureSession(s)
}
