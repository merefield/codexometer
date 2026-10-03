package codex

// These optional reports mirror Codex's backend-client analytics and plan_history
// contracts. They are NOT token totals and must never enter the token ledger.
import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type UsageReports struct {
	Identity    string            `json:"-"`
	Daily       *DailyUsageReport `json:"daily"`
	Plan        *PlanUsageReport  `json:"plan"`
	DailyStatus string            `json:"dailyStatus"`
	PlanStatus  string            `json:"planStatus"`
}

type DailyUsageReport struct {
	Units     string              `json:"units"`
	DataAsOf  string              `json:"dataAsOf,omitempty"`
	From      string              `json:"from"`
	Through   string              `json:"through"`
	FetchedAt time.Time           `json:"fetchedAt"`
	Days      []UsageBreakdownDay `json:"days"`
}

type UsageBreakdownDay struct {
	Date  string  `json:"date"`
	Total float64 `json:"total"`
	// Each dimension is a different breakdown of the SAME total, not additive.
	Groups map[string]map[string]float64 `json:"groups"`
}

type PlanUsageReport struct {
	FetchedAt         time.Time         `json:"fetchedAt"`
	DataAsOf          string            `json:"data_as_of"`
	CoverageStart     string            `json:"coverage_start"`
	CoverageComplete  bool              `json:"coverage_complete"`
	Approximate       bool              `json:"approximate"`
	BoundaryTolerance *uint32           `json:"boundary_tolerance_seconds"`
	Periods           []PlanUsagePeriod `json:"periods"`
}

type PlanUsagePeriod struct {
	ID                 string               `json:"id"`
	WindowMinutes      uint32               `json:"window_minutes"`
	PlanType           string               `json:"plan_type"`
	StartsAt           string               `json:"starts_at"`
	EndsAt             string               `json:"ends_at"`
	AccountingComplete bool                 `json:"accounting_complete"`
	UsedBasisPoints    *float64             `json:"used_basis_points"`
	Breakdowns         []PlanUsageBreakdown `json:"breakdowns"`
}

type PlanUsageBreakdown struct {
	Dimension string           `json:"dimension"`
	Rows      []PlanUsageValue `json:"rows"`
}

type PlanUsageValue struct {
	Key         string  `json:"key"`
	BasisPoints float64 `json:"basis_points"`
}

type dailyUsageWire struct {
	Units    string `json:"units"`
	DataAsOf string `json:"data_freshness_ts"`
	Data     []struct {
		Date     string             `json:"date"`
		Surfaces map[string]float64 `json:"product_surface_usage_values"`
		Models   []struct {
			Model   string   `json:"model"`
			Credits *float64 `json:"credits"`
		} `json:"models"`
		Attribution []struct {
			Model   string  `json:"model"`
			Surface string  `json:"surface"`
			Feature string  `json:"thread_source"`
			Trigger string  `json:"turn_trigger"`
			Value   float64 `json:"value"`
		} `json:"attribution"`
	} `json:"data"`
}

// Only an in-memory token exported by the user's Codex process is used. No
// auth.json/keyring reading, independent refresh, arbitrary URL or browser token.
type usageCredentials struct{ token, account, user, email string }

func usageAuth(result json.RawMessage) (usageCredentials, bool) {
	var auth struct {
		Method string `json:"authMethod"`
		Token  string `json:"authToken"`
	}
	if json.Unmarshal(result, &auth) != nil || auth.Method != "chatgpt" {
		return usageCredentials{}, false
	}
	parts := strings.Split(auth.Token, ".")
	if len(parts) != 3 {
		return usageCredentials{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return usageCredentials{}, false
	}
	var claims struct {
		Auth struct {
			Account       string `json:"chatgpt_account_id"`
			User          string `json:"chatgpt_user_id"`
			AlternateUser string `json:"user_id"`
			Fedramp       bool   `json:"chatgpt_account_is_fedramp"`
		} `json:"https://api.openai.com/auth"`
		Profile struct {
			Email string `json:"email"`
		} `json:"https://api.openai.com/profile"`
		Email string `json:"email"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return usageCredentials{}, false
	}
	user := claims.Auth.User
	if user == "" {
		user = claims.Auth.AlternateUser
	}
	email := claims.Profile.Email
	if email == "" {
		email = claims.Email
	}
	c := usageCredentials{auth.Token, claims.Auth.Account, user, strings.ToLower(strings.TrimSpace(email))}
	return c, c.account != "" && c.user != "" && c.email != "" && !claims.Auth.Fedramp
}

func (c usageCredentials) identity() string {
	sum := sha256.Sum256([]byte(c.account + "\x00" + c.user))
	return fmt.Sprintf("%x", sum[:])
}

var usageHTTP = &http.Client{
	Timeout: 5 * time.Second,
	// Never follow a credential-bearing request to another endpoint.
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

func readOptionalUsageReports(ctx context.Context, encoder *json.Encoder, decoder *json.Decoder, email string) *UsageReports {
	get := func(id int) (usageCredentials, bool) {
		if encoder.Encode(map[string]any{"id": id, "method": "getAuthStatus", "params": map[string]bool{"includeToken": true}}) != nil {
			return usageCredentials{}, false
		}
		result, err := responseFor(decoder, id)
		if err != nil {
			return usageCredentials{}, false
		}
		return usageAuth(result)
	}
	auth, ok := get(6)
	if !ok || auth.email != email {
		return nil
	}
	reports := fetchUsageReports(ctx, usageHTTP, "https://chatgpt.com/backend-api/wham", auth, time.Now().UTC())
	// Recheck identity before publishing or persisting either report. Token
	// rotation is fine, workspace/user changes are not.
	after, ok := get(7)
	if !ok || after.identity() != auth.identity() || after.email != email {
		return nil
	}
	return reports
}

func readUsageReport(ctx context.Context, client *http.Client, base, route string, auth usageCredentials, target any) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+route, nil)
	if err != nil {
		return "UNAVAILABLE"
	}
	req.Header.Set("Authorization", "Bearer "+auth.token)
	req.Header.Set("ChatGPT-Account-Id", auth.account)
	req.Header.Set("User-Agent", "codexometer")
	response, err := client.Do(req)
	if err != nil {
		return "UNAVAILABLE"
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "UNAVAILABLE"
	}
	// Do not log server bodies, credentials, or transport errors.
	const limit = 4 << 20
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || len(body) > limit || json.Unmarshal(body, target) != nil {
		return "INVALID RESPONSE"
	}
	return "OPENAI"
}

func fetchUsageReports(ctx context.Context, client *http.Client, base string, auth usageCredentials, now time.Time) *UsageReports {
	result := &UsageReports{Identity: auth.identity()}
	from, through := now.UTC().AddDate(0, 0, -29).Format(time.DateOnly), now.UTC().Format(time.DateOnly)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		var wire dailyUsageWire
		result.DailyStatus = readUsageReport(ctx, client, base, "/usage/daily-token-usage-breakdown?group_by=day&start_date="+from+"&end_date="+through, auth, &wire)
		if result.DailyStatus == "OPENAI" {
			var err error
			result.Daily, err = normalizeDailyReport(wire, from, through, now)
			if err != nil {
				result.DailyStatus = "INVALID RESPONSE"
			}
		}
	}()
	go func() {
		defer wg.Done()
		plan := &PlanUsageReport{Approximate: true}
		result.PlanStatus = readUsageReport(ctx, client, base, "/usage/plan_limit_history?days=7", auth, plan)
		if result.PlanStatus == "OPENAI" {
			if validatePlanReport(plan) {
				plan.FetchedAt = now
				result.Plan = plan
			} else {
				result.PlanStatus = "INVALID RESPONSE"
			}
		}
	}()
	wg.Wait()
	return result
}

func validAmount(value float64) bool {
	return value >= 0 && finiteAmount(value)
}

func finiteAmount(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func reportKey(key string) string {
	key = strings.TrimSpace(SanitizeSessionContext(key))
	if key == "" {
		return "unknown"
	}
	runes := []rune(key)
	return string(runes[:min(len(runes), 80)])
}

func normalizeDailyReport(wire dailyUsageWire, from, through string, now time.Time) (*DailyUsageReport, error) {
	bad := errors.New("invalid usage report")
	if wire.Data == nil {
		return nil, bad
	}
	complete := true
	for _, day := range wire.Data {
		if len(day.Date) < 10 || !validUsageDate(day.Date[:10]) {
			return nil, bad
		}
		if day.Date[:10] >= from && day.Date[:10] <= through && day.Attribution == nil {
			complete = false
		}
	}
	result := &DailyUsageReport{Units: "RELATIVE USAGE", From: from, Through: through, FetchedAt: now, Days: []UsageBreakdownDay{}}
	if stamp, err := time.Parse(time.RFC3339, wire.DataAsOf); err == nil {
		result.DataAsOf = stamp.UTC().Format(time.RFC3339Nano)
	}
	if !complete && wire.Units == "credits" {
		result.Units = "CREDITS"
	}
	seen := map[string]bool{}
	for _, day := range wire.Data {
		date := day.Date[:10]
		if date < from || date > through {
			continue
		}
		if seen[date] {
			return nil, bad
		}
		seen[date] = true
		d := UsageBreakdownDay{Date: date, Groups: map[string]map[string]float64{}}
		add := func(dimension, key string, value float64) {
			if d.Groups[dimension] == nil {
				d.Groups[dimension] = map[string]float64{}
			}
			d.Groups[dimension][reportKey(key)] += value
		}
		if complete {
			for _, dimension := range []string{"model", "surface", "feature", "task start"} {
				d.Groups[dimension] = map[string]float64{}
			}
			for _, row := range day.Attribution {
				if !validAmount(row.Value) {
					return nil, bad
				}
				d.Total += row.Value
				add("model", row.Model, row.Value)
				add("surface", row.Surface, row.Value)
				add("feature", row.Feature, row.Value)
				add("task start", row.Trigger, row.Value)
			}
		} else {
			if day.Surfaces == nil {
				return nil, bad
			}
			d.Groups["surface"] = map[string]float64{}
			for surface, value := range day.Surfaces {
				if !validAmount(value) {
					return nil, bad
				}
				d.Total += value
				add("surface", surface, value)
			}
			// Legacy model totals are useful only when reported completely and
			// reconciling with the authoritative surface denominator.
			modelTotal, modelsKnown := 0.0, day.Models != nil
			for _, model := range day.Models {
				if model.Credits == nil || !validAmount(*model.Credits) {
					modelsKnown = false
					break
				}
				modelTotal += *model.Credits
			}
			if modelsKnown && validAmount(modelTotal) && math.Abs(modelTotal-d.Total) <= 1e-8*math.Max(1, d.Total) {
				d.Groups["model"] = map[string]float64{}
				for _, model := range day.Models {
					add("model", model.Model, *model.Credits)
				}
			}
		}
		if !validAmount(d.Total) {
			return nil, bad
		}
		result.Days = append(result.Days, d)
	}
	sort.Slice(result.Days, func(i, j int) bool { return result.Days[i].Date < result.Days[j].Date })
	return result, nil
}

func validatePlanReport(plan *PlanUsageReport) bool {
	if plan.Periods == nil {
		return false
	}
	for _, stamp := range []*string{&plan.DataAsOf, &plan.CoverageStart} {
		if *stamp != "" {
			parsed, err := time.Parse(time.RFC3339, *stamp)
			if err != nil {
				return false
			}
			*stamp = parsed.UTC().Format(time.RFC3339Nano)
		}
	}
	seen := map[string]bool{}
	for i := range plan.Periods {
		p := &plan.Periods[i]
		start, e1 := time.Parse(time.RFC3339, p.StartsAt)
		end, e2 := time.Parse(time.RFC3339, p.EndsAt)
		if p.ID == "" || len(p.ID) > 256 || seen[p.ID] || e1 != nil || e2 != nil || !end.After(start) || p.WindowMinutes == 0 || (p.UsedBasisPoints != nil && !finiteAmount(*p.UsedBasisPoints)) {
			return false
		}
		seen[p.ID] = true
		p.StartsAt = start.UTC().Format(time.RFC3339Nano)
		p.EndsAt = end.UTC().Format(time.RFC3339Nano)
		p.PlanType = reportKey(p.PlanType)
		dimensions := map[string]bool{}
		for j := range p.Breakdowns {
			b := &p.Breakdowns[j]
			b.Dimension = reportKey(b.Dimension)
			if dimensions[b.Dimension] {
				return false
			}
			dimensions[b.Dimension] = true
			total := 0.0
			for k := range b.Rows {
				r := &b.Rows[k]
				if !finiteAmount(r.BasisPoints) {
					return false
				}
				r.Key = reportKey(r.Key)
				total += r.BasisPoints
			}
			if !finiteAmount(total) {
				return false
			}
		}
	}
	sort.Slice(plan.Periods, func(i, j int) bool { return plan.Periods[i].StartsAt > plan.Periods[j].StartsAt })
	return true
}
