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

package client

import (
	"context"

	"connectrpc.com/connect"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// Branding is what the organization says about itself (spec_053 EN-52):
// its name, about text, support link and documents. The zero value: none
// published (Blitz's own look).
type Branding struct {
	Name, ShortName, Support, About string
	Documents                       []OrgDocument
}

// OrgDocument is one of the organization's documents: its terms of
// service ("terms") or privacy notice ("privacy"), as text or a link.
type OrgDocument struct {
	Document, Version, Text, URL string
}

// Branding is the organization's branding (ConfigService.GetBranding),
// asking its service first when refresh is set; why is the reason its service
// couldn't be asked then (the kept copy is answered all the same).
func (s *Settings) Branding(ctx context.Context, refresh bool) (b Branding, why string, err error) {
	res, err := s.c.GetBranding(ctx, connect.NewRequest(&pb.GetBrandingRequest{Refresh: refresh}))
	if err != nil {
		return Branding{}, "", fromAPI(err)
	}
	m := res.Msg.GetBranding()
	b = Branding{Name: m.GetName(), ShortName: m.GetShortName(), Support: m.GetSupport(), About: m.GetAbout()}
	for _, d := range m.GetDocuments() {
		b.Documents = append(b.Documents, OrgDocument{Document: d.GetDocument(), Version: d.GetVersion(), Text: d.GetText(), URL: d.GetUrl()})
	}
	return b, res.Msg.GetError(), nil
}
