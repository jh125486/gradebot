package rubrics_test

import (
	"context"
	"fmt"
	"io/fs"
	"testing"
	"testing/fstest"

	"connectrpc.com/connect"
	pb "github.com/jh125486/gradebot/pkg/proto"
	"github.com/jh125486/gradebot/pkg/rubrics"
	"github.com/stretchr/testify/require"
)

// errorFS is a filesystem that can be configured to return errors
type errorFS struct {
	fstest.MapFS
	readFileError     bool
	readFileErrorPath string
}

func (e *errorFS) Open(name string) (fs.File, error) {
	if e.readFileError && name == e.readFileErrorPath {
		return nil, fmt.Errorf("simulated read error for %s", name)
	}
	return e.MapFS.Open(name)
}

func (e *errorFS) ReadFile(name string) ([]byte, error) {
	if e.readFileError && name == e.readFileErrorPath {
		return nil, fmt.Errorf("simulated read error for %s", name)
	}
	return fs.ReadFile(e.MapFS, name)
}

// walkDirErrorFS simulates filesystem walk errors
type walkDirErrorFS struct {
	fstest.MapFS
}

func (w *walkDirErrorFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if name == "baddir" {
		return nil, fmt.Errorf("simulated walk error for directory: %s", name)
	}
	return w.MapFS.ReadDir(name)
}

// configErrorFS simulates config file errors - but since the config is embedded,
// we need to test paths that can actually fail in loadFiles
type configErrorFS struct {
	fstest.MapFS
	walkError bool
}

func (c *configErrorFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if c.walkError && name == "." {
		return nil, fmt.Errorf("simulated root directory read error")
	}
	return c.MapFS.ReadDir(name)
}

// specificErrorFS creates a filesystem that triggers specific walk errors
type specificErrorFS struct {
	fstest.MapFS
	triggerWalkErr bool
}

func (s *specificErrorFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if s.triggerWalkErr && name == "." {
		// Return a directory entry that will cause walkErr != nil in walkFn
		return []fs.DirEntry{
			&fakeDirEntry{name: "error-file.go", isDir: false},
		}, nil
	}
	return s.MapFS.ReadDir(name)
}

func (s *specificErrorFS) Stat(name string) (fs.FileInfo, error) {
	if s.triggerWalkErr && name == "error-file.go" {
		return nil, fmt.Errorf("simulated stat error for error-file.go")
	}
	return s.MapFS.Stat(name)
}

func (s *specificErrorFS) Open(name string) (fs.File, error) {
	if s.triggerWalkErr && name == "error-file.go" {
		return nil, fmt.Errorf("simulated open error for error-file.go")
	}
	return s.MapFS.Open(name)
}

// realWalkErrorFS creates an actual walk error by implementing a broken ReadDir
type realWalkErrorFS struct {
	fstest.MapFS
	causeBrokenWalk bool
}

func (r *realWalkErrorFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if r.causeBrokenWalk && name == "." {
		// Return an error that will propagate as walkErr to the walkFn
		return nil, fmt.Errorf("simulated directory read error")
	}
	return r.MapFS.ReadDir(name)
}

// fakeDirEntry implements fs.DirEntry
type fakeDirEntry struct {
	name  string
	isDir bool
}

func (f *fakeDirEntry) Name() string { return f.name }
func (f *fakeDirEntry) IsDir() bool  { return f.isDir }
func (f *fakeDirEntry) Type() fs.FileMode {
	if f.isDir {
		return fs.ModeDir
	}
	return 0
}
func (f *fakeDirEntry) Info() (fs.FileInfo, error) {
	return nil, fmt.Errorf("simulated info error")
}

// MockQualityServiceClient is a mock for testing quality evaluation
type MockQualityServiceClient struct {
	EvaluateResponse *pb.EvaluateCodeQualityResponse
	EvaluateError    error
}

func (m *MockQualityServiceClient) EvaluateCodeQuality(ctx context.Context, req *connect.Request[pb.EvaluateCodeQualityRequest]) (*connect.Response[pb.EvaluateCodeQualityResponse], error) {
	if m.EvaluateError != nil {
		return nil, m.EvaluateError
	}
	return connect.NewResponse(m.EvaluateResponse), nil
}

// mockProgramRunner is a mock for testing
type mockProgramRunner struct{}

func (m *mockProgramRunner) Path() string                         { return "" }
func (m *mockProgramRunner) Run(context.Context, ...string) error { return nil }
func (m *mockProgramRunner) Do(stdin string) (stdout, stderr []string, err error) {
	return nil, nil, nil
}
func (m *mockProgramRunner) Kill() error                       { return nil }
func (m *mockProgramRunner) Cleanup(ctx context.Context) error { return nil }

func TestEvaluateQuality(t *testing.T) {
	type args struct {
		instructions string
		mockClient   *MockQualityServiceClient
		sourceFS     fs.FS
	}
	type want struct {
		name        string
		noteContain string
		points      float64
		errContain  string // non-empty means we expect an error message in the note
	}

	tests := []struct {
		name   string
		args   args
		want   want
		setup  func(t *testing.T)
		verify func(t *testing.T, got rubrics.RubricItem)
	}{
		{
			name: "Success",
			args: args{
				instructions: "Review this Go code",
				mockClient: &MockQualityServiceClient{
					EvaluateResponse: &pb.EvaluateCodeQualityResponse{QualityScore: 85, Feedback: "Good code quality"},
				},
				sourceFS: fstest.MapFS{"main.go": &fstest.MapFile{Data: []byte("package main\n\nfunc main() { println(\"hello\") }")}},
			},
			want: want{name: "Quality", noteContain: "Good code quality", points: 17.0},
		},
		{
			name: "ClientError",
			args: args{
				instructions: "Review this code",
				mockClient:   &MockQualityServiceClient{EvaluateError: fmt.Errorf("API error")},
				sourceFS:     fstest.MapFS{"main.go": &fstest.MapFile{Data: []byte("package main\n\nfunc main() { println(\"hello\") }")}},
			},
			want: want{name: "Quality", errContain: "Connect call failed: API error", points: 0},
		},
		{
			name: "EmptyFilesystem",
			args: args{
				instructions: "Review this code",
				mockClient:   &MockQualityServiceClient{EvaluateResponse: &pb.EvaluateCodeQualityResponse{QualityScore: 90, Feedback: "Great code"}},
				sourceFS:     fstest.MapFS{},
			},
			want: want{name: "Quality", noteContain: "Great code", points: 18.0},
		},
		{
			name: "LoadFilesFileSystemError",
			args: args{
				instructions: "Review this code",
				mockClient:   &MockQualityServiceClient{EvaluateResponse: &pb.EvaluateCodeQualityResponse{QualityScore: 90, Feedback: "Great code"}},
				sourceFS:     &errorFS{MapFS: fstest.MapFS{"main.go": &fstest.MapFile{Data: []byte("package main")}}, readFileError: true, readFileErrorPath: "main.go"},
			},
			want: want{name: "Quality", errContain: "Failed to prepare code for review:", points: 0},
		},
		{
			name: "BinaryFileSkipped",
			args: args{
				instructions: "Review this mixed code",
				mockClient:   &MockQualityServiceClient{EvaluateResponse: &pb.EvaluateCodeQualityResponse{QualityScore: 75, Feedback: "Decent code quality"}},
				sourceFS:     fstest.MapFS{"main.go": &fstest.MapFile{Data: []byte("package main\nfunc main() {}")}, "binary.go": &fstest.MapFile{Data: []byte{0x00, 0x01, 0x02, 0xFF, 0xFE}}},
			},
			want: want{name: "Quality", noteContain: "Decent code quality", points: 15.0},
		},
		{
			name: "ExcludeDirectories",
			args: args{
				instructions: "Review this structured code",
				mockClient:   &MockQualityServiceClient{EvaluateResponse: &pb.EvaluateCodeQualityResponse{QualityScore: 80, Feedback: "Good structure"}},
				sourceFS:     fstest.MapFS{"main.go": &fstest.MapFile{Data: []byte("package main")}, "src/lib.go": &fstest.MapFile{Data: []byte("package lib")}, "target/main.go": &fstest.MapFile{Data: []byte("package target")}, ".git/config": &fstest.MapFile{Data: []byte("git config")}, "node_modules/x.js": &fstest.MapFile{Data: []byte("console.log('test')")}},
			},
			want: want{name: "Quality", noteContain: "Good structure", points: 16.0},
		},
		{
			name: "MixedFileTypes",
			args: args{
				instructions: "Review multiple languages",
				mockClient:   &MockQualityServiceClient{EvaluateResponse: &pb.EvaluateCodeQualityResponse{QualityScore: 85, Feedback: "Multi-language code"}},
				sourceFS:     fstest.MapFS{"main.go": &fstest.MapFile{Data: []byte("package main")}, "app.py": &fstest.MapFile{Data: []byte("print('hello')")}, "lib.rs": &fstest.MapFile{Data: []byte("fn main() {}")}, "script.js": &fstest.MapFile{Data: []byte("console.log('test')")}, "config.yaml": &fstest.MapFile{Data: []byte("key: value")}, "README": &fstest.MapFile{Data: []byte("readme")}},
			},
			want: want{name: "Quality", noteContain: "Multi-language code", points: 17.0},
		},
		{
			name: "WalkDirError",
			args: args{
				instructions: "Review code with walk error",
				mockClient:   &MockQualityServiceClient{EvaluateResponse: &pb.EvaluateCodeQualityResponse{QualityScore: 90, Feedback: "Good code"}},
				sourceFS:     &walkDirErrorFS{MapFS: fstest.MapFS{"main.go": &fstest.MapFile{Data: []byte("package main")}, "baddir/file.go": &fstest.MapFile{Data: []byte("package baddir")}}},
			},
			want: want{name: "Quality", errContain: "Failed to prepare code for review:", points: 0},
		},
		{
			name: "ConfigLoadError",
			args: args{
				instructions: "Review code with config error",
				mockClient:   &MockQualityServiceClient{EvaluateResponse: &pb.EvaluateCodeQualityResponse{QualityScore: 90, Feedback: "Good code"}},
				sourceFS:     &configErrorFS{MapFS: fstest.MapFS{"main.go": &fstest.MapFile{Data: []byte("package main")}}, walkError: true},
			},
			want: want{name: "Quality", errContain: "Failed to prepare code for review:", points: 0},
		},
		{
			name: "FileWalkError",
			args: args{
				instructions: "Review code with specific walk error",
				mockClient:   &MockQualityServiceClient{EvaluateResponse: &pb.EvaluateCodeQualityResponse{QualityScore: 90, Feedback: "Good code"}},
				sourceFS:     &specificErrorFS{MapFS: fstest.MapFS{"main.go": &fstest.MapFile{Data: []byte("package main")}}, triggerWalkErr: true},
			},
			want: want{name: "Quality", errContain: "Failed to prepare code for review:", points: 0},
		},
		{
			name: "MultipleFileTypesWithErrors",
			args: args{
				instructions: "Comprehensive file processing test",
				mockClient:   &MockQualityServiceClient{EvaluateResponse: &pb.EvaluateCodeQualityResponse{QualityScore: 85, Feedback: "Mixed code quality"}},
				sourceFS: fstest.MapFS{
					"main.go":       &fstest.MapFile{Data: []byte("package main\nfunc main() { println(\"hello\") }")},
					"lib.py":        &fstest.MapFile{Data: []byte("def hello(): print('world')")},
					"script.js":     &fstest.MapFile{Data: []byte("console.log('test');")},
					"app.rs":        &fstest.MapFile{Data: []byte("fn main() { println!(\"rust\"); }")},
					"config.toml":   &fstest.MapFile{Data: []byte("key = 'value'")},
					"binary.go":     &fstest.MapFile{Data: []byte{0xFF, 0xFE, 0x00}},
					"empty.go":      &fstest.MapFile{Data: []byte("")},
					"target/out.go": &fstest.MapFile{Data: []byte("package target")},
					"src/nested.go": &fstest.MapFile{Data: []byte("package nested")},
				},
			},
			want: want{name: "Quality", noteContain: "Mixed code quality", points: 17.0},
		},

		{
			name: "UTF8ValidationTest",
			args: args{
				instructions: "Test UTF-8 validation path",
				mockClient:   &MockQualityServiceClient{EvaluateResponse: &pb.EvaluateCodeQualityResponse{QualityScore: 100, Feedback: "Only valid UTF-8 files processed"}},
				sourceFS:     fstest.MapFS{"main.go": &fstest.MapFile{Data: []byte("package main\nfunc main() { fmt.Println(\"Hello\") }")}, "invalid.go": &fstest.MapFile{Data: []byte{0xFF, 0xFE, 0xFD}}},
			},
			want: want{name: "Quality", noteContain: "Only valid UTF-8 files processed", points: 20.0},
		},
		{
			name: "WalkDirErrorTest",
			args: args{
				instructions: "Test WalkDir error handling",
				mockClient:   &MockQualityServiceClient{EvaluateResponse: &pb.EvaluateCodeQualityResponse{QualityScore: 80, Feedback: "Files processed"}},
				sourceFS:     &walkDirErrorFS{MapFS: fstest.MapFS{"baddir/test.go": &fstest.MapFile{Data: []byte("package main")}}},
			},
			want: want{name: "Quality", errContain: "simulated walk error"},
		},
		{
			name: "SingleFileSuccess",
			args: args{
				instructions: "Simple single file test",
				mockClient:   &MockQualityServiceClient{EvaluateResponse: &pb.EvaluateCodeQualityResponse{QualityScore: 100, Feedback: "Perfect code"}},
				sourceFS:     fstest.MapFS{"perfect.go": &fstest.MapFile{Data: []byte("package main\n\n// Perfect Go code\nfunc main() {\n\tfmt.Println(\"Hello, World!\")\n}")}},
			},
			want: want{name: "Quality", noteContain: "Perfect code", points: 20.0},
		},
		{
			name: "RealWalkError",
			args: args{
				instructions: "Test with actual walk directory error",
				mockClient:   &MockQualityServiceClient{EvaluateResponse: &pb.EvaluateCodeQualityResponse{QualityScore: 90, Feedback: "Good code"}},
				sourceFS:     &realWalkErrorFS{MapFS: fstest.MapFS{"main.go": &fstest.MapFile{Data: []byte("package main")}}, causeBrokenWalk: true},
			},
			want: want{name: "Quality", errContain: "Failed to prepare code for review:", points: 0},
		},
	}

	for _, tc := range tests {
		// capture
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.setup != nil {
				tc.setup(t)
			}

			evaluator := rubrics.EvaluateQuality(tc.args.mockClient, tc.args.sourceFS, tc.args.instructions)
			got := evaluator(t.Context(), &mockProgramRunner{}, rubrics.RunBag{})

			if tc.want.errContain != "" {
				require.Contains(t, got.Note, tc.want.errContain)
				require.Equal(t, 0.0, got.Awarded)
			} else {
				require.InDelta(t, tc.want.points, got.Awarded, 0.0001)
				require.Contains(t, got.Note, tc.want.noteContain)
			}

			if tc.verify != nil {
				tc.verify(t, got)
			}
		})
	}
}
