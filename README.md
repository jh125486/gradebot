# Gradebot

[![Go Reference](https://pkg.go.dev/badge/github.com/jh125486/gradebot)](https://pkg.go.dev/github.com/jh125486/gradebot)
[![Go Report](https://goreportcard.com/badge/github.com/jh125486/gradebot)](https://goreportcard.com/report/github.com/jh125486/gradebot)
[![Tests](https://github.com/jh125486/gradebot/actions/workflows/test.yaml/badge.svg)](https://github.com/jh125486/gradebot/actions/workflows/test.yaml)
[![CodeQL](https://github.com/jh125486/gradebot/actions/workflows/codeql-analysis.yml/badge.svg)](https://github.com/jh125486/gradebot/actions/workflows/codeql-analysis.yml)
[![Codecov](https://codecov.io/gh/jh125486/gradebot/branch/main/graph/badge.svg)](https://codecov.io/gh/jh125486/gradebot)
[![Quality Gate Status](https://sonarcloud.io/api/project_badges/measure?project=jh125486_gradebot&metric=alert_status)](https://sonarcloud.io/summary/new_code?id=jh125486_gradebot)
[![Sonar Coverage](https://sonarcloud.io/api/project_badges/measure?project=jh125486_gradebot&metric=coverage)](https://sonarcloud.io/summary/overall?id=jh125486_gradebot)

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
    "context"
    "github.com/go-git/go-billy/v5/osfs"
    "github.com/jh125486/gradebot/pkg/client"
    "github.com/jh125486/gradebot/pkg/rubrics"
)

// Use the generic execution framework
cfg := &client.Config{
    WorkDir: client.WorkDir("/path/to/student/code"),
    RunCmd:  "go run .",
    // ProgramBuilder is optional - defaults to creating a Program with ExecCommandBuilder
    // For testing, provide a custom builder:
    // ProgramBuilder: func(workDir, runCmd string) (rubrics.ProgramRunner, error) {
    //     return myTestProgram, nil
    // },
}

// Execute with custom evaluators
ctx := context.Background()
err := client.ExecuteProject(ctx, cfg, "Assignment1", "Assignment instructions", nil,
    rubrics.EvaluateGit(osfs.New(cfg.WorkDir.String())),
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
// Note: cli.CommonArgs can be embedded in commands to get standard project grading flags
ctx := kong.Parse(&CLI,
    kong.Bind(svc),
)

// 3. Define commands that accept the service as an argument
type MyCommand struct {
    cli.CommonArgs `embed:""`
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

## Examples

- A short example for `cli.NewService` is available in the package docs (it will appear as `ExampleNewService` on pkg.go.dev).
- For runnable examples of grading workflows, see the `client` and `rubrics` packages and the unit tests for usage patterns.

## Creating a downstream grader (quickstart)

This project is intended to be embedded in course-specific grader CLIs. Below is a minimal step-by-step guide and a runnable example in `examples/grader`.

1. Define a Kong command type that embeds `cli.CommonArgs`:

```go
// In your grader package
type gradeCmd struct {
    cli.CommonArgs `embed:""`
}
```

1. Implement `Run(svc *cli.Service) error` on the command. Inside `Run`:
   - Build a `*client.Config` using the parsed `Args`.
   - Optionally set `ProgramBuilder` for testability.
   - Call `client.ExecuteProject` with built-in evaluators like `rubrics.EvaluateGit` and your custom `rubrics.Evaluator` implementations.

2. Wire up Kong and bind `*cli.Service` with `cli.NewKongContext` in `main`:

```go
var CLI struct {
    Grade gradeCmd `cmd:"" help:"Grade a project"`
}

kctx := cli.NewKongContext(context.Background(), "gradebot", "build-id", &CLI, os.Args[1:])
if err := kctx.Run(); err != nil {
    // handle error
}
```

1. For a downstream grader example, see the CSCE3550 repository: [https://github.com/jh125486/CSCE3550](https://github.com/jh125486/CSCE3550), which includes a grader that grades three distinct projects.

Tips & guidance:

- Use `rubrics.EvaluateGit` to run repository checks; add `rubrics.Evaluator` for assignment-specific tests.
- For AI quality scoring, configure `client.Config.QualityClient` and include `rubrics.EvaluateQuality`.
- Provide a test-friendly `ProgramBuilder` in `client.Config` when writing unit tests.

## Development

Use the Makefile to initialize the development environment and run checks.

```bash
# Initialize development environment (installs protoc, git hooks, etc.)
make init

# Run formatting, vet, linting and tests
make check
```

## License

See [LICENSE](LICENSE) file for details.
