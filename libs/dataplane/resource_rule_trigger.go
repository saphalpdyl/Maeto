package dataplane

import (
	"fmt"
	"slices"
	"strings"
)

// Special resource; not applied to the dataplane, but stored as a state
// trigger by the trigger function ActivateRepair() ResetRepair() etc.
type RuleTrigger struct {
	Egress string
	Rules  []TriggerRule
}

func (t RuleTrigger) ID() ID {
	rules := make([]string, 0, len(t.Rules))
	for _, rule := range t.Rules {
		rules = append(rules, rule.String())
	}

	slices.Sort(rules)
	return ID{
		Kind: KindRuleTrigger,
		Key:  fmt.Sprintf("rule_trigger:%s.%s", t.Egress, strings.Join(rules, "-")),
	}
}

func (t RuleTrigger) CompareTo(actual Resource) Action {
	return ActionNone
}

func (t RuleTrigger) Type() string {
	return "rule_trigger"
}
