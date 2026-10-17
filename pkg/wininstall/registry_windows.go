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

package wininstall

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

// WriteEntry writes the app's entry in Add or remove programs.
func WriteEntry(e Entry) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, UninstallKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	for name, v := range e.Strings() {
		if err := k.SetStringValue(name, v); err != nil {
			return err
		}
	}
	for name, v := range e.Numbers() {
		if err := k.SetDWordValue(name, v); err != nil {
			return err
		}
	}
	return nil
}

// DeleteEntry removes the app's entry (none is fine).
func DeleteEntry() error {
	err := registry.DeleteKey(registry.CURRENT_USER, UninstallKey)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	return err
}

// SetDisplayVersion updates the entry's version after an update, when the
// app was installed with the setup program (else there's no entry, and
// nothing is written).
func SetDisplayVersion(v string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, UninstallKey, registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue("DisplayVersion", v)
}
