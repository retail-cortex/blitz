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

package server

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/engine"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// languageService implements LanguageService over the workspace's
// language servers (spec_visual_editor_037).
type languageService struct{ s *Server }

// languageError maps the language servers' errors to Connect codes and
// reasons.
func languageError(err error) error {
	var missing *engine.ServerNotInstalledError
	switch {
	case errors.Is(err, engine.ErrUnknownDocument):
		return apiError(connect.CodeNotFound, "UNKNOWN_DOCUMENT", err)
	case errors.Is(err, engine.ErrStaleDocument):
		return apiError(connect.CodeAborted, "STALE", err)
	case errors.Is(err, engine.ErrUntrusted):
		return apiError(connect.CodeFailedPrecondition, "UNTRUSTED", err)
	case errors.Is(err, engine.ErrNoLanguageServer):
		return apiError(connect.CodeFailedPrecondition, "NO_SERVER", err)
	case errors.As(err, &missing):
		return apiError(connect.CodeFailedPrecondition, "SERVER_MISSING", err, "command", missing.Command)
	case errors.Is(err, engine.ErrBadPath):
		return apiError(connect.CodeInvalidArgument, "BAD_PATH", err)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return toAPI(err)
	}
	var ce *connect.Error
	if errors.As(err, &ce) {
		return ce
	}
	return apiError(connect.CodeUnavailable, "SERVER_FAILED", err)
}

var serverStates = map[engine.LanguageState]pb.ServerState{
	engine.LanguageReady:     pb.ServerState_SERVER_STATE_READY,
	engine.LanguageStarting:  pb.ServerState_SERVER_STATE_STARTING,
	engine.LanguageMissing:   pb.ServerState_SERVER_STATE_MISSING,
	engine.LanguageFailed:    pb.ServerState_SERVER_STATE_FAILED,
	engine.LanguageUntrusted: pb.ServerState_SERVER_STATE_UNTRUSTED,
	engine.LanguageNone:      pb.ServerState_SERVER_STATE_NONE,
	engine.LanguageIdle:      pb.ServerState_SERVER_STATE_IDLE,
}

func (h languageService) OpenDocument(ctx context.Context, r req[pb.OpenDocumentRequest]) (*connect.Response[pb.OpenDocumentResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	var block *engine.CodeBlock
	if b := r.Msg.Block; b != nil {
		block = &engine.CodeBlock{Extension: b.Extension, ID: b.Id}
	}
	info, err := w.OpenDocument(r.Msg.Client, r.Msg.Path, r.Msg.Text, r.Msg.Version, block)
	if err != nil {
		return nil, languageError(err)
	}
	return ok(&pb.OpenDocumentResponse{Document: info.ID, Language: info.Language, State: serverStates[info.State], Detail: info.Detail, Install: info.Install})
}

func (h languageService) ChangeDocument(ctx context.Context, r req[pb.ChangeDocumentRequest]) (*connect.Response[pb.ChangeDocumentResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if err := w.ChangeDocument(ctx, r.Msg.Document, r.Msg.Version, r.Msg.Text); err != nil {
		return nil, languageError(err)
	}
	return ok(&pb.ChangeDocumentResponse{})
}

func (h languageService) CloseDocument(ctx context.Context, r req[pb.CloseDocumentRequest]) (*connect.Response[pb.CloseDocumentResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if err := w.CloseDocument(r.Msg.Document); err != nil && !errors.Is(err, engine.ErrUnknownDocument) {
		return nil, languageError(err) // closing one already gone is fine
	}
	return ok(&pb.CloseDocumentResponse{})
}

func (h languageService) KeepDocuments(ctx context.Context, r req[pb.KeepDocumentsRequest]) (*connect.Response[pb.KeepDocumentsResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	return ok(&pb.KeepDocumentsResponse{Documents: w.KeepDocuments(r.Msg.Client)})
}

func textPos(p engine.TextPosition) *pb.TextPosition {
	return &pb.TextPosition{Line: int32(p.Line), Column: int32(p.Column)}
}

func textEditMsg(e engine.TextEdit) *pb.TextEdit {
	return &pb.TextEdit{Range: &pb.TextRange{Start: textPos(e.Start), End: textPos(e.End)}, Text: e.Text}
}

func (h languageService) Complete(ctx context.Context, r req[pb.CompleteRequest]) (*connect.Response[pb.CompleteResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	p := r.Msg.Position
	list, err := w.Complete(ctx, r.Msg.Document, r.Msg.Version, int(p.GetLine()), int(p.GetColumn()), r.Msg.Trigger)
	if err != nil {
		return nil, languageError(err)
	}
	res := &pb.CompleteResponse{Incomplete: list.Incomplete}
	for _, c := range list.Items {
		item := &pb.CompletionItem{Label: c.Label, Kind: c.Kind, Detail: c.Detail, Documentation: c.Documentation,
			InsertText: c.InsertText, Snippet: c.Snippet, SortText: c.SortText, FilterText: c.FilterText}
		if c.Edit != nil {
			item.Edit = textEditMsg(*c.Edit)
		}
		for _, e := range c.AdditionalEdits {
			item.AdditionalEdits = append(item.AdditionalEdits, textEditMsg(e))
		}
		res.Items = append(res.Items, item)
	}
	return ok(res)
}

func (h languageService) Hover(ctx context.Context, r req[pb.HoverRequest]) (*connect.Response[pb.HoverResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	p := r.Msg.Position
	text, err := w.DocumentHover(ctx, r.Msg.Document, r.Msg.Version, int(p.GetLine()), int(p.GetColumn()))
	if err != nil {
		return nil, languageError(err)
	}
	return ok(&pb.HoverResponse{Text: text})
}

func locationMsgs(locs []engine.SourceLocation) []*pb.SourceLocation {
	out := make([]*pb.SourceLocation, len(locs))
	for i, l := range locs {
		out[i] = &pb.SourceLocation{Path: l.Path, Line: int32(l.Line), Column: int32(l.Column), Text: l.Text, Outside: l.Outside}
	}
	return out
}

func (h languageService) Definition(ctx context.Context, r req[pb.DefinitionRequest]) (*connect.Response[pb.DefinitionResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	p := r.Msg.Position
	locs, err := w.DocumentDefinition(ctx, r.Msg.Document, r.Msg.Version, int(p.GetLine()), int(p.GetColumn()))
	if err != nil {
		return nil, languageError(err)
	}
	return ok(&pb.DefinitionResponse{Locations: locationMsgs(locs)})
}

func (h languageService) References(ctx context.Context, r req[pb.ReferencesRequest]) (*connect.Response[pb.ReferencesResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	p := r.Msg.Position
	locs, err := w.DocumentReferences(ctx, r.Msg.Document, r.Msg.Version, int(p.GetLine()), int(p.GetColumn()))
	if err != nil {
		return nil, languageError(err)
	}
	return ok(&pb.ReferencesResponse{Locations: locationMsgs(locs)})
}

func (h languageService) WatchDiagnostics(ctx context.Context, r req[pb.WatchDiagnosticsRequest], stream *connect.ServerStream[pb.WatchDiagnosticsResponse]) error {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return err
	}
	err = w.WatchDiagnostics(ctx, r.Msg.Client, func(d engine.DocumentDiagnostics) error {
		msg := &pb.WatchDiagnosticsResponse{Document: d.Document, Version: d.Version}
		for _, x := range d.Diagnostics {
			msg.Diagnostics = append(msg.Diagnostics, &pb.LanguageDiagnostic{
				Range:    &pb.TextRange{Start: textPos(x.Start), End: textPos(x.End)},
				Severity: x.Severity, Message: x.Message, Source: x.Source, Code: x.Code,
			})
		}
		return stream.Send(msg)
	})
	if errors.Is(err, context.Canceled) {
		return nil // the client went away
	}
	return err
}

func (h languageService) GetLanguageStatus(ctx context.Context, r req[pb.GetLanguageStatusRequest]) (*connect.Response[pb.GetLanguageStatusResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	res := &pb.GetLanguageStatusResponse{Untrusted: !w.LanguageTrusted()}
	for _, st := range w.LanguageStatus() {
		res.Servers = append(res.Servers, &pb.LanguageServerStatus{
			Language: st.Language, Command: st.Command, Extensions: st.Extensions,
			State: serverStates[st.State], Since: timestamp(st.Since), Error: st.Err, Install: st.Install,
		})
	}
	return ok(res)
}

func (h languageService) RestartLanguageServer(ctx context.Context, r req[pb.RestartLanguageServerRequest]) (*connect.Response[pb.RestartLanguageServerResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if err := w.RestartLanguageServer(r.Msg.Language); err != nil {
		return nil, languageError(err)
	}
	return ok(&pb.RestartLanguageServerResponse{})
}
