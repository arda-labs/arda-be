package main

import (
	"reflect"
	"testing"
)

func TestParseSLAMilestones(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []int
		wantErr bool
	}{
		{name: "defaults", want: []int{50, 90, 100}},
		{name: "sort and dedupe", input: "90,50,100,50", want: []int{50, 90, 100}},
		{name: "requires overdue", input: "50,90", wantErr: true},
		{name: "range", input: "0,100", wantErr: true},
		{name: "integer", input: "ninety,100", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSLAMilestones(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("milestones = %v, want %v", got, tt.want)
			}
		})
	}
}
