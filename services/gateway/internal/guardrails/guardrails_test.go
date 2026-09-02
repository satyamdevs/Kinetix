package guardrails

import "testing"

func TestFilterRoles(t *testing.T) {
	input, _ := DenyKeyword("block-prompts", "forbidden", "no", RoleInput)
	output, _ := DenyKeyword("block-results", "violent", "no", RoleOutput)
	f := New(input, output)

	if d := f.Check("say forbidden", RoleInput); d.OK {
		t.Fatal("input rule should match input")
	}
	if d := f.Check("say violent", RoleInput); !d.OK {
		t.Fatalf("output phrase should pass input check: %+v", d)
	}
	if d := f.Check("violent stuff", RoleOutput); d.OK {
		t.Fatal("output rule should match output")
	}
	if d := f.Check("forbidden", RoleOutput); !d.OK {
		t.Fatalf("input phrase should pass output check: %+v", d)
	}
}

func TestFilterCaseInsensitive(t *testing.T) {
	rule, _ := DenyKeyword("block", "Forbidden", "no", RoleBoth)
	f := New(rule)
	for _, text := range []string{"forbidden", "FORBIDDEN", "FoRbIdDeN"} {
		if d := f.Check(text, RoleInput); d.OK {
			t.Fatalf("keyword %q must match %q", rule.Pattern, text)
		}
	}
}

func TestFilterMessage(t *testing.T) {
	rule, _ := DenyKeyword("pol", "badword", "content not allowed", RoleInput)
	f := New(rule)
	d := f.Check("containing badword here", RoleInput)
	if d.OK || d.Rule != "pol" || d.Reason != "content not allowed" {
		t.Fatalf("unexpected decision: %+v", d)
	}
}

func TestParseRules(t *testing.T) {
	rules, err := ParseRules([]string{
		"secrets|[A-Z]{4}-[0-9]{4}|input",
		"unsafe|bad\\s+words|output",
		"all|mixed|both",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 3 {
		t.Fatalf("rules = %d, want 3", len(rules))
	}

	f := New(rules...)
	if d := f.Check("my ABCD-1234 here", RoleInput); d.OK {
		t.Fatal("regex input rule should match")
	}
	if d := f.Check("bad words", RoleOutput); d.OK {
		t.Fatal("regex output rule should match")
	}

	if _, err := ParseRules([]string{"broken"}); err == nil {
		t.Fatal("malformed rule should error")
	}
	if _, err := ParseRules([]string{"nope|xx|sideways"}); err == nil {
		t.Fatal("unknown role should error")
	}
}
