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

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/retail-cortex/blitz/pkg/i18n"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// pictureFlags are `blitz picture`'s settings, as the Pictures panel's
// controls (spec_058_pictures PIC-80).
type pictureFlags struct {
	model, ratio, size, thinking, search, avoid, people, format, out, session string
	count, quality                                                            int
	seed                                                                      int32
	refs                                                                      []string
	enhance                                                                   bool
}

// newPictureCommand is `blitz picture`: a Pictures turn from the terminal,
// through the same service call as the desktop's panel, so the model's
// catalog decides what it takes (PIC-62); its pictures are saved into the
// workspace and kept as a Pictures session.
func newPictureCommand(g *globalFlags) *cobra.Command {
	var f pictureFlags
	cmd := &cobra.Command{
		Use:   "picture [prompt…]",
		Short: "Make pictures with an image model and save them in the workspace (\"-\" or a pipe reads the prompt from stdin)",
		Long: "picture makes a Pictures turn: the prompt (its words, or stdin), the model (--model, else the [images] model, else the first offered), " +
			"pictures to start from (--ref path[:character|object|style], repeatable) and the model's settings. It saves each picture (--out, else the pictures folder, " +
			"named from the prompt) and prints where, and what it cost. The turn is kept as a Pictures session: --continue <session> adds the next turn to one, " +
			"and the desktop's Pictures opens it. What the model doesn't take is refused before anything is sent.",
		RunE: func(cmd *cobra.Command, args []string) error {
			prompt, err := picturePrompt(cmd, args)
			if err != nil {
				return err
			}
			r, err := attachWorkspace(cmd.Context(), g)
			if err != nil {
				return err
			}
			req, err := f.request(cmd, r.Dir(), prompt)
			if err != nil {
				return err
			}
			if req.Model == "" {
				models, err := r.PictureModels(cmd.Context())
				if err != nil {
					return err
				}
				req.Model = defaultPictureModel(models.Models)
				if req.Model == "" {
					return withCode(exitUsage, errors.New(i18n.T("picture.cli_no_models")))
				}
			}
			res, err := r.MakePicture(cmd.Context(), req)
			if err != nil {
				return withCode(exitFailure, err)
			}
			s := res.GetSession()
			if s == nil || len(s.Turns) == 0 {
				return withCode(exitFailure, errors.New("the service kept no turn"))
			}
			turn := s.Turns[len(s.Turns)-1]
			out := cmd.OutOrStdout()
			if turn.Text != "" {
				fmt.Fprintln(out, strings.TrimSpace(turn.Text))
			}
			for i, p := range turn.Pictures {
				saved, err := r.SavePicture(cmd.Context(), s.Id, p.Id, numbered(f.out, i))
				if err != nil {
					return withCode(exitFailure, err)
				}
				fmt.Fprintln(out, saved)
			}
			fmt.Fprintln(out, i18n.T("picture.cli_done", "cost", fmt.Sprintf("$%.3f", turn.CostUsd), "session", s.Id))
			if turn.Problem != "" {
				return withCode(exitFailure, errors.New(turn.Problem))
			}
			return nil
		},
	}
	fl := cmd.Flags()
	fl.StringVarP(&f.model, "model", "m", "", "The image model, provider/model (blitz picture models lists them)")
	fl.StringVar(&f.ratio, "ratio", "", "The shape: 1:1, 16:9, 9:16, 3:2… as the model takes")
	fl.StringVar(&f.size, "size", "", "The size: 0.5K, 1K, 2K or 4K, as the model takes")
	fl.IntVarP(&f.count, "count", "n", 1, "How many pictures (Imagen and OpenAI's models make up to 4)")
	fl.StringArrayVar(&f.refs, "ref", nil, "A workspace picture to start from or match, path[:character|object|style] (repeatable)")
	fl.StringVar(&f.thinking, "thinking", "", "How much the model thinks first: MINIMAL, MEDIUM or HIGH (Gemini)")
	fl.StringVar(&f.search, "search", "", "Ground the picture in a web search: web, or image for image search too")
	fl.Lookup("search").NoOptDefVal = "web"
	fl.StringVar(&f.avoid, "avoid", "", "What to leave out of the picture (Imagen)")
	fl.StringVar(&f.people, "people", "", "People in the picture: DONT_ALLOW, ALLOW_ADULT or ALLOW_ALL")
	fl.StringVar(&f.format, "format", "", "The picture's format: image/png, image/jpeg or image/webp")
	fl.IntVar(&f.quality, "quality", 0, "JPEG quality, 1 to 100")
	fl.Int32Var(&f.seed, "seed", 0, "A seed, for the same picture again (Imagen)")
	fl.BoolVar(&f.enhance, "enhance", false, "Let the model improve the prompt (Imagen)")
	fl.StringVarP(&f.out, "out", "o", "", "Where to save the picture in the workspace (several are numbered: poster.png, poster-2.png)")
	fl.StringVar(&f.session, "continue", "", "Add the turn to this Pictures session (blitz picture list)")
	cmd.AddCommand(newPictureModelsCommand(g), newPictureListCommand(g), newPictureDeleteCommand(g))
	return cmd
}

// request is f as the service takes it, for prompt in the workspace dir.
func (f *pictureFlags) request(cmd *cobra.Command, dir, prompt string) (*pb.MakePictureRequest, error) {
	req := &pb.MakePictureRequest{Session: f.session, Model: f.model, Prompt: prompt, Count: int32(max(f.count, 1)), AspectRatio: f.ratio, Size: strings.ToUpper(f.size),
		Thinking: strings.ToUpper(f.thinking), Avoid: f.avoid, People: strings.ToUpper(f.people), Format: f.format, Quality: int32(f.quality), Enhance: f.enhance}
	switch f.search {
	case "":
	case "web":
		req.Search = true
	case "image":
		req.Search, req.SearchImages = true, true
	default:
		return nil, withCode(exitUsage, fmt.Errorf("--search is web or image, not %q", f.search))
	}
	if cmd.Flags().Changed("seed") {
		req.Seed, req.UseSeed = f.seed, true
	}
	for _, ref := range f.refs {
		p, role := ref, "object"
		if i := strings.LastIndexByte(ref, ':'); i > 0 {
			switch r := ref[i+1:]; r {
			case "character", "object", "style":
				p, role = ref[:i], r
			}
		}
		rel, err := workspacePath(dir, p)
		if err != nil {
			return nil, withCode(exitUsage, err)
		}
		req.References = append(req.References, &pb.PictureReference{Path: rel, Name: path.Base(rel), Role: role})
	}
	if f.out != "" {
		rel, err := workspacePath(dir, f.out)
		if err != nil {
			return nil, withCode(exitUsage, err)
		}
		f.out = rel
	}
	return req, nil
}

// workspacePath is p (relative to the current folder, or absolute) as a
// path in the workspace dir; one outside it is refused.
func workspacePath(dir, p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		real = dir
	}
	for _, base := range []string{dir, real} {
		if rel, err := filepath.Rel(base, abs); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return filepath.ToSlash(rel), nil
		}
	}
	return "", fmt.Errorf("%s is outside the workspace (%s)", p, dir)
}

// numbered is out for the i-th picture: out itself first, then out-2,
// out-3 beside it; "" (the pictures folder) stays "".
func numbered(out string, i int) string {
	if out == "" || i == 0 {
		return out
	}
	ext := path.Ext(out)
	return fmt.Sprintf("%s-%d%s", strings.TrimSuffix(out, ext), i+1, ext)
}

// picturePrompt is the prompt: the arguments' words, or stdin for "-" or
// none when stdin isn't a terminal.
func picturePrompt(cmd *cobra.Command, args []string) (string, error) {
	text := strings.Join(args, " ")
	in := cmd.InOrStdin()
	f, isFile := in.(*os.File)
	if text == "-" || (text == "" && (!isFile || !term.IsTerminal(int(f.Fd())))) {
		b, err := io.ReadAll(io.LimitReader(in, 1<<20))
		if err != nil {
			return "", err
		}
		text = string(b)
	}
	if text = strings.TrimSpace(text); text == "" {
		return "", withCode(exitUsage, errors.New(i18n.T("picture.cli_no_prompt")))
	}
	return text, nil
}

// defaultPictureModel is the model a turn uses without --model: the
// [images] model, else the first offered.
func defaultPictureModel(models []*pb.PictureModel) string {
	for _, m := range models {
		if m.Default {
			return m.Ref
		}
	}
	if len(models) > 0 {
		return models[0].Ref
	}
	return ""
}

// newPictureModelsCommand is `blitz picture models`: the image models the
// workspace offers, with what each makes.
func newPictureModelsCommand(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "models",
		Short: "List the image models offered, with their shapes, sizes and how many each makes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, err := attachWorkspace(cmd.Context(), g)
			if err != nil {
				return err
			}
			res, err := r.PictureModels(cmd.Context())
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(res.Models) == 0 {
				fmt.Fprintln(out, i18n.T("picture.cli_no_models"))
				return nil
			}
			tw := tabwriter.NewWriter(out, 2, 4, 2, ' ', 0)
			for _, m := range res.Models {
				mark := ""
				if m.Default {
					mark = " *"
				}
				sizes := strings.Join(m.Sizes, ", ")
				if sizes == "" {
					sizes = "-"
				}
				fmt.Fprintf(tw, "%s%s\t%s\t%s\t%s\n", m.Ref, mark, strings.Join(m.Ratios, " "), sizes, i18n.T("picture.cli_model_counts", "pictures", max(m.MaxPictures, 1), "refs", m.MaxReferences))
			}
			return tw.Flush()
		},
	}
}

// newPictureListCommand is `blitz picture list`: the workspace's Pictures
// sessions, newest first.
func newPictureListCommand(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the workspace's Pictures sessions, newest first",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, err := attachWorkspace(cmd.Context(), g)
			if err != nil {
				return err
			}
			sessions, err := r.PictureSessions(cmd.Context())
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(sessions) == 0 {
				fmt.Fprintln(out, i18n.T("picture.cli_no_sessions"))
				return nil
			}
			for _, s := range sessions {
				fmt.Fprintf(out, "%s\t%s\t%s\n", s.Id, s.Name, i18n.T("picture.cli_turns", "count", len(s.Turns)))
			}
			return nil
		},
	}
}

// newPictureDeleteCommand is `blitz picture delete <session>`: the session
// and its pictures go (not what was saved into the workspace).
func newPictureDeleteCommand(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <session>",
		Short: "Delete a Pictures session and its pictures (what was saved into the workspace stays)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := attachWorkspace(cmd.Context(), g)
			if err != nil {
				return err
			}
			if err := r.DeletePictureSession(cmd.Context(), args[0]); err != nil {
				return withCode(exitFailure, err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), i18n.T("picture.cli_deleted", "session", args[0]))
			return nil
		},
	}
}
