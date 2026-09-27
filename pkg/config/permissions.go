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

package config

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// SavePermissionRules writes one [permissions] list (allow, ask or deny)
// in dir/.env.toml, replacing that key's line only; an empty list removes
// it. Comments and other settings are kept.
func SavePermissionRules(dir, effect string, rules []string) (string, error) {
	switch effect {
	case "allow", "ask", "deny":
	default:
		return "", fmt.Errorf("unknown permission effect %q", effect)
	}
	quoted := make([]string, len(rules))
	for i, r := range rules {
		quoted[i] = strconv.Quote(r)
	}
	return editConfigFile(dir,
		func(doc string) string {
			if len(rules) == 0 {
				return removeTOMLKey(doc, "permissions", effect)
			}
			return setTOMLKey(doc, "permissions", effect, "["+strings.Join(quoted, ", ")+"]")
		},
		func(check map[string]any) error {
			perms, _ := check["permissions"].(map[string]any)
			var got []string
			if list, ok := perms[effect].([]any); ok {
				for _, v := range list {
					s, _ := v.(string)
					got = append(got, s)
				}
			}
			if !slices.Equal(got, rules) {
				return errors.New("could not update [permissions] " + effect)
			}
			return nil
		})
}
