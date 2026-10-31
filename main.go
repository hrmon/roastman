// Command roastman converts Postman Collection v2.1 JSON to Postman's
// git-native local file layout and back.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"roastman/internal/convert"
	"roastman/internal/runner"
)

const version = "0.3.0"

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, "roastman:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage(os.Stderr)
		return errors.New("missing command")
	}
	switch args[0] {
	case "to-files", "split", "export":
		return runToFiles(args[1:])
	case "to-json", "join", "import":
		return runToJSON(args[1:])
	case "run":
		return runCollection(args[1:])
	case "version", "--version", "-v":
		fmt.Printf("roastman %s\n", version)
		return nil
	case "help", "--help", "-h":
		printUsage(os.Stdout)
		return nil
	default:
		printUsage(os.Stderr)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runToFiles(args []string) error {
	fs := flag.NewFlagSet("to-files", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	output := fs.String("o", "", "output collection directory (default: ./<collection name>)")
	fs.StringVar(output, "output", "", "output collection directory (default: ./<collection name>)")
	force := fs.Bool("force", false, "replace a non-empty output directory")
	quiet := fs.Bool("quiet", false, "only print errors")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: roastman to-files <collection.json> [-o DIR] [--force]")
		fmt.Fprintln(os.Stderr, "\nConvert a Postman Collection v2.1 JSON file into a git-native native Git collection directory.")
		fs.PrintDefaults()
	}
	flagArgs, positional := reorderFlags(args, map[string]bool{"-o": true, "--output": true})
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if len(positional) != 1 {
		fs.Usage()
		return errors.New("expected exactly one collection JSON file")
	}
	input := positional[0]
	data, err := os.ReadFile(input)
	if err != nil {
		return err
	}
	res, err := convert.Split(data, convert.SplitOptions{OutputDir: *output, Force: *force})
	if err != nil {
		return err
	}
	if !*quiet {
		fmt.Printf("Converted %q: %d requests, %d folders\n", res.Collection, res.Requests, res.Folders)
		fmt.Printf("Output: %s\n", res.CollectionDir)
	}
	return nil
}

func runToJSON(args []string) error {
	fs := flag.NewFlagSet("to-json", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	output := fs.String("o", "", "output JSON file (default: stdout)")
	fs.StringVar(output, "output", "", "output JSON file (default: stdout)")
	collection := fs.String("collection", "", "collection name when the input contains several collections")
	quiet := fs.Bool("quiet", false, "only print errors (only applies when writing to a file)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: roastman to-json <collection-dir|postman-dir> [-o FILE] [--collection NAME]")
		fmt.Fprintln(os.Stderr, "\nConvert a git-native collection directory back to Postman Collection v2.1 JSON.")
		fs.PrintDefaults()
	}
	flagArgs, positional := reorderFlags(args, map[string]bool{"-o": true, "--output": true, "--collection": true})
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if len(positional) != 1 {
		fs.Usage()
		return errors.New("expected exactly one collection directory")
	}
	input := positional[0]
	if *output != "" && filepath.Dir(*output) != "." {
		if err := os.MkdirAll(filepath.Dir(*output), 0o755); err != nil {
			return err
		}
	}
	res, err := convert.Join(input, convert.JoinOptions{OutputPath: *output, Collection: *collection})
	if err != nil {
		return err
	}
	if *output == "" {
		_, err = os.Stdout.Write(res.JSON)
		return err
	}
	for _, warning := range res.Warnings {
		fmt.Fprintln(os.Stderr, "warning:", warning)
	}
	if !*quiet {
		fmt.Printf("Converted %q to %s\n", res.Collection, *output)
	}
	return nil
}

type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

type envVars map[string]string

func (e envVars) String() string { return "" }
func (e envVars) Set(v string) error {
	key, value, ok := strings.Cut(v, "=")
	if !ok || key == "" {
		return fmt.Errorf("expected KEY=VALUE, got %q", v)
	}
	e[key] = value
	return nil
}

func runCollection(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	collectionName := fs.String("collection", "", "collection name when the input workspace contains several collections")
	environment := fs.String("e", "", "environment name or YAML file")
	fs.StringVar(environment, "environment", "", "environment name or YAML file")
	globals := fs.String("g", "", "globals YAML file")
	fs.StringVar(globals, "globals", "", "globals YAML file")
	data := fs.String("d", "", "iteration data file (.csv or .json)")
	fs.StringVar(data, "data", "", "iteration data file (.csv or .json)")
	iterations := fs.Int("n", 0, "number of iterations")
	fs.IntVar(iterations, "iterations", 0, "number of iterations")
	delay := fs.Int("delay", 0, "delay between requests in milliseconds")
	fs.IntVar(delay, "delay-request", 0, "delay between requests in milliseconds")
	timeout := fs.Duration("timeout", 30*time.Second, "per-request timeout")
	maxResponse := fs.Int("max-response-size", 20, "maximum response body size in MiB")
	bail := fs.Bool("bail", false, "stop on the first failure or error")
	insecure := fs.Bool("insecure", false, "skip TLS certificate verification")
	noScripts := fs.Bool("no-scripts", false, "do not execute pre-request/test scripts")
	reportBodies := fs.Bool("report-bodies", false, "include request/response headers and bodies in the report")
	verbose := fs.Bool("verbose", false, "print every request and console output")
	quiet := fs.Bool("quiet", false, "only print failures and the summary")
	reportFormat := fs.String("report", "cli", "report format: cli, json, or junit")
	exportEnvironment := fs.String("export-environment", "", "write the final environment to this file")
	exportGlobals := fs.String("export-globals", "", "write the final globals to this file")
	output := fs.String("o", "", "write the report to a file")
	fs.StringVar(output, "output", "", "write the report to a file")
	var folders, requests, items stringList
	fs.Var(&items, "i", "run only this request/folder name, UID, or path (repeatable, in order)")
	fs.Var(&items, "item", "run only this request/folder name, UID, or path (repeatable, in order)")
	fs.Var(&folders, "folder", "only run requests under this folder (repeatable)")
	fs.Var(&requests, "request", "only run this request name or path (repeatable)")
	vars := envVars{}
	fs.Var(vars, "env-var", "override an environment variable as KEY=VALUE (repeatable)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: roastman run <collection-dir|workspace|collection.json> [flags]")
		fmt.Fprintln(os.Stderr, "\nRun a collection offline using local native Git files.")
		fs.PrintDefaults()
	}
	valueFlags := map[string]bool{
		"-e": true, "--environment": true, "-g": true, "--globals": true,
		"-d": true, "--data": true, "-n": true, "--iterations": true,
		"--delay": true, "--delay-request": true, "--timeout": true,
		"--max-response-size": true, "--report": true, "-o": true, "--output": true,
		"--collection": true, "-i": true, "--item": true, "--folder": true, "--request": true, "--env-var": true,
		"--export-environment": true, "--export-globals": true,
	}
	flagArgs, positional := reorderFlags(args, valueFlags)
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if len(positional) != 1 {
		fs.Usage()
		return errors.New("expected exactly one collection path")
	}
	switch strings.ToLower(*reportFormat) {
	case "cli", "json", "junit":
	default:
		return fmt.Errorf("unknown report format %q (use cli, json, or junit)", *reportFormat)
	}
	var printer *runner.CLIPrinter
	liveCLI := strings.EqualFold(*reportFormat, "cli") && *output == "" && !*quiet
	opts := runner.Options{
		CollectionPath: positional[0],
		Collection:     *collectionName,
		Environment:    *environment,
		Globals:        *globals,
		Data:           *data,
		Iterations:     *iterations,
		Folders:        folders,
		Requests:       requests,
		Items:          items,
		DelayMS:        *delay,
		TimeoutMS:      int((*timeout).Milliseconds()),
		MaxResponseMB:  *maxResponse,
		Bail:           *bail,
		Insecure:       *insecure,
		NoScripts:      *noScripts,
		ReportBodies:   *reportBodies,
		EnvOverrides:   vars,
		Verbose:        *verbose,
		OnItem: func(item runner.ItemReport) {
			if printer != nil {
				printer.Item(item)
			}
		},
	}
	r, err := runner.New(opts)
	if err != nil {
		return err
	}
	if liveCLI {
		printer = runner.NewCLIPrinter(os.Stdout, *verbose, *reportBodies)
		printer.Header(r.CollectionName(), r.EnvironmentName())
	}
	report, runErr := r.Run(context.Background())
	if *exportEnvironment != "" {
		if err := r.ExportEnvironment(*exportEnvironment); err != nil && runErr == nil {
			runErr = err
		}
	}
	if *exportGlobals != "" {
		if err := r.ExportGlobals(*exportGlobals); err != nil && runErr == nil {
			runErr = err
		}
	}
	if runErr != nil {
		return runErr
	}
	if *output != "" {
		f, err := os.Create(*output)
		if err != nil {
			return err
		}
		defer f.Close()
		switch strings.ToLower(*reportFormat) {
		case "json":
			err = report.WriteJSON(f)
		case "junit":
			err = report.WriteJUnit(f)
		default:
			report.WriteCLI(f, *verbose, true, *reportBodies)
		}
		if err != nil {
			return err
		}
	} else {
		switch strings.ToLower(*reportFormat) {
		case "json":
			err = report.WriteJSON(os.Stdout)
		case "junit":
			err = report.WriteJUnit(os.Stdout)
		default:
			if liveCLI && printer != nil {
				printer.Summary(report)
			} else {
				report.WriteCLI(os.Stdout, *verbose && !*quiet, true, *reportBodies)
			}
		}
		if err != nil {
			return err
		}
	}
	if report.Failed() {
		return fmt.Errorf("%d failure(s), %d error(s)", report.Failures, report.Errors)
	}
	return nil
}

func printUsage(w *os.File) {
	fmt.Fprintf(w, `roastman %s - convert Postman collections and run them offline

Usage:
  roastman to-files <collection.json> [-o DIR] [--force]
  roastman to-json  <collection-dir>  [-o FILE] [--collection NAME]
  roastman run      <collection-dir|collection.json> [run flags]
  roastman version

Commands:
  to-files   Split a Postman Collection v2.1 JSON file into one YAML file per
             request/folder (Postman native Git v3 layout).
  to-json    Combine a native Git collection directory back into a single
             Postman Collection v2.1 JSON file.
  run        Execute a collection offline and report test results.
  version    Print the version.

Aliases: split/export = to-files, join/import = to-json.

The local layout is compatible with Postman's Native Git workspace:

  <collection>/
    .resources/
      definition.yaml
    Folder/
      .resources/definition.yaml
      Request.request.yaml

An unmodified JSON -> files -> JSON round trip is byte-for-byte identical.
`, version)
}

// reorderFlags lets flags appear before or after positional arguments. The
// standard flag package stops at the first positional argument.
func reorderFlags(args []string, valueFlags map[string]bool) (flags, positional []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			flags = append(flags, a)
			name := a
			if j := strings.IndexByte(name, '='); j >= 0 {
				name = name[:j]
			}
			if valueFlags[name] && !strings.Contains(a, "=") && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
			continue
		}
		positional = append(positional, a)
	}
	return flags, positional
}
