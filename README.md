# go-modrank

[![PkgGoDev](https://pkg.go.dev/badge/github.com/goccy/go-modrank)](https://pkg.go.dev/github.com/goccy/go-modrank)
![Go](https://github.com/goccy/go-modrank/workflows/Go/badge.svg)

A tool to calculate the Go modules that are truly important to you.

# Motivation

Most of us rely on open-source software (OSS) for our business. However, the OSS that is crucial to our business is not always properly recognized. For example, the number of GitHub stars is a useful metric for popularity, but it does not necessarily indicate how important a piece of software is to us.

If star counts were to increase according to importance, then libraries that applications and frameworks depend on should have more stars than the applications and frameworks themselves, which are closer to users. Unfortunately, this is not the case today.

To address this issue, I wanted to create a tool that quantitatively evaluates the importance of software, visualizes it, and connects it with other systems.

# Use Case

This tool can score the Go software used by repositories associated with a specific GitHub organization. If you want to understand which Go-based software is critical to your organization, this tool will be useful.

A special feature of this tool is that it retrieves repositories hosting Go modules, allowing you to identify which repositories are essential to your needs.

Since this tool can also be used as a Go library, it can be applied to various other use cases, such as visualizing contributors to the repositories that are important to your organization.

# Scoring Strategy

The most critical aspect of this tool is how it quantitatively evaluates importance. Below is an explanation of the scoring rules.

## Assigning Scores to Repositories Based on Importance (Default: 1)

Organizations often contain repositories used for personal tool development as well as repositories used in production environments. The latter should be considered more important.

If your organization has a way to quantitatively assess repository importance, this tool allows you to reflect that value in its scoring.

## Detecting go.mod and Retrieving Dependency Graphs

The tool clones repositories, navigates to paths containing go.mod, and executes `go mod graph`. It then analyzes the results to construct a dependency graph of Go modules. If multiple go.mod files are found, a graph will be created for each one.

## Scoring Modules Based on the Dependency Graph

Modules that are not depended on by any other modules are called Root Modules. Their score is determined by the score assigned to their respective repositories.

As dependencies go deeper in the hierarchy, their score increases by 1 at each level. In other words, the modules located at the deepest level of the dependency hierarchy will have the highest scores.

Since root modules correspond to paths containing go.mod files, modules used in multiple go.mod files will naturally have higher scores.

If a module is recursively referenced within the dependency graph, its score will not be counted.

# Installation

To use this tool as a standalone application, run the following command:

```console
go install github.com/goccy/go-modrank/cmd/go-modrank@latest
```

For example, you can use the tool by executing the following command:

```console
go-modrank run --repository https://github.com/goccy/go-modrank.git
```

For more details, use the command help:

```console
go-modrank -h
```

```console
Usage:
  main [OPTIONS] <run | update>

Help Options:
  -h, --help  Show this help message

Available commands:
  run     Scan all repositories and output ranking data
  update  Update repository status using the GitHub API to improve performance
```

# Synopsis

To use this tool as a library, you can follow the example below. By default, SQLite is used for data storage, but other databases can also be used. The behavior can be fine-tuned using options. For more details, refer to the API Reference.

```go
package main

import (
	"context"
	"fmt"

	"github.com/goccy/go-modrank"
	"github.com/goccy/go-modrank/repository"
)

func main() {
	if err := run(context.Background()); err != nil {
		panic(err)
	}
}

func run(ctx context.Context) error {
	r, err := modrank.New(ctx)
	if err != nil {
		return err
	}
	repo, err := repository.New("https://github.com/goccy/go-modrank.git")
	if err != nil {
		return err
	}

	mods, err := r.Run(ctx, repo)
	if err != nil {
		return err
	}

	for idx, mod := range mods {
		fmt.Printf("- [%d] %s (%s): %d\n", idx+1, mod.Name, mod.Repository, mod.Score)
	}
	return nil
}
```

# Features

- Concurrent Scanning
  - You can specify workers to enable concurrent processing
- Interruptible
  - By analyzing while saving scan results to the database, already searched items can be skipped even if the process is interrupted
- Efficient API Calls Considering GitHub API Rate Limits
- Automatically detects repositories hosting Go modules and includes them in the results

# License

MIT

