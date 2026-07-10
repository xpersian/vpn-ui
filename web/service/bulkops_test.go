package service

import "testing"

const testNow = int64(1_000_000_000_000)

func clientMap(expiry, total int64, enable bool) map[string]any {
	// Numbers arrive as float64 from JSON decoding; mirror that here.
	return map[string]any{
		"email":      "u@t",
		"expiryTime": float64(expiry),
		"totalGB":    float64(total),
		"enable":     enable,
	}
}

func int64Of(v any) int64 { return bulkNumToInt64(v) }

func TestApplyBulkClientOp(t *testing.T) {
	const day = bulkMsPerDay
	tests := []struct {
		name       string
		expiry     int64
		total      int64
		enable     bool
		req        BulkClientUpdateRequest
		wantApply  bool
		wantExpiry int64 // checked only when the op is a day op
		wantTotal  int64 // checked only when the op is a traffic op
		wantEnable bool  // checked only when the op is enable/disable
	}{
		// addDays across the three expiryTime regimes.
		{"addDays absolute", 5000, 0, true, BulkClientUpdateRequest{Op: "addDays", Days: 2}, true, 5000 + 2*day, 0, false},
		{"addDays delayed grows", -3 * day, 0, true, BulkClientUpdateRequest{Op: "addDays", Days: 2}, true, -5 * day, 0, false},
		{"addDays no-expiry anchors now", 0, 0, true, BulkClientUpdateRequest{Op: "addDays", Days: 2}, true, testNow + 2*day, 0, false},
		{"addDays skipUnlimited skips no-expiry", 0, 0, true, BulkClientUpdateRequest{Op: "addDays", Days: 2, SkipUnlimited: true}, false, 0, 0, false},

		// subDays.
		{"subDays absolute", 10 * day, 0, true, BulkClientUpdateRequest{Op: "subDays", Days: 3}, true, 7 * day, 0, false},
		{"subDays delayed clamps at 0", -1 * day, 0, true, BulkClientUpdateRequest{Op: "subDays", Days: 3}, true, 0, 0, false},
		{"subDays no-expiry is no-op", 0, 0, true, BulkClientUpdateRequest{Op: "subDays", Days: 3}, false, 0, 0, false},

		// Traffic ops (amount in bytes).
		{"addTraffic limited", 0, 1000, true, BulkClientUpdateRequest{Op: "addTraffic", AmountBytes: 500}, true, 0, 1500, false},
		{"addTraffic converts unlimited when not skipped", 0, 0, true, BulkClientUpdateRequest{Op: "addTraffic", AmountBytes: 500}, true, 0, 500, false},
		{"addTraffic skipUnlimited skips unlimited", 0, 0, true, BulkClientUpdateRequest{Op: "addTraffic", AmountBytes: 500, SkipUnlimited: true}, false, 0, 0, false},
		{"subTraffic limited", 0, 1000, true, BulkClientUpdateRequest{Op: "subTraffic", AmountBytes: 300}, true, 0, 700, false},
		{"subTraffic floors at 1 not 0", 0, 1000, true, BulkClientUpdateRequest{Op: "subTraffic", AmountBytes: 5000}, true, 0, 1, false},
		{"subTraffic unlimited is no-op", 0, 0, true, BulkClientUpdateRequest{Op: "subTraffic", AmountBytes: 5000}, false, 0, 0, false},

		// enable / disable.
		{"enable disabled", 0, 0, false, BulkClientUpdateRequest{Op: "enable"}, true, 0, 0, true},
		{"enable already-enabled no-op", 0, 0, true, BulkClientUpdateRequest{Op: "enable"}, false, 0, 0, true},
		{"disable enabled", 0, 0, true, BulkClientUpdateRequest{Op: "disable"}, true, 0, 0, false},
		{"disable already-disabled no-op", 0, 0, false, BulkClientUpdateRequest{Op: "disable"}, false, 0, 0, false},

		// Skip toggles independent of op.
		{"skipFirstUse skips delayed", -2 * day, 100, true, BulkClientUpdateRequest{Op: "addTraffic", AmountBytes: 50, SkipFirstUse: true}, false, 0, 0, false},
		{"skipDisabled skips disabled", 5000, 100, false, BulkClientUpdateRequest{Op: "addTraffic", AmountBytes: 50, SkipDisabled: true}, false, 0, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm := clientMap(tt.expiry, tt.total, tt.enable)
			got := applyBulkClientOp(cm, tt.req, testNow)
			if got != tt.wantApply {
				t.Fatalf("apply = %v, want %v", got, tt.wantApply)
			}
			if !got {
				return // skipped: nothing should have changed
			}
			switch tt.req.Op {
			case "addDays", "subDays":
				if e := int64Of(cm["expiryTime"]); e != tt.wantExpiry {
					t.Errorf("expiryTime = %d, want %d", e, tt.wantExpiry)
				}
			case "addTraffic", "subTraffic":
				if v := int64Of(cm["totalGB"]); v != tt.wantTotal {
					t.Errorf("totalGB = %d, want %d", v, tt.wantTotal)
				}
			case "enable", "disable":
				if b, _ := cm["enable"].(bool); b != tt.wantEnable {
					t.Errorf("enable = %v, want %v", b, tt.wantEnable)
				}
			}
		})
	}
}

func TestBulkUnknownOpRejected(t *testing.T) {
	s := &InboundService{}
	_, _, err := s.BulkUpdateClients(BulkClientUpdateRequest{Op: "nuke"})
	if err == nil {
		t.Fatal("expected error for unknown op")
	}
}

// TestBulkFreezeUnfreeze covers the freeze/unfreeze ops: freeze disables + parks the
// expiry (locking the remaining time), unfreeze re-enables + resumes from now.
func TestBulkFreezeUnfreeze(t *testing.T) {
	const day = bulkMsPerDay
	cm := clientMap(testNow+10*day, 1000, true) // 10 days remaining, in use
	if !applyBulkClientOp(cm, BulkClientUpdateRequest{Op: "freeze"}, testNow) {
		t.Fatal("freeze should apply")
	}
	if en, _ := cm["enable"].(bool); en {
		t.Error("freeze should disable")
	}
	// remaining time is locked as a negative (non-ticking) value.
	if got := int64Of(cm["expiryTime"]); got != -10*day {
		t.Errorf("freeze expiry = %d, want %d (negative remaining)", got, -10*day)
	}
	if applyBulkClientOp(cm, BulkClientUpdateRequest{Op: "freeze"}, testNow) {
		t.Error("re-freeze of an already-off, non-counting account should be a no-op")
	}
	// unfreeze 3 days later -> re-enabled, expiry = later + 10 days (resume from now).
	later := testNow + 3*day
	if !applyBulkClientOp(cm, BulkClientUpdateRequest{Op: "unfreeze"}, later) {
		t.Fatal("unfreeze should apply")
	}
	if en, _ := cm["enable"].(bool); !en {
		t.Error("unfreeze should enable")
	}
	if got := int64Of(cm["expiryTime"]); got != later+10*day {
		t.Errorf("unfreeze expiry = %d, want %d", got, later+10*day)
	}
	if applyBulkClientOp(cm, BulkClientUpdateRequest{Op: "unfreeze"}, later) {
		t.Error("unfreeze of an active account should be a no-op")
	}
	// freeze a no-expiry account: just disabled, expiry stays 0 (nothing to lock).
	cm2 := clientMap(0, 0, true)
	if !applyBulkClientOp(cm2, BulkClientUpdateRequest{Op: "freeze"}, testNow) {
		t.Fatal("freeze of a no-expiry account should apply (disable)")
	}
	if en, _ := cm2["enable"].(bool); en || int64Of(cm2["expiryTime"]) != 0 {
		t.Error("freeze no-expiry: should be disabled with expiry 0")
	}
}

// TestBulkClientSkipped covers the skip toggles shared by the update ops and the new
// "delete" op: a client excluded by a toggle must not be deleted.
func TestBulkClientSkipped(t *testing.T) {
	const day = bulkMsPerDay
	tests := []struct {
		name   string
		expiry int64
		total  int64
		enable bool
		req    BulkClientUpdateRequest
		want   bool
	}{
		{"no toggles never skips", -2 * day, 0, false, BulkClientUpdateRequest{Op: "delete"}, false},
		{"skipFirstUse skips delayed start", -1 * day, 100, true, BulkClientUpdateRequest{Op: "delete", SkipFirstUse: true}, true},
		{"skipFirstUse keeps active", 5000, 100, true, BulkClientUpdateRequest{Op: "delete", SkipFirstUse: true}, false},
		{"skipDisabled skips disabled", 5000, 100, false, BulkClientUpdateRequest{Op: "delete", SkipDisabled: true}, true},
		{"skipDisabled keeps enabled", 5000, 100, true, BulkClientUpdateRequest{Op: "delete", SkipDisabled: true}, false},
		{"skipUnlimited skips unlimited", 5000, 0, true, BulkClientUpdateRequest{Op: "delete", SkipUnlimited: true}, true},
		{"skipUnlimited keeps limited", 5000, 100, true, BulkClientUpdateRequest{Op: "delete", SkipUnlimited: true}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm := clientMap(tt.expiry, tt.total, tt.enable)
			if got := bulkClientSkipped(cm, tt.req); got != tt.want {
				t.Fatalf("bulkClientSkipped = %v, want %v", got, tt.want)
			}
		})
	}
}
