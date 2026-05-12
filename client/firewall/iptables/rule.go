package iptables

// Rule to handle management of rules
type Rule struct {
	ruleID    string
	ipsetName string

	specs []string
	// connmarkSpecs carries the iptables CONNMARK rule that stamps
	// the agent-local rule_index onto bits 17-31 of the ct mark
	// (ADR-0013) so the netflow conntrack collector resolves the
	// originating PolicyID. nil when the rule has no PolicyID, when
	// the verdict is Drop, or when this Rule struct is a follow-up
	// IP-add to an existing ipset (the first install owns the
	// CONNMARK spec, subsequent ones do not). Same lifecycle as
	// mangleSpecs.
	connmarkSpecs []string
	mangleSpecs   []string
	ip            string
	chain         string
}

// GetRuleID returns the rule id
func (r *Rule) ID() string {
	return r.ruleID
}
