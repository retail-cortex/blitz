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

package doctor

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/retail-cortex/blitz/pkg/enterprise"
	"github.com/retail-cortex/blitz/pkg/secrets"
)

// enterpriseTimeout bounds asking the organization's service.
const enterpriseTimeout = 10 * time.Second

// checkEnterprise is the organization's service and the sign-in to it
// (spec_053_enterprise EN-14). It reads the sign-in without renewing it:
// the Blitz service renews, and a refresh token is good once. Online, it
// asks the service whether it's reached.
func checkEnterprise(ctx context.Context, raw string, store secrets.Store, online bool) (checkStatus, string) {
	u, err := enterprise.Normalize(raw)
	if err != nil {
		return statusFail, "[enterprise] url: " + err.Error()
	}
	st := enterprise.New(u, store, enterprise.Options{}).Status()
	name := u
	if st.Info.Name != "" {
		name = fmt.Sprintf("%s (%s)", st.Info.Name, u)
	}
	reached := ""
	if online {
		ctx, cancel := context.WithTimeout(ctx, enterpriseTimeout)
		defer cancel()
		info, err := enterprise.Discover(ctx, http.DefaultClient, u)
		if err != nil {
			return statusFail, fmt.Sprintf("%s isn't reached: %v", name, err)
		}
		reached = ", reached (version " + info.Version + ")"
	}
	if !st.SignedIn {
		return statusFail, fmt.Sprintf("connected to %s%s, not signed in: blitz enterprise login", name, reached)
	}
	return statusOK, fmt.Sprintf("signed in to %s as %s%s", name, st.Person.Email, reached)
}
