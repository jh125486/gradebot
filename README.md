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
    "github.com/go-git/go-billy/v5/osfs"
    "github.com/jh125486/gradebot/pkg/client"
    "github.com/jh125486/gradebot/pkg/rubrics"
)

// Use the generic execution framework
cfg := &client.Config{
    Dir:    client.WorkDir("/path/to/student/code"),
    RunCmd: "go run .",
    // ProgramBuilder is optional - defaults to creating a Program with ExecCommandBuilder
    // For testing, provide a custom builder:
    // ProgramBuilder: func(workDir, runCmd string) (rubrics.ProgramRunner, error) {
    //     return myTestProgram, nil
    // },
}

// Execute with custom evaluators
err := client.ExecuteProject(ctx, cfg, "Assignment1", "Assignment instructions", nil,
    rubrics.EvaluateGit(osfs.New(cfg.Dir.String())),
    customEvaluator1,
    customEvaluator2,
)
```

## Building a CLI

Gradebot provides a `cli` package to help structure command-line tools using [Kong](https://github.com/alecthomas/kong).

```go
// 1. Create a service container for dependencies
svc := cli.NewService(buildID)

// 2. Bind the service when parsing Kong arguments
// Note: cli.CommonArgs can be embedded in commands to get standard scheduling flags
ctx := kong.Parse(&CLI,
    kong.Bind(svc),
)

// 3. Define commands that accept the service as an argument
type MyCommand struct {
    Args cli.CommonArgs `embed:""`
}

func (c *MyCommand) Run(svc *cli.Service) error {
    // Access configured shared dependencies (Client, Stdin/Stdout, etc.)
    return nil
}
```

## Course-Specific Implementations

Course-specific implementations should:

1. Import gradebot as a dependency
2. Implement course-specific evaluators
3. Provide ExecuteProjectX functions that call `client.ExecuteProject` with appropriate evaluators

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
