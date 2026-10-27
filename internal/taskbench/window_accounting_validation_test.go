package taskbench

import "testing"

// Every owned request checks prior journal entries. Fixed validation patterns
// must not be recompiled for each entry as a campaign grows.
func TestWindowAccountingValidationAllocationBound(t *testing.T) {
	c := windowTestContract()
	r := windowTestRequest("model")
	for _, test := range []struct {
		name string
		run  func() error
	}{
		{"provider", func() error { return c.Model.validate(true) }},
		{"contract", c.validate},
		{"request", func() error { return c.validateRequest(r) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.run(); err != nil {
				t.Fatal(err)
			}
			allocations := testing.AllocsPerRun(10, func() {
				if err := test.run(); err != nil {
					panic(err)
				}
			})
			if allocations > 8 {
				t.Fatalf("fixed validation allocates %.0f objects per entry; want at most 8", allocations)
			}
		})
	}
}
