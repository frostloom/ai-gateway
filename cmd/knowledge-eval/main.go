package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/knowledgeeval"
	"io"
	"os"
	"path/filepath"
)

func run(args []string) error {
	f := flag.NewFlagSet("knowledge-eval", flag.ContinueOnError)
	input := f.String("input", "", "recorded comparison JSON")
	output := f.String("output", "", "report JSON")
	if e := f.Parse(args); e != nil {
		return e
	}
	if *input == "" || *output == "" || f.NArg() != 0 {
		return fmt.Errorf("input and output are required")
	}
	a, _ := filepath.Abs(*input)
	b, _ := filepath.Abs(*output)
	if a == b {
		return fmt.Errorf("input and output must differ")
	}
	fd, e := os.Open(*input)
	if e != nil {
		return e
	}
	defer fd.Close()
	raw, e := io.ReadAll(io.LimitReader(fd, 8*1024*1024+1))
	if e != nil {
		return e
	}
	if len(raw) > 8*1024*1024 {
		return fmt.Errorf("input exceeds 8 MiB")
	}
	var x knowledgeeval.ComparisonInput
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e = d.Decode(&x); e != nil {
		return e
	}
	if d.Decode(new(any)) != io.EOF {
		return fmt.Errorf("trailing input")
	}
	r, e := knowledgeeval.BuildReport(x)
	if e != nil {
		return e
	}
	raw, e = json.MarshalIndent(r, "", "  ")
	if e != nil {
		return e
	}
	temp, e := os.CreateTemp(filepath.Dir(*output), ".knowledge-report-")
	if e != nil {
		return e
	}
	name := temp.Name()
	defer os.Remove(name)
	if _, e = temp.Write(append(raw, '\n')); e != nil {
		temp.Close()
		return e
	}
	if e = temp.Close(); e != nil {
		return e
	}
	return os.Rename(name, *output)
}
func main() {
	if e := run(os.Args[1:]); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
