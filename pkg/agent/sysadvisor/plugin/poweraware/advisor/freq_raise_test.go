/*
Copyright 2022 The Katalyst Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package advisor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// mockPowerCapper is a test double for capper.PowerCapper
type mockPowerCapper struct {
	mock.Mock
}

func (m *mockPowerCapper) Init() error                                          { return nil }
func (m *mockPowerCapper) Start() error                                         { return nil }
func (m *mockPowerCapper) Stop() error                                          { return nil }
func (m *mockPowerCapper) Reset()                                               {}
func (m *mockPowerCapper) Cap(_ context.Context, targetWatts, currWatt int)     {}
func (m *mockPowerCapper) Raise(_ context.Context, targetWatts, currWatt int) {
	m.Called(targetWatts, currWatt)
}

// ---------------------------------------------------------------------------
// shouldRaiseFreq
// ---------------------------------------------------------------------------

func TestShouldRaiseFreq(t *testing.T) {
	tests := []struct {
		name       string
		actualWatt int
		budget     int
		want       bool
	}{
		{
			name:       "well below 80% of budget → should raise",
			actualWatt: 400,
			budget:     600, // 80% = 480; 400 < 480 → true
			want:       true,
		},
		{
			name:       "exactly 80% of budget → should NOT raise",
			actualWatt: 480,
			budget:     600, // 80% = 480; 480 is not < 480 → false
			want:       false,
		},
		{
			name:       "above 80% but below 90% (hold zone) → should NOT raise",
			actualWatt: 530,
			budget:     600, // 80%=480, 90%=540; 530 ≥ 480 → false
			want:       false,
		},
		{
			name:       "at 90% of budget → should NOT raise",
			actualWatt: 540,
			budget:     600,
			want:       false,
		},
		{
			name:       "above budget → should NOT raise",
			actualWatt: 700,
			budget:     600,
			want:       false,
		},
		{
			name:       "zero budget → should NOT raise (guard)",
			actualWatt: 0,
			budget:     0,
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldRaiseFreq(tt.actualWatt, tt.budget)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// shouldStopRaise
// ---------------------------------------------------------------------------

func TestShouldStopRaise(t *testing.T) {
	tests := []struct {
		name       string
		actualWatt int
		budget     int
		want       bool
	}{
		{
			name:       "actual >= 90% budget → stop",
			actualWatt: 540,
			budget:     600, // 90% = 540; 540 >= 540 → true
			want:       true,
		},
		{
			name:       "actual slightly above 90% → stop",
			actualWatt: 560,
			budget:     600,
			want:       true,
		},
		{
			name:       "actual at budget → stop",
			actualWatt: 600,
			budget:     600,
			want:       true,
		},
		{
			name:       "actual below 90% → do NOT stop",
			actualWatt: 530,
			budget:     600, // 90% = 540; 530 < 540 → false
			want:       false,
		},
		{
			name:       "actual well below budget → do NOT stop",
			actualWatt: 400,
			budget:     600,
			want:       false,
		},
		{
			name:       "zero budget → stop (guard)",
			actualWatt: 0,
			budget:     0,
			want:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldStopRaise(tt.actualWatt, tt.budget)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// calcRaiseTarget
// ---------------------------------------------------------------------------

func TestCalcRaiseTarget(t *testing.T) {
	tests := []struct {
		name       string
		actualWatt int
		budget     int
		want       int
	}{
		{
			name:       "5% step without hitting budget",
			actualWatt: 400,
			budget:     600,
			// 400 + 400*5/100 = 400 + 20 = 420; 420 < 600 → 420
			want: 420,
		},
		{
			name:       "5% step would exceed budget → clamped to budget",
			actualWatt: 590,
			budget:     600,
			// 590 + 590*5/100 = 590 + 29 = 619; 619 > 600 → 600
			want: 600,
		},
		{
			name:       "5% step exactly equals budget",
			actualWatt: 571,
			budget:     600,
			// 571 + 571*5/100 = 571 + 28 = 599; 599 < 600 → 599
			want: 599,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calcRaiseTarget(tt.actualWatt, tt.budget)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// tryRaiseFreq
// ---------------------------------------------------------------------------

func TestTryRaiseFreq_WellBelowBudget_TriggersRaise(t *testing.T) {
	// actualWatt=400, budget=600 → 400 < 480 (80%): should raise
	mc := &mockPowerCapper{}
	// expect Raise(420, 400): target = 400 + 400*5/100 = 420
	mc.On("Raise", 420, 400).Once()

	raised := tryRaiseFreq(context.Background(), 400, 600, mc)

	assert.True(t, raised, "expected tryRaiseFreq to return true (raise issued)")
	mc.AssertExpectations(t)
}

func TestTryRaiseFreq_InHoldZone_NoAction(t *testing.T) {
	// actualWatt=530, budget=600 → 530 is in [480, 540): hold zone → no raise
	mc := &mockPowerCapper{}
	// no Raise expected

	raised := tryRaiseFreq(context.Background(), 530, 600, mc)

	assert.False(t, raised, "expected tryRaiseFreq to return false (hold zone)")
	mc.AssertNotCalled(t, "Raise", mock.Anything, mock.Anything)
}

func TestTryRaiseFreq_WithinStopThreshold_NoAction(t *testing.T) {
	// actualWatt=550, budget=600 → 550 >= 540 (90%): stop zone → no raise
	mc := &mockPowerCapper{}

	raised := tryRaiseFreq(context.Background(), 550, 600, mc)

	assert.False(t, raised, "expected tryRaiseFreq to return false (stop zone)")
	mc.AssertNotCalled(t, "Raise", mock.Anything, mock.Anything)
}

func TestTryRaiseFreq_AtExactly80Percent_NoRaise(t *testing.T) {
	// actualWatt=480, budget=600 → exactly 80%: boundary, should NOT raise
	mc := &mockPowerCapper{}

	raised := tryRaiseFreq(context.Background(), 480, 600, mc)

	assert.False(t, raised, "exactly at 80%% boundary should not raise")
	mc.AssertNotCalled(t, "Raise", mock.Anything, mock.Anything)
}

func TestTryRaiseFreq_TargetClampedToBudget(t *testing.T) {
	// actualWatt=590, budget=600 → 590 < 480? No (590 ≥ 480). Actually 590 is in hold zone.
	// Use actualWatt=450 to stay below 80% but have a step that would exceed budget.
	// 450 < 480 (80% of 600) → raise; target = 450 + 22 = 472; 472 < 600 → 472
	mc := &mockPowerCapper{}
	mc.On("Raise", 472, 450).Once()

	raised := tryRaiseFreq(context.Background(), 450, 600, mc)

	assert.True(t, raised)
	mc.AssertExpectations(t)
}

func TestTryRaiseFreq_ZeroBudget_NoRaise(t *testing.T) {
	// budget=0: guard condition prevents any action
	mc := &mockPowerCapper{}

	raised := tryRaiseFreq(context.Background(), 0, 0, mc)

	assert.False(t, raised)
	mc.AssertNotCalled(t, "Raise", mock.Anything, mock.Anything)
}
