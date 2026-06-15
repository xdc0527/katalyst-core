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

	"github.com/kubewharf/katalyst-core/pkg/agent/sysadvisor/plugin/poweraware/capper"
	"github.com/kubewharf/katalyst-core/pkg/util/general"
)

const (
	// freqRaiseLowerThresholdPercent is the lower boundary (as % of budget) below which
	// frequency raise is triggered. When actualWatt < budget * 80%, we start raising.
	freqRaiseLowerThresholdPercent = 80

	// freqRaiseStopThresholdPercent is the upper boundary (as % of budget) at or above which
	// frequency raise stops. When actualWatt >= budget * 90%, no further raise is needed.
	freqRaiseStopThresholdPercent = 90

	// freqRaiseStepPercent is the incremental raise per cycle, expressed as a percentage of
	// the current actual watt. Each call raises the target by 5% of current consumption.
	freqRaiseStepPercent = 5
)

// shouldRaiseFreq returns true if actual power is more than 20% below the budget,
// meaning the capper has over-throttled and frequency should be gradually restored.
func shouldRaiseFreq(actualWatt, budget int) bool {
	if budget <= 0 {
		return false
	}
	// actualWatt < budget * 80% → over-throttled, need to raise
	return actualWatt*100 < budget*freqRaiseLowerThresholdPercent
}

// shouldStopRaise returns true if actual power is within 10% below the budget,
// meaning we are close enough to the budget and should stop raising frequency.
func shouldStopRaise(actualWatt, budget int) bool {
	if budget <= 0 {
		return true
	}
	// actualWatt >= budget * 90% → close enough, stop raising
	return actualWatt*100 >= budget*freqRaiseStopThresholdPercent
}

// calcRaiseTarget computes the next target watt for a frequency raise step.
// It raises by freqRaiseStepPercent% of current actual consumption, clamped to budget.
func calcRaiseTarget(actualWatt, budget int) int {
	target := actualWatt + actualWatt*freqRaiseStepPercent/100
	if target > budget {
		target = budget
	}
	return target
}

// tryRaiseFreq checks whether a frequency raise should be issued this cycle.
// It returns true if a Raise instruction was sent (caller should skip normal reconcile).
// It returns false if no raise is needed or the gap is already small enough.
func tryRaiseFreq(ctx context.Context, actualWatt, budget int, powerCapper capper.PowerCapper) bool {
	if shouldStopRaise(actualWatt, budget) {
		// Within 10% of budget — no action needed
		general.InfofV(6, "pap: freq raise: actual=%dW budget=%dW within stop threshold, skip raise", actualWatt, budget)
		return false
	}

	if !shouldRaiseFreq(actualWatt, budget) {
		// Between 80%–90% of budget — in the "dead zone", hold current state
		general.InfofV(6, "pap: freq raise: actual=%dW budget=%dW in hold zone [80%%,90%%), no raise", actualWatt, budget)
		return false
	}

	// actualWatt < budget * 80%: issue a raise step
	target := calcRaiseTarget(actualWatt, budget)
	general.Infof("pap: freq raise: actual=%dW < budget*80%%=%dW; raising target to %dW", actualWatt, budget*freqRaiseLowerThresholdPercent/100, target)
	powerCapper.Cap(ctx, target, actualWatt)
	return true
}
