//go:build unix

package codex

import (
	"context"
	"errors"
)

func (p *daemonStatusProvider) readUsageCoverage(ctx context.Context, ids []string) map[string]string {
	result := make(map[string]string)
	for _, id := range ids {
		if !p.isSubscribed(id) {
			continue
		}
		issue, err := p.threadUsageCoverage(ctx, id)
		if err != nil {
			issue = "effective background-compaction feature state could not be verified"
		}
		result[id] = issue
	}
	return result
}

func (p *daemonStatusProvider) threadUsageCoverage(ctx context.Context, id string) (string, error) {
	params := map[string]any{"threadId": id, "limit": 256}
	seen := make(map[string]bool)
	async, tokenBudget := false, false
	for page := 0; page < 32; page++ {
		var response struct {
			Data *[]struct {
				Name    string
				Enabled *bool
			}
			NextCursor *string
		}
		if err := p.request(ctx, "experimentalFeature/list", params, &response); err != nil {
			return "", err
		}
		if response.Data == nil {
			return "", errors.New("feature inventory omitted data")
		}
		for _, feature := range *response.Data {
			if feature.Name != "async_compaction" && feature.Name != "token_budget" {
				continue
			}
			if feature.Enabled == nil {
				return "", errors.New("feature inventory omitted enablement")
			}
			if feature.Name == "async_compaction" {
				async = *feature.Enabled
			} else {
				tokenBudget = *feature.Enabled
			}
		}
		if response.NextCursor == nil || *response.NextCursor == "" {
			if async && !tokenBudget {
				return asyncCompactionCoverage, nil
			}
			return "", nil
		}
		if seen[*response.NextCursor] {
			return "", errors.New("feature inventory repeated a cursor")
		}
		seen[*response.NextCursor] = true
		params["cursor"] = *response.NextCursor
	}
	return "", errors.New("feature inventory exceeded pagination limit")
}
