package evaluation

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Calibration compares the Jev judge against human labels before any strict
// gate is considered. Labels are booleans per dimension; nil means the
// dimension does not apply to that case (for example groundedness for an
// out-of-corpus question).
type CalibrationSet struct {
	Version     int               `yaml:"version"`
	Description string            `yaml:"description"`
	Cases       []CalibrationCase `yaml:"cases"`
}

type CalibrationCase struct {
	ID                string `yaml:"id"`
	Grounded          *bool  `yaml:"grounded"`
	AnswersQuestion   *bool  `yaml:"answers_question"`
	CorrectAbstention *bool  `yaml:"correct_abstention"`
}

func ParseCalibrationSet(data []byte) (CalibrationSet, error) {
	var set CalibrationSet
	if err := yaml.Unmarshal(data, &set); err != nil {
		return CalibrationSet{}, fmt.Errorf("decode calibration set: %w", err)
	}
	if len(set.Cases) == 0 {
		return CalibrationSet{}, fmt.Errorf("calibration set has no cases")
	}
	seen := make(map[string]struct{}, len(set.Cases))
	for _, c := range set.Cases {
		if strings.TrimSpace(c.ID) == "" {
			return CalibrationSet{}, fmt.Errorf("calibration case needs an id")
		}
		if _, ok := seen[c.ID]; ok {
			return CalibrationSet{}, fmt.Errorf("duplicate calibration case id %q", c.ID)
		}
		seen[c.ID] = struct{}{}
	}
	return set, nil
}

// AnswerArtifact is the JSON written by cmd/ai-eval -mode=answer. The
// calibration runner consumes it so labels never require re-running the agent.
type AnswerArtifact struct {
	Judge     string       `json:"judge"`
	Model     string       `json:"model,omitempty"`
	Timestamp string       `json:"timestamp"`
	Report    AnswerReport `json:"report"`
}

// CalibrationDimension is the confusion matrix of one judge dimension against
// human labels at the 0.5 probability threshold.
type CalibrationDimension struct {
	Dimension string  `json:"dimension"`
	Cases     int     `json:"cases"`
	TP        int     `json:"tp"`
	FP        int     `json:"fp"`
	FN        int     `json:"fn"`
	TN        int     `json:"tn"`
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
	F1        float64 `json:"f1"`
	Agreement float64 `json:"agreement"`
}

type CalibrationReport struct {
	Version        int                    `json:"version"`
	Threshold      float64                `json:"threshold"`
	Dimensions     []CalibrationDimension `json:"dimensions"`
	MissingValues  int                    `json:"missing_values"`
	UntrackedCases int                    `json:"untracked_cases"`
	JudgeErrors    int                    `json:"judge_errors"`
}

const calibrationThreshold = 0.5

var calibrationDimensions = []string{"grounded", "answers_question", "correct_abstention"}

func calibrationValue(result AnswerCaseResult, dimension string) *float64 {
	switch dimension {
	case "grounded":
		return result.Grounded
	case "answers_question":
		return result.AnswersQuestion
	case "correct_abstention":
		return result.CorrectAbstention
	}
	return nil
}

func calibrationLabel(c CalibrationCase, dimension string) *bool {
	switch dimension {
	case "grounded":
		return c.Grounded
	case "answers_question":
		return c.AnswersQuestion
	case "correct_abstention":
		return c.CorrectAbstention
	}
	return nil
}

func RunCalibration(set CalibrationSet, artifact AnswerArtifact) CalibrationReport {
	report := CalibrationReport{Version: set.Version, Threshold: calibrationThreshold}
	results := make(map[string]AnswerCaseResult, len(artifact.Report.Cases))
	for _, c := range artifact.Report.Cases {
		results[c.ID] = c
	}
	counters := map[string]*CalibrationDimension{}
	for _, dimension := range calibrationDimensions {
		counters[dimension] = &CalibrationDimension{Dimension: dimension}
	}
	for _, c := range set.Cases {
		result, ok := results[c.ID]
		if !ok {
			report.UntrackedCases++
			continue
		}
		if result.JudgeError != "" {
			report.JudgeErrors++
		}
		for _, dimension := range calibrationDimensions {
			label := calibrationLabel(c, dimension)
			if label == nil {
				continue
			}
			value := calibrationValue(result, dimension)
			if value == nil {
				report.MissingValues++
				continue
			}
			row := counters[dimension]
			predicted := *value >= calibrationThreshold
			switch {
			case *label && predicted:
				row.TP++
			case !*label && predicted:
				row.FP++
			case *label && !predicted:
				row.FN++
			default:
				row.TN++
			}
		}
	}
	for _, dimension := range calibrationDimensions {
		row := counters[dimension]
		row.Cases = row.TP + row.FP + row.FN + row.TN
		if row.Cases == 0 {
			continue
		}
		if row.TP+row.FP > 0 {
			row.Precision = float64(row.TP) / float64(row.TP+row.FP)
		}
		if row.TP+row.FN > 0 {
			row.Recall = float64(row.TP) / float64(row.TP+row.FN)
		}
		if row.Precision+row.Recall > 0 {
			row.F1 = 2 * row.Precision * row.Recall / (row.Precision + row.Recall)
		}
		row.Agreement = float64(row.TP+row.TN) / float64(row.Cases)
		report.Dimensions = append(report.Dimensions, *row)
	}
	return report
}
