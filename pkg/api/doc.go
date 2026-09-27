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

// Package api is the contract between Blitz's front ends and its engine:
// the Backend a front end drives, and the values, events and errors that
// cross it. The engine's Workspace implements Backend in process and the
// client implements it over the service's API, so the CLI, the desktop
// app and the service see the same types.
//
// It depends on nothing else in Blitz except the shared config and images
// packages, and nothing here reaches into the engine.
package api
