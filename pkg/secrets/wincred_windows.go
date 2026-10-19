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

package secrets

import (
	"errors"
	"fmt"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ---- Windows: the Credential Manager (advapi32's Cred* calls), generic
// credentials named dev.blitz/<name>, for this user only.

var (
	advapi32       = windows.NewLazySystemDLL("advapi32.dll")
	procCredReadW  = advapi32.NewProc("CredReadW")
	procCredWriteW = advapi32.NewProc("CredWriteW")
	procCredDelete = advapi32.NewProc("CredDeleteW")
	procCredFree   = advapi32.NewProc("CredFree")
)

const (
	credTypeGeneric         = 1
	credPersistLocalMachine = 2
)

// credential is Windows' CREDENTIALW.
type credential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

// wincred keeps secrets in the Credential Manager; legacy is the file
// earlier versions kept them in, read (and emptied) as names move over.
type wincred struct {
	legacy *FileStore
}

func (wincred) Kind() string { return "Windows Credential Manager" }

func target(name string) (*uint16, error) { return windows.UTF16PtrFromString(Service + "/" + name) }

func (w wincred) Get(name string) (string, error) {
	t, err := target(name)
	if err != nil {
		return "", err
	}
	var c *credential
	r, _, callErr := procCredReadW.Call(uintptr(unsafe.Pointer(t)), credTypeGeneric, 0, uintptr(unsafe.Pointer(&c)))
	if r == 0 {
		if errors.Is(callErr, syscall.Errno(windows.ERROR_NOT_FOUND)) {
			return w.fromLegacy(name)
		}
		return "", fmt.Errorf("reading %s from the Credential Manager: %w", name, callErr)
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(c)))
	return string(unsafe.Slice(c.CredentialBlob, c.CredentialBlobSize)), nil
}

// fromLegacy moves name's secret from the file earlier versions used, if
// it's there.
func (w wincred) fromLegacy(name string) (string, error) {
	if w.legacy == nil {
		return "", ErrNotFound
	}
	v, err := w.legacy.Get(name)
	if err != nil {
		return "", err
	}
	if w.Set(name, v) == nil {
		_ = w.legacy.Delete(name)
	}
	return v, nil
}

func (wincred) Set(name, value string) error {
	t, err := target(name)
	if err != nil {
		return err
	}
	blob := []byte(value)
	c := credential{Type: credTypeGeneric, TargetName: t, Persist: credPersistLocalMachine, CredentialBlobSize: uint32(len(blob))}
	if len(blob) > 0 {
		c.CredentialBlob = &blob[0]
	}
	if r, _, callErr := procCredWriteW.Call(uintptr(unsafe.Pointer(&c)), 0); r == 0 {
		return fmt.Errorf("saving %s in the Credential Manager: %w", name, callErr)
	}
	return nil
}

func (w wincred) Delete(name string) error {
	t, err := target(name)
	if err != nil {
		return err
	}
	if r, _, callErr := procCredDelete.Call(uintptr(unsafe.Pointer(t)), credTypeGeneric, 0); r == 0 && !errors.Is(callErr, syscall.Errno(windows.ERROR_NOT_FOUND)) {
		return fmt.Errorf("removing %s from the Credential Manager: %w", name, callErr)
	}
	if w.legacy != nil {
		return w.legacy.Delete(name)
	}
	return nil
}

// osStore is Windows' store: the Credential Manager, taking over the
// file in dir.
func osStore(dir string) Store {
	return wincred{legacy: &FileStore{Path: filepath.Join(dir, "secrets.toml")}}
}
