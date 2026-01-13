# Gradebot

A Go library for automated code grading and rubric-based assessment.

## Overview

Gradebot provides a generic framework for building automated grading systems for programming assignments. It includes:

- **Client Package**: Generic configuration and execution framework
- **Rubrics Package**: Core types for program evaluation (`Program`, `Result`, `Evaluator`)
- **Generic Evaluators**: Git repository validation, code quality assessment
- **Server Package**: HTTP/gRPC server for rubric management
- **Storage Package**: R2 and SQL storage backends
- **Proto Package**: Protocol buffer definitions for communication

## Usage

This library is designed to be imported by course-specific grading implementations.

```go
import (
    "github.com/jh125486/gradebot/pkg/client"
    "github.com/jh125486/gradebot/pkg/rubrics"
)

// Use the generic execution framework
cfg := &client.Config{
    Dir:    client.WorkDir("/path/to/student/code"),
    RunCmd: "go run .",
    // ProgramBuilder is optional - defaults to creating a Program with ExecCommandFactory
    // For testing, provide a custom builder:
    // ProgramBuilder: func(workDir, runCmd string) (rubrics.ProgramRunner, error) {
    //     return myTestProgram, nil
    // },
}

// Execute with custom evaluators
err := client.ExecuteProject(ctx, cfg, "Assignment1",
    rubrics.EvaluateGit(osfs.New(cfg.Dir.String())),
    customEvaluator1,
    customEvaluator2,
)
```

## Course-Specific Implementations

Course-specific implementations should:

1. Import gradebot as a dependency
2. Implement course-specific evaluators
3. Provide ExecuteProjectX functions that call `client.ExecuteProject` with appropriate evaluators

Example: Course-specific implementations should import this library

## Development

```bash
# Install dependencies
go mod download

# Run tests
go test ./...

# Run tests with coverage
go test -race -coverprofile=coverage.txt -covermode=atomic ./...

# Lint
golangci-lint run
```

## License

See [LICENSE](LICENSE) file for details.
