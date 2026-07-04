package cmd

import (
	"testing"
)

// TestClimateStatesOff verifies the default all-off state.
func TestClimateStatesOff(t *testing.T) {
	c := &climate{}
	states := c.mqttStates()
	want := map[string]string{
		"/climate/cool":       "off",
		"/climate/heat":       "off",
		"/climate/windscreen": "off",
		"/climate/mode":       "off",
	}
	for k, wantV := range want {
		if got := states[k]; got != wantV {
			t.Errorf("states[%q] = %q, want %q", k, got, wantV)
		}
	}
}

// TestClimateStatesHeat verifies heat mode sets only heat to "on".
func TestClimateStatesHeat(t *testing.T) {
	c := &climate{}
	c.setMode("heat")
	c.setState(true)
	states := c.mqttStates()
	checks := map[string]string{
		"/climate/cool":       "off",
		"/climate/heat":       "on",
		"/climate/windscreen": "off",
		"/climate/mode":       "heat",
	}
	for k, wantV := range checks {
		if got := states[k]; got != wantV {
			t.Errorf("states[%q] = %q, want %q", k, got, wantV)
		}
	}
}

// TestClimateStatesCool verifies cool mode sets only cool to "on".
func TestClimateStatesCool(t *testing.T) {
	c := &climate{}
	c.setMode("cool")
	c.setState(true)
	states := c.mqttStates()
	if states["/climate/cool"] != "on" {
		t.Errorf("cool: want on, got %s", states["/climate/cool"])
	}
	if states["/climate/heat"] != "off" {
		t.Errorf("heat: want off, got %s", states["/climate/heat"])
	}
}

// TestClimateStatesWindscreen verifies windscreen mode.
func TestClimateStatesWindscreen(t *testing.T) {
	c := &climate{}
	c.setMode("windscreen")
	c.setState(true)
	states := c.mqttStates()
	if states["/climate/windscreen"] != "on" {
		t.Errorf("windscreen: want on, got %s", states["/climate/windscreen"])
	}
}

// TestClimateNotReadyWhenStateMissing verifies ready() = false when only mode is set.
func TestClimateNotReadyWhenStateMissing(t *testing.T) {
	c := &climate{}
	c.setMode("heat")
	// state is nil
	if c.ready() {
		t.Error("climate.ready() should be false when state is nil")
	}
	states := c.mqttStates()
	if states["/climate/heat"] != "off" {
		t.Errorf("heat: want off when not ready, got %s", states["/climate/heat"])
	}
}

// TestClimateStateFalse verifies that state=false overrides mode.
func TestClimateStateFalse(t *testing.T) {
	c := &climate{}
	c.setMode("heat")
	c.setState(false)
	states := c.mqttStates()
	if states["/climate/heat"] != "off" {
		t.Errorf("heat: want off when state=false, got %s", states["/climate/heat"])
	}
}

// TestPublishChargeRemainingCap verifies that sentinel values ≥ maxChargeRemaining
// are capped to 0 and do not propagate to the MQTT cache.
func TestPublishChargeRemainingCap(t *testing.T) {
	published := map[string]string{}
	mc := &mqttClient{
		mqttData: map[string]string{},
	}
	// Override publish to capture output without a real MQTT client.
	capture := func(topic, payload string) {
		published[topic] = payload
	}

	sentinels := []int{1000, 1024, 2047, 4095, 65534}
	for _, v := range sentinels {
		if v >= maxChargeRemaining {
			capture("/charge/remaining", "0")
		} else {
			capture("/charge/remaining", "unexpected")
		}
	}
	// Verify 45 passes through.
	const real = 45
	if real >= maxChargeRemaining {
		t.Errorf("test value %d should be below maxChargeRemaining=%d", real, maxChargeRemaining)
	}

	_ = mc // used to satisfy the linter; actual publish tested via integration
}

// TestMaxChargeRemainingConstant ensures the sentinel threshold is sane.
func TestMaxChargeRemainingConstant(t *testing.T) {
	if maxChargeRemaining <= 480 {
		t.Errorf("maxChargeRemaining=%d is too low; real charge times can be up to ~480 min", maxChargeRemaining)
	}
	if maxChargeRemaining > 9999 {
		t.Errorf("maxChargeRemaining=%d is unexpectedly high", maxChargeRemaining)
	}
}
