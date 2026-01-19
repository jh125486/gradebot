package rubrics_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res := &r.Result{Rubric: tt.items}
			if got := res.Awarded(); got != tt.wantTotal {
				t.Fatalf("Awarded() = %f, want %f", got, tt.wantTotal)
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
	}{
		{name: "empty", submissionID: "s1", items: nil},
		{name: "single", submissionID: "s2", items: []r.RubricItem{{Name: "A", Note: "ok", Points: 5}}},
		{name: "multiple", submissionID: "s3", items: []r.RubricItem{{Name: "A", Note: "ok", Points: 3}, {Name: "B", Note: "also", Points: 7}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res := &r.Result{SubmissionID: tt.submissionID, Rubric: tt.items}

			var buf bytes.Buffer
			res.Render(&buf)
			out := buf.String()

			for _, it := range tt.items {
				if !strings.Contains(out, it.Name) {
					t.Fatalf("output missing item name %q: %s", it.Name, out)
				}
			}
			if !strings.Contains(out, tt.submissionID) {
				t.Fatalf("output missing SubmissionID %q: %s", tt.submissionID, out)
			}
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

func TestBagValue(t *testing.T) {
	t.Parallel()

	strVal := "hello"
	intVal := 42

	type args struct {
		bag r.RunBag
		key string
	}
	tests := []struct {
		name string
		args args
		want *string
	}{
		{
			name: "found string",
			args: args{
				bag: r.RunBag{"str": &strVal},
				key: "str",
			},
			want: &strVal,
		},
		{
			name: "missing key",
			args: args{
				bag: r.RunBag{},
				key: "unknown",
			},
			want: nil,
		},
		{
			name: "wrong type in bag",
			args: args{
				bag: r.RunBag{"int": &intVal},
				key: "int",
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := r.BagValue[string](tt.args.bag, tt.args.key)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSetBagValue(t *testing.T) {
	t.Parallel()

	type args struct {
		bag  r.RunBag
		key  string
		pVal *string
	}
	tests := []struct {
		name   string
		args   args
		verify func(*testing.T, r.RunBag)
	}{
		{
			name: "sets new value",
			args: args{
				bag:  make(r.RunBag),
				key:  "new-key",
				pVal: func() *string { s := "val"; return &s }(),
			},
			verify: func(t *testing.T, b r.RunBag) {
				got := r.BagValue[string](b, "new-key")
				require.NotNil(t, got)
				assert.Equal(t, "val", *got)
			},
		},
		{
			name: "overwrites existing",
			args: args{
				bag: func() r.RunBag {
					s := "old"
					return r.RunBag{"key": &s}
				}(),
				key:  "key",
				pVal: func() *string { s := "new"; return &s }(),
			},
			verify: func(t *testing.T, b r.RunBag) {
				got := r.BagValue[string](b, "key")
				require.NotNil(t, got)
				assert.Equal(t, "new", *got)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r.SetBagValue(tt.args.bag, tt.args.key, tt.args.pVal)
			if tt.verify != nil {
				tt.verify(t, tt.args.bag)
			}
		})
	}
}
