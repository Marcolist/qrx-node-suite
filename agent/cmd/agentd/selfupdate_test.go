package main

import (
	"testing"

	"qrx-node-suite/agent/storage"
)

func TestSelfUpdateRollbackRequiresRestart(t *testing.T) {
	cases := []struct {
		name      string
		component string
		rec       *storage.UpdateHistoryRecord
		want      bool
	}{
		{
			name:      "agent rolled back requires restart",
			component: "agent",
			rec:       &storage.UpdateHistoryRecord{Status: storage.UpdateStatusRolledBack},
			want:      true,
		},
		{
			name:      "agent succeeded does not require restart",
			component: "agent",
			rec:       &storage.UpdateHistoryRecord{Status: storage.UpdateStatusSucceeded},
			want:      false,
		},
		{
			name:      "adapter rolled back does not require restart -- adapter code is compiled into this same binary",
			component: "adapter_qrx007",
			rec:       &storage.UpdateHistoryRecord{Status: storage.UpdateStatusRolledBack},
			want:      false,
		},
		{
			name:      "nil record (nothing was resumed) does not require restart",
			component: "agent",
			rec:       nil,
			want:      false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := selfUpdateRollbackRequiresRestart(tc.component, tc.rec)
			if got != tc.want {
				t.Errorf("selfUpdateRollbackRequiresRestart(%q, %+v) = %v, want %v", tc.component, tc.rec, got, tc.want)
			}
		})
	}
}
