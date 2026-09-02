package guardrails

import (
	"fmt"
	"regexp"
	"strings"
)

// Role selects whether a rule inspects inbound prompts, outbound completions,
// or both.
type Role int

const (
	RoleInput Role = iota
	RoleOutput
	RoleBoth
)

// Rule is a single content policy check. A match blocks traffic.
type Rule struct {
	Name    string
	Pattern *regexp.Regexp
	Role    Role
	Message string
}

// Filter evaluates a set of rules against request input and/or model output.
type Filter struct {
	rules []Rule
}

func New(rules ...Rule) *Filter {
	return &Filter{rules: rules}
}

// Len returns the number of configured rules.
func (f *Filter) Len() int {
	if f == nil {
		return 0
	}
	return len(f.rules)
}

// Check scans text for any rule active in the given role.
func (f *Filter) Check(text string, role Role) Decision {
	for _, r := range f.rules {
		if r.Role != role && r.Role != RoleBoth {
			continue
		}
		if r.Pattern.MatchString(text) {
			reason := r.Message
			if reason == "" {
				reason = fmt.Sprintf("blocked by guardrail %q", r.Name)
			}
			return Decision{OK: false, Rule: r.Name, Reason: reason}
		}
	}
	return Decision{OK: true}
}

// DenyKeyword builds a case-insensitive substring deny rule.
func DenyKeyword(name, keyword, message string, role Role) (Rule, error) {
	pattern, err := regexp.Compile(`(?i)` + regexp.QuoteMeta(keyword))
	if err != nil {
		return Rule{}, err
	}
	return Rule{Name: name, Pattern: pattern, Role: role, Message: message}, nil
}

// DenyExpression builds a deny rule from an arbitrary regular expression.
func DenyExpression(name, expression, message string, role Role) (Rule, error) {
	pattern, err := regexp.Compile(expression)
	if err != nil {
		return Rule{}, err
	}
	return Rule{Name: name, Pattern: pattern, Role: role, Message: message}, nil
}

// ParseRules parses "name|regex|role" entries from an environment-style list
// (comma-separated). Roles are "input", "output", or "both".
func ParseRules(entries []string) ([]Rule, error) {
	var rules []Rule
	for _, entry := range entries {
		parts := strings.SplitN(entry, "|", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("guardrail %q: want name|regex|role", entry)
		}
		var role Role
		switch strings.ToLower(parts[2]) {
		case "input":
			role = RoleInput
		case "output":
			role = RoleOutput
		case "both":
			role = RoleBoth
		default:
			return nil, fmt.Errorf("guardrail %q: unknown role %q", entry, parts[2])
		}
		rule, err := DenyExpression(parts[0], parts[1], "", role)
		if err != nil {
			return nil, fmt.Errorf("guardrail %q: %w", entry, err)
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

type Decision struct {
	OK     bool
	Rule   string
	Reason string
}
