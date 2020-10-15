// Copyright The OpenTelemetry Authors
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

package sampling

import (
	"errors"
	"regexp"

	"go.opentelemetry.io/collector/consumer/pdata"

	tracepb "github.com/census-instrumentation/opencensus-proto/gen-go/trace/v1"
	"github.com/golang/protobuf/ptypes/timestamp"
	"go.uber.org/zap"
)

type spanPropertiesFilter struct {
	operationRe       *regexp.Regexp
	minDurationMicros *int64
	minNumberOfSpans  *int
	logger            *zap.Logger
}

var _ PolicyEvaluator = (*spanPropertiesFilter)(nil)

// NewSpanPropertiesFilter creates a policy evaluator that samples all traces with
// the specified criteria
func NewSpanPropertiesFilter(logger *zap.Logger, operationNamePattern *string, minDurationMicros *int64, minNumberOfSpans *int) (PolicyEvaluator, error) {
	var operationRe *regexp.Regexp
	if operationNamePattern != nil {
		var err error
		operationRe, err = regexp.Compile(*operationNamePattern)
		if err != nil {
			return nil, err
		}
	}

	if operationNamePattern == nil && minDurationMicros == nil && minNumberOfSpans == nil {
		return nil, errors.New("at least one property must be defined")
	}

	return &spanPropertiesFilter{
		operationRe:       operationRe,
		minDurationMicros: minDurationMicros,
		minNumberOfSpans:  minNumberOfSpans,
		logger:            logger,
	}, nil
}

// OnLateArrivingSpans notifies the evaluator that the given list of spans arrived
// after the sampling decision was already taken for the trace.
// This gives the evaluator a chance to log any message/metrics and/or update any
// related internal state.
func (df *spanPropertiesFilter) OnLateArrivingSpans(earlyDecision Decision, spans []*tracepb.Span) error {
	return nil
}

// EvaluateSecondChance looks at the trace again and if it can/cannot be fit, returns a SamplingDecision
func (df *spanPropertiesFilter) EvaluateSecondChance(_ pdata.TraceID, trace *TraceData) (Decision, error) {
	return NotSampled, nil
}

func tsToMicros(ts *timestamp.Timestamp) int64 {
	return ts.Seconds*1000000 + int64(ts.Nanos/1000)
}

// Evaluate looks at the trace data and returns a corresponding SamplingDecision.
func (df *spanPropertiesFilter) Evaluate(_ pdata.TraceID, trace *TraceData) (Decision, error) {
	trace.Lock()
	batches := trace.ReceivedBatches
	trace.Unlock()

	matchingOperationFound := false
	spanCount := 0
	minStartTime := int64(0)
	maxEndTime := int64(0)

	for _, batch := range batches {
		spanCount += len(batch.Spans)

		for _, span := range batch.Spans {
			if span == nil {
				continue
			}

			if df.operationRe != nil && !matchingOperationFound {
				if df.operationRe.MatchString(span.Name.Value) {
					matchingOperationFound = true
				}
			}

			if df.minDurationMicros != nil {
				startTs := tsToMicros(span.StartTime)
				endTs := tsToMicros(span.EndTime)

				if minStartTime == 0 {
					minStartTime = startTs
					maxEndTime = endTs
				} else {
					if startTs < minStartTime {
						minStartTime = startTs
					}
					if endTs > maxEndTime {
						maxEndTime = endTs
					}
				}
			}
		}
	}

	operationNameConditionMet := true
	minDurationConditionMet := true
	minSpanCountConditionMet := true


	if df.operationRe != nil {
		operationNameConditionMet = matchingOperationFound
	}

	if df.minDurationMicros != nil {
		// Sanity check first
		minDurationConditionMet = maxEndTime > minStartTime && maxEndTime-minStartTime >= *df.minDurationMicros
	}

	if df.minNumberOfSpans != nil {
		minSpanCountConditionMet = spanCount >= *df.minNumberOfSpans
	}

	if minDurationConditionMet && operationNameConditionMet && minSpanCountConditionMet {
		return Sampled, nil
	}

	return NotSampled, nil
}

// OnDroppedSpans is called when the trace needs to be dropped, due to memory
// pressure, before the decision_wait time has been reached.
func (df *spanPropertiesFilter) OnDroppedSpans(_ pdata.TraceID, _ *TraceData) (Decision, error) {
	return NotSampled, nil
}
