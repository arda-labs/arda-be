package decision

import (
	"encoding/json"
	"errors"
	"math"
	"strconv"
)

// Score is a rubric answer: a position on ordered levels with a probability
// per level. Level 0 is the first criterion in the question.
type Score struct {
	Value         float64            `json:"score"`
	Confidence    float64            `json:"confidence"`
	Probabilities map[string]float64 `json:"probabilities"`
}

// ScoreQuestion asks the provider to rate the state against ordered levels,
// lowest first. At least two levels are required.
func ScoreQuestion(instructions string, levels []string) Question {
	encoded, _ := json.Marshal(levels)
	return Question{Type: "score", Instructions: instructions, Criteria: encoded}
}

func decodeScore(question Question, raw wireAnswer) (Answer, error) {
	var levels []string
	if err := json.Unmarshal(question.Criteria, &levels); err != nil || len(levels) < 2 {
		return Answer{}, errors.New("score question has invalid criteria")
	}
	if raw.Score == nil || raw.Confidence == nil || math.IsNaN(*raw.Score) || math.IsNaN(*raw.Confidence) {
		return Answer{}, ErrInvalidResponse
	}
	if *raw.Score < 0 || *raw.Score > float64(len(levels)-1) || *raw.Confidence < 0 || *raw.Confidence > 1 {
		return Answer{}, ErrInvalidResponse
	}
	if len(raw.Probabilities) != len(levels) {
		return Answer{}, ErrInvalidResponse
	}
	total := 0.0
	for index := range levels {
		p, exists := raw.Probabilities[strconv.Itoa(index)]
		if !exists || math.IsNaN(p) || p < 0 || p > 1 {
			return Answer{}, ErrInvalidResponse
		}
		total += p
	}
	if math.Abs(total-1) >= 0.01 {
		return Answer{}, ErrInvalidResponse
	}
	return Answer{Type: "score", Score: Score{Value: *raw.Score, Confidence: *raw.Confidence, Probabilities: raw.Probabilities}}, nil
}

// ScoreOf returns a score answer, or false when it is missing or of another
// type.
func (r *Result) ScoreOf(id string) (Score, bool) {
	if r == nil {
		return Score{}, false
	}
	answer, ok := r.Answers[id]
	if !ok || answer.Type != "score" {
		return Score{}, false
	}
	return answer.Score, true
}
