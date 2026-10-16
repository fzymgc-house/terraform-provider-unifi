package unifi

import (
	"context"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestPortsMatchPlan(t *testing.T) {
	named := func(name string) types.Object {
		return portObject(t, map[string]attr.Value{"name": types.StringValue(name)})
	}
	withUnknownOpMode := func(name string) types.Object {
		return portObject(t, map[string]attr.Value{
			"name":    types.StringValue(name),
			"op_mode": types.StringUnknown(),
		})
	}

	cases := []struct {
		name         string
		planned, got map[string]attr.Value
		want         bool
	}{
		{
			name:    "same ports and values",
			planned: map[string]attr.Value{"21": named("wan2-gateway")},
			got:     map[string]attr.Value{"21": named("wan2-gateway")},
			want:    true,
		},
		{
			name:    "stale value",
			planned: map[string]attr.Value{"21": named("wan2-gateway")},
			got:     map[string]attr.Value{"21": named("BackupNet")},
			want:    false,
		},
		{
			name:    "removed port still present",
			planned: map[string]attr.Value{"21": named("wan2-gateway")},
			got: map[string]attr.Value{
				"21": named("wan2-gateway"),
				"23": named("firewall-uplink"),
			},
			want: false,
		},
		{
			name:    "planned port missing",
			planned: map[string]attr.Value{"13": named("ap-office")},
			got:     map[string]attr.Value{"14": named("ap-office")},
			want:    false,
		},
		{
			name:    "unknown planned attribute matches any value",
			planned: map[string]attr.Value{"21": withUnknownOpMode("wan2-gateway")},
			got: map[string]attr.Value{"21": portObject(t, map[string]attr.Value{
				"name":    types.StringValue("wan2-gateway"),
				"op_mode": types.StringValue("switch"),
			})},
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := portsMatchPlan(portMap(t, tc.planned), portMap(t, tc.got))
			if got != tc.want {
				t.Errorf("portsMatchPlan = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSettle_ReadsUntilThePlanMatches(t *testing.T) {
	restore := setSettle(5, time.Millisecond)
	defer restore()

	planned := portMap(t, map[string]attr.Value{
		"21": portObject(t, map[string]attr.Value{"name": types.StringValue("wan2-gateway")}),
	})
	stale := portMap(t, map[string]attr.Value{
		"21": portObject(t, map[string]attr.Value{"name": types.StringValue("BackupNet")}),
	})

	reads := 0
	diags := settle(context.Background(), planned, func() (types.Map, diag.Diagnostics) {
		reads++
		if reads < 3 {
			return stale, nil
		}
		return planned, nil
	})
	if diags.HasError() {
		t.Fatal(diags)
	}
	if reads != 3 {
		t.Errorf("reads = %d, want 3: settle must stop at the first read that matches", reads)
	}
}

func TestSettle_StopsAfterTheLastAttempt(t *testing.T) {
	restore := setSettle(4, time.Millisecond)
	defer restore()

	planned := portMap(t, map[string]attr.Value{
		"21": portObject(t, map[string]attr.Value{"name": types.StringValue("wan2-gateway")}),
	})
	stale := portMap(t, map[string]attr.Value{
		"21": portObject(t, map[string]attr.Value{"name": types.StringValue("BackupNet")}),
	})

	reads := 0
	settle(context.Background(), planned, func() (types.Map, diag.Diagnostics) {
		reads++
		return stale, nil
	})
	if reads != 4 {
		t.Errorf("reads = %d, want 4", reads)
	}
}

func TestSettle_StopsOnAReadError(t *testing.T) {
	restore := setSettle(5, time.Millisecond)
	defer restore()

	reads := 0
	diags := settle(
		context.Background(),
		types.MapNull(types.ObjectType{AttrTypes: devicePortAttrTypes()}),
		func() (types.Map, diag.Diagnostics) {
			reads++
			var d diag.Diagnostics
			d.AddError("Unable to read device", "boom")
			return types.MapNull(types.ObjectType{AttrTypes: devicePortAttrTypes()}), d
		},
	)
	if !diags.HasError() {
		t.Error("the read error must reach the caller")
	}
	if reads != 1 {
		t.Errorf("reads = %d, want 1", reads)
	}
}

func setSettle(attempts int, interval time.Duration) func() {
	a, i := settleAttempts, settleInterval
	settleAttempts, settleInterval = attempts, interval
	return func() { settleAttempts, settleInterval = a, i }
}
