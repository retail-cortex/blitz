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

package workers

import "github.com/retail-cortex/blitz/pkg/jobspec"

// Schedule is when a worker runs.
type Schedule = jobspec.Schedule

// ParseSchedule understands a schedule, as jobspec.ParseSchedule.
func ParseSchedule(text, tz string) (Schedule, error) { return jobspec.ParseSchedule(text, tz) }
