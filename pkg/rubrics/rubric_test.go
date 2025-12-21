package rubrics_test

import (
	"bytes"
	"strings"
	"testing"

	r "github.com/jh125486/gradebot/pkg/rubrics"
)

func TestResult_Awarded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		items     []r.RubricItem
		wantTotal float64
	}{
		{name: "empty", items: nil, wantTotal: 0},
		{name: "single", items: []r.RubricItem{{Name: "A", Note: "ok", Awarded: 5}}, wantTotal: 5},
		{name: "multiple", items: []r.RubricItem{{Name: "A", Note: "ok", Awarded: 3}, {Name: "B", Note: "also", Awarded: 7}}, wantTotal: 10},
	}

	for i := range tests {
		tc := tests[i]
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res := &r.Result{Rubric: tc.items}
			if got := res.Awarded(); got != tc.wantTotal {
				t.Fatalf("Awarded() = %f, want %f", got, tc.wantTotal)
			}
		})
	}
}

func TestResult_ToTable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		submissionID string
		items        []r.RubricItem
		useNilWriter bool
	}{
		{name: "empty", submissionID: "s1", items: nil, useNilWriter: false},
		{name: "single", submissionID: "s2", items: []r.RubricItem{{Name: "A", Note: "ok", Points: 5}}, useNilWriter: false},
		{name: "multiple", submissionID: "s3", items: []r.RubricItem{{Name: "A", Note: "ok", Points: 3}, {Name: "B", Note: "also", Points: 7}}, useNilWriter: false},
		{name: "nil_writer_uses_stdout", submissionID: "s4", items: []r.RubricItem{{Name: "C", Note: "test", Points: 10}}, useNilWriter: true},
	}

	for i := range tests {
		tc := tests[i]
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res := &r.Result{SubmissionID: tc.submissionID, Rubric: tc.items}

			var buf bytes.Buffer
			var out string
			if tc.useNilWriter {
				res.Render(nil) // Should use os.Stdout
			} else {
				res.Render(&buf)
				out = buf.String()
			}

			if !tc.useNilWriter {
				for _, it := range tc.items {
					if !strings.Contains(out, it.Name) {
						t.Fatalf("output missing item name %q: %s", it.Name, out)
					}
				}
				if !strings.Contains(out, tc.submissionID) {
					t.Fatalf("output missing SubmissionID %q: %s", tc.submissionID, out)
				}
			}
			// nil writer case - just verify it doesn't panic (output goes to stdout)
		})
	}
}

func TestNewResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
	}{
		{
			name: "default",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res := r.NewResult(tt.name)
			if res == nil {
				t.Fatalf("empty Result")
			}
		})
	}
}
