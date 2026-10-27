// full-customer-local executes only closed synthetic transports and owned local fixtures.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/frostloom/ai-gateway/internal/taskbench"
	driver "github.com/go-sql-driver/mysql"
)

func main() { os.Exit(run(os.Args[1:])) }

func localAdminDSN(value string) bool {
	c, e := driver.ParseDSN(value)
	if e != nil || c.DBName != "" || c.Net != "tcp" || !strings.Contains(value, "@tcp("+c.Addr+")/") {
		return false
	}
	host, _, e := net.SplitHostPort(c.Addr)
	ip := net.ParseIP(host)
	return e == nil && ip != nil && ip.IsLoopback()
}

func run(args []string) int {
	f := flag.NewFlagSet("full-customer-local", flag.ContinueOnError)
	f.SetOutput(io.Discard) // Never echo operator arguments or DSN values on parse errors.
	execute := f.Bool("execute-local", false, "explicit local-only fixture execution")
	keysFile := f.String("keys", "", "JSON array of 1..100 primary keys")
	dsn := f.String("mysql-dsn", "", "explicit literal-loopback admin DSN without database")
	revision := f.String("revision", "", "archived executable source revision")
	out := f.String("out", "", "new output directory")
	definitions := f.String("definitions", "eval/task-definitions/2026-10-12-full-dev-v1", "frozen definitions")
	profilesDir := f.String("profiles", "eval/runtime-profiles/2026-10-13-six-profiles-v1", "pinned profiles")
	if f.Parse(args) != nil || f.NArg() != 0 || !*execute || *out == "" || !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(*revision) || !localAdminDSN(*dsn) {
		return 2
	}
	if _, e := os.Lstat(*out); !os.IsNotExist(e) {
		return 2
	}
	raw, e := os.ReadFile(*keysFile)
	if e != nil || len(raw) > 65536 {
		return 2
	}
	var keys []string
	if json.Unmarshal(raw, &keys) != nil {
		return 2
	}
	source, e := taskbench.LoadConversations(*definitions)
	if e != nil {
		return 2
	}
	profiles, e := taskbench.LoadPairedProfiles(*profilesDir)
	if e != nil {
		return 2
	}
	if _, e := taskbench.PrepareFullLocalCustomerWindow(source, profiles, keys, *revision); e != nil {
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()
	env, e := taskbench.OpenPairedFixtureEnvironment(ctx, taskbench.PairedFixtureOptions{AllowFixtureCreation: true, AdminDSN: *dsn, ProfilesDir: *profilesDir})
	if e != nil {
		fmt.Fprintln(os.Stderr, "local fixture initialization failed")
		return 1
	}
	receipt, runErr := taskbench.RunFullLocalCustomerWindow(ctx, *out, taskbench.FullLocalCampaignOptions{DefinitionsDir: *definitions, Environment: env, Revision: *revision, Keys: keys})
	closeErr := env.Close()
	if runErr != nil || closeErr != nil {
		fmt.Fprintln(os.Stderr, "local window stopped; inspect private evidence and fixture cleanup")
		return 1
	}
	fmt.Printf("Local substitutes only: %d pairs, %d arms, %d observed turns; actual evaluation remains incomplete.\n", receipt.CompletedBatches, receipt.Observations.Tasks, receipt.Observations.Turns)
	return 0
}
