// Copyright 2024-2025 NetCracker Technology Corporation
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

package util

import "testing"

func TestShouldLoadPgFailoverSlots(t *testing.T) {
	tests := []struct {
		name      string
		pgVersion int64
		want      bool
	}{
		{name: "postgres 9.6 major", pgVersion: 9, want: false},
		{name: "postgres 10", pgVersion: 10, want: true},
		{name: "postgres 15", pgVersion: 15, want: true},
		{name: "postgres 16", pgVersion: 16, want: true},
		{name: "postgres 17 uses native sync slots", pgVersion: 17, want: false},
		{name: "postgres 18", pgVersion: 18, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShouldLoadPgFailoverSlots(tt.pgVersion); got != tt.want {
				t.Fatalf("ShouldLoadPgFailoverSlots(%d) = %v, want %v", tt.pgVersion, got, tt.want)
			}
		})
	}
}
