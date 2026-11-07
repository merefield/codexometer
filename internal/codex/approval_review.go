package codex

// ApprovalActionDetails remains a comparable value snapshot. It is populated
// only for a complete validated action, never inferred from explanatory text.
type ApprovalActionDetails struct {
	Kind, Target, Protocol, Justification string
}

// ApprovalReview is a browser-safe projection shared with terminal rendering.
// It deliberately excludes request IDs, decision payloads and capabilities.
type ApprovalReview struct {
	Title  string                `json:"title"`
	Fields []ApprovalReviewField `json:"fields"`
}

type ApprovalReviewField struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

func (c SessionContext) ActionReview() *ApprovalReview {
	d := c.ActionDetails
	if d.Kind != "network" {
		return nil
	}
	r := &ApprovalReview{Title: "NETWORK APPROVAL", Fields: []ApprovalReviewField{{"HOST", d.Target}, {"PROTOCOL", d.Protocol}}}
	if d.Justification != "" {
		r.Fields = append(r.Fields, ApprovalReviewField{"JUSTIFICATION", d.Justification})
	}
	for _, option := range c.ApprovalOptions {
		if option.Detail != "" {
			r.Fields = append(r.Fields, ApprovalReviewField{"PERMISSION SCOPE", option.Detail})
		}
	}
	return r
}
