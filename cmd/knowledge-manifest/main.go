package main

import (
	"flag"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/knowledge"
	"os"
	"path/filepath"
)

func run(args []string) error {
	f := flag.NewFlagSet("knowledge-manifest", flag.ContinueOnError)
	dir := f.String("dir", "knowledge/platform", "public corpus directory")
	check := f.Bool("check", false, "validate without writing")
	if e := f.Parse(args); e != nil {
		return e
	}
	if f.NArg() != 0 {
		return fmt.Errorf("unexpected arguments")
	}
	if *check {
		_, e := knowledge.Load(os.DirFS(*dir))
		return e
	}
	b, e := knowledge.GenerateManifest(os.DirFS(*dir))
	if e != nil {
		return e
	}
	return os.WriteFile(filepath.Join(*dir, "manifest.json"), append(b, '\n'), 0644)
}
func main() {
	if e := run(os.Args[1:]); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
