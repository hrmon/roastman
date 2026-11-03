package runner

import (
	"strings"
	"testing"
)

func TestBuildPlanDuplicateDisplayNames(t *testing.T) {
	collection := &Collection{Item: []Item{
		{Name: "Folder", Item: []Item{
			{Name: "Same", Request: &Request{}},
			{Name: "Same", Request: &Request{}},
			{Name: "Unique", Request: &Request{}},
		}},
		{Name: "Other", Item: []Item{
			{Name: "Same", Request: &Request{}},
		}},
		{Name: "Same", Request: &Request{}},
	}}

	plan := buildPlan(collection)
	want := []string{"Same (1/2)", "Same (2/2)", "Unique", "Same", "Same"}
	if len(plan) != len(want) {
		t.Fatalf("plan length = %d, want %d", len(plan), len(want))
	}
	for i, name := range want {
		if plan[i].DisplayName != name {
			t.Errorf("plan[%d].DisplayName = %q, want %q", i, plan[i].DisplayName, name)
		}
	}

	selected, err := selectPlan(plan, Options{Items: []string{"Same (2/2)"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 1 || selected[0] != plan[1] {
		t.Fatalf("selected = %v, want the second duplicate", selected)
	}
}

func TestWriteCLIColors(t *testing.T) {
	report := &Report{
		Collection: "Colors",
		Requests:   1,
		Assertions: 1,
		Items: []ItemReport{{
			Name:           "OK",
			Method:         "GET",
			Code:           200,
			DurationMS:     5,
			ResponseTimeMS: 5,
			Tests:          []TestResult{{Name: "status", Passed: true}},
		}},
	}

	var plain strings.Builder
	report.WriteCLI(&plain, true, true)
	if strings.Contains(plain.String(), "\x1b[") {
		t.Fatalf("no-color output contains ANSI escapes:\n%s", plain.String())
	}

	var colored strings.Builder
	report.WriteCLI(&colored, true, false)
	if !strings.Contains(colored.String(), "\x1b[") {
		t.Fatalf("colored output contains no ANSI escapes:\n%s", colored.String())
	}
	for _, want := range []string{"\x1b[1m", "\x1b[32m", "\x1b[36m"} {
		if !strings.Contains(colored.String(), want) {
			t.Errorf("colored output is missing %q:\n%s", want, colored.String())
		}
	}
}

func TestCLIPrinterDuplicateNamesAndSummary(t *testing.T) {
	var out strings.Builder
	printer := NewCLIPrinter(&out, true, false)
	printer.Color = true
	printer.Header("Dup", "Local")
	printer.Item(ItemReport{
		Name:        "Same",
		DisplayName: "Same (1/2)",
		Folder:      "Folder",
		Method:      "GET",
		Code:        200,
		DurationMS:  3,
		Tests:       []TestResult{{Name: "ok", Passed: true}},
	})
	printer.Item(ItemReport{
		Name:        "Same",
		DisplayName: "Same (2/2)",
		Folder:      "Folder",
		Method:      "GET",
		Code:        500,
		DurationMS:  4,
		Failed:      true,
		Error:       "boom",
	})
	printer.Summary(&Report{
		Collection: "Dup",
		Requests:   2,
		Errors:     1,
		Items: []ItemReport{
			{Name: "Same", DisplayName: "Same (1/2)", Method: "GET", Code: 200},
			{Name: "Same", DisplayName: "Same (2/2)", Method: "GET", Code: 500, Failed: true, Error: "boom"},
		},
	})

	got := out.String()
	for _, want := range []string{"Same (1/2)", "Same (2/2)", "1 problem(s) found."} {
		if !strings.Contains(got, want) {
			t.Errorf("output is missing %q:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "\x1b[") {
		t.Errorf("colored printer output contains no ANSI escapes:\n%s", got)
	}
}

func TestWriteJUnitUsesDisplayNames(t *testing.T) {
	report := &Report{
		Collection: "Dup",
		Requests:   1,
		Failures:   1,
		Items: []ItemReport{{
			Name:        "Same",
			DisplayName: "Same (2/2)",
			Folder:      "Folder",
			Method:      "GET",
			Code:        500,
			Failed:      true,
			Tests:       []TestResult{{Name: "status", Passed: false, Error: "expected 200"}},
		}},
	}
	var out strings.Builder
	if err := report.WriteJUnit(&out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `name="Same (2/2) :: status"`) {
		t.Fatalf("junit output does not use display name:\n%s", out.String())
	}
}
