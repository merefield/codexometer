package codex

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"strings"
	"unicode"
)

func decodeApprovalObject(raw json.RawMessage, value any) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return len(raw) <= approvalTextLimit && json.Valid(raw) && decoder.Decode(value) == nil
}

func configureNetworkApproval(c *SessionContext, raw json.RawMessage, decisions []json.RawMessage, reason string) {
	c.Kind, c.ApprovalBlocked = SessionContextApproval, "network"
	c.Text = reason + "\nNetwork request: " + string(raw)
	var network struct {
		Host     string `json:"host"`
		Protocol string `json:"protocol"`
	}
	if !decodeApprovalObject(raw, &network) || network.Host == "" || len(network.Host) > 1024 || !approvalTextDisplayable(network.Host) || strings.IndexFunc(network.Host, unicode.IsSpace) >= 0 || strings.ContainsAny(network.Host, " \t\r\n/@?#\\") {
		return
	}
	switch network.Protocol {
	case "http", "https", "socks5Tcp", "socks5Udp":
	default:
		return
	}
	if !approvalTextFits(c.Text) {
		c.ApprovalBlocked = "truncated"
		return
	}
	if !approvalTextDisplayable(c.Text) {
		c.ApprovalBlocked = "sanitised"
		return
	}
	var options [8]ApprovalOption
	n := 0
	for _, raw := range decisions {
		parsed, _ := commandApprovalOptions([]json.RawMessage{raw})
		option := parsed[0]
		if oneTimeApproval(option.Kind) || option.Kind == "acceptForSession" {
			if option.Kind == "acceptForSession" {
				option.Detail = "Allow this host for the current Codex conversation."
			}
		} else {
			option = ApprovalOption{}
			var rule struct {
				Amendment *struct {
					Policy *struct {
						Host   string `json:"host"`
						Action string `json:"action"`
					} `json:"network_policy_amendment"`
				} `json:"applyNetworkPolicyAmendment"`
			}
			if decodeApprovalObject(raw, &rule) && rule.Amendment != nil && rule.Amendment.Policy != nil {
				policy := rule.Amendment.Policy
				if policy.Host == network.Host && (policy.Action == "allow" || policy.Action == "deny") {
					kind := "allowNetworkHost"
					if policy.Action == "deny" {
						kind = "denyNetworkHost"
					}
					// Preserve the exact advertised object; never construct a wider rule.
					option = ApprovalOption{Kind: kind, Value: string(raw), Wire: string(raw), Detail: "Future network requests: " + policy.Action + " " + policy.Host}
				}
			}
		}
		if option.Kind == "" || n == len(options) {
			continue
		}
		duplicate := false
		for _, old := range options {
			duplicate = duplicate || old.Kind == option.Kind
		}
		if !duplicate {
			options[n] = option
			n++
		}
	}
	if n == 0 {
		c.ApprovalBlocked = "decisions"
		return
	}
	for i, option := range options {
		if option.Kind == "accept" {
			copy(options[1:i+1], options[:i])
			options[0] = option
			break
		}
	}
	c.ActionDetails = ApprovalActionDetails{Kind: "network", Target: network.Host, Protocol: network.Protocol, Justification: reason}
	c.ApprovalOptions = options
	for i, option := range options {
		if option.Kind != "" {
			if i > 0 {
				c.ApprovalDecisions += ", "
			}
			c.ApprovalDecisions += option.Kind
		}
	}
	c.ApprovalToken, c.ApprovalBlocked = rand.Text(), ""
}
