package codex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const dailyReportFixture = `{"units":"credits","data":[{"date":"2026-10-02T00:00:00Z","product_surface_usage_values":{"codex":999},"attribution":[{"model":"gpt-6.1-sol","surface":"cli","thread_source":"agent","turn_trigger":"user","value":2.5},{"model":"gpt-6-luna","surface":"cli","thread_source":"subagent","turn_trigger":"agent","value":1.5}]}]}`
const planReportFixture = `{"coverage_complete":false,"coverage_start":"2026-09-26T00:00:00Z","periods":[{"id":"period-1","window_minutes":10080,"plan_type":"pro","starts_at":"2026-09-28T00:00:00Z","ends_at":"2026-10-05T00:00:00Z","accounting_complete":false,"used_basis_points":12500,"breakdowns":[{"dimension":"model","rows":[{"key":"gpt-6.1-sol","basis_points":12500}]}]}]}`

func TestNormalizeUsageReports(t *testing.T) {
	var wire dailyUsageWire
	if err := json.Unmarshal([]byte(dailyReportFixture), &wire); err != nil {
		t.Fatal(err)
	}
	r, err := normalizeDailyReport(wire, "2026-09-04", "2026-10-03", time.Now())
	if err != nil || r.Units != "RELATIVE USAGE" || len(r.Days) != 1 || r.Days[0].Total != 4 || r.Days[0].Groups["model"]["gpt-6.1-sol"] != 2.5 {
		t.Fatalf("normalization: %+v %v", r, err)
	}
	// Attribution is used once, never added to the parallel surface totals.
	for _, dimension := range []string{"model", "surface", "feature", "task start"} {
		total := 0.0
		for _, value := range r.Days[0].Groups[dimension] {
			total += value
		}
		if total != 4 {
			t.Fatalf("%s double counted", dimension)
		}
	}
	for _, bad := range []string{`{}`, `{"data":[{"date":"bad"}]}`, `{"data":[{"date":"2026-10-02","product_surface_usage_values":{"cli":-1}}]}`, `{"data":[{"date":"2026-10-02"}]}`} {
		var w dailyUsageWire
		_ = json.Unmarshal([]byte(bad), &w)
		if _, err := normalizeDailyReport(w, "2026-09-04", "2026-10-03", time.Now()); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
	// One missing attribution bucket disables those dimensions for the whole
	// refresh range. Legacy surface data stays useful without invented models.
	var legacy dailyUsageWire
	_ = json.Unmarshal([]byte(`{"units":"credits","data":[{"date":"2026-10-01","product_surface_usage_values":{"cli":2}},{"date":"2026-10-02","product_surface_usage_values":{"cli":3},"attribution":[{"model":"wrong","value":30}]}]}`), &legacy)
	r, err = normalizeDailyReport(legacy, "2026-10-01", "2026-10-03", time.Now())
	if err != nil || r.Units != "CREDITS" || r.Days[1].Total != 3 || r.Days[1].Groups["model"] != nil {
		t.Fatalf("legacy %+v %v", r, err)
	}
}

func TestFetchUsageReportsContractsAndIndependentFailures(t *testing.T) {
	for _, planStatus := range []int{200, 404, 401, 500} {
		t.Run(fmt.Sprint(planStatus), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer secret" || r.Header.Get("ChatGPT-Account-Id") != "account" || r.Method != "GET" {
					t.Error("incorrect authenticated read")
				}
				switch r.URL.Path {
				case "/usage/daily-token-usage-breakdown":
					if r.URL.Query().Get("start_date") != "2026-09-04" || r.URL.Query().Get("end_date") != "2026-10-03" || r.URL.Query().Get("group_by") != "day" {
						t.Error("range contract")
					}
					fmt.Fprint(w, dailyReportFixture)
				case "/usage/plan_limit_history":
					if r.URL.RawQuery != "days=7" {
						t.Error("plan range contract")
					}
					w.WriteHeader(planStatus)
					fmt.Fprint(w, planReportFixture)
				default:
					t.Error("unexpected route")
				}
			}))
			defer server.Close()
			r := fetchUsageReports(context.Background(), server.Client(), server.URL, usageCredentials{token: "secret", account: "account", user: "user"}, time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC))
			if r.Daily == nil || r.DailyStatus != "OPENAI" || (r.Plan != nil) != (planStatus == 200) {
				t.Fatalf("independent reports %+v", r)
			}
			if r.Plan != nil && (!r.Plan.Approximate || *r.Plan.Periods[0].UsedBasisPoints != 12500) {
				t.Fatal("lost approximation or >100%")
			}
			serialized, _ := json.Marshal(r)
			if strings.Contains(string(serialized), "secret") || strings.Contains(string(serialized), r.Identity) {
				t.Fatal("leaked credentials/identity")
			}
		})
	}
}

func TestUsageReportTransportBoundsAndRedirects(t *testing.T) {
	redirected := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected = true }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, target.URL, 302)
		case "/large":
			fmt.Fprint(w, strings.Repeat(" ", 4<<20)+"{}")
		default:
			fmt.Fprint(w, "sensitive invalid body")
		}
	}))
	defer server.Close()
	for _, route := range []string{"/redirect", "/large", "/invalid"} {
		var result any
		status := readUsageReport(context.Background(), usageHTTP, server.URL, route, usageCredentials{}, &result)
		if status == "OPENAI" || strings.Contains(status, "sensitive") {
			t.Fatalf("unsafe response %s", status)
		}
	}
	if redirected {
		t.Fatal("followed authenticated redirect")
	}
}

func TestUsageAuth(t *testing.T) {
	claims := `{"https://api.openai.com/auth":{"chatgpt_account_id":"account","chatgpt_user_id":"user"},"https://api.openai.com/profile":{"email":"User@Example.com"}}`
	token := "header." + base64.RawURLEncoding.EncodeToString([]byte(claims)) + ".signature"
	raw, _ := json.Marshal(map[string]string{"authMethod": "chatgpt", "authToken": token})
	c, ok := usageAuth(raw)
	if !ok || c.account != "account" || c.email != "user@example.com" {
		t.Fatal("valid exported auth rejected")
	}
	other := c
	other.account = "other"
	if c.identity() == other.identity() {
		t.Fatal("workspace identities collide")
	}
	for _, raw := range []string{`{}`, `{"authMethod":"apiKey","authToken":"secret"}`, `{"authMethod":"chatgpt","authToken":"a.b.c"}`} {
		if _, ok := usageAuth([]byte(raw)); ok {
			t.Fatal("invalid auth accepted")
		}
	}
}

type usageRoundTrip func(*http.Request) (*http.Response, error)

func (f usageRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOptionalUsageReportsRecheckWorkspaceAndSurviveTokenEndpointFailure(t *testing.T) {
	t.Setenv("CODEXOMETER_FAKE_APP_SERVER", "1")
	tokenFor := func(account string) string {
		return "header." + base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"https://api.openai.com/auth":{"chatgpt_account_id":%q,"chatgpt_user_id":"user"},"email":"user@example.com"}`, account))) + ".signature"
	}
	t.Setenv("CODEXOMETER_FAKE_USAGE_AUTH", tokenFor("a"))
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	previous := usageHTTP
	t.Cleanup(func() { usageHTTP = previous })
	usageHTTP = &http.Client{Transport: usageRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "chatgpt.com" || !strings.HasPrefix(r.URL.Path, "/backend-api/wham/usage/") {
			t.Error("unexpected credential destination")
		}
		body := dailyReportFixture
		if strings.Contains(r.URL.Path, "plan_limit_history") {
			body = planReportFixture
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	client := Client{Binary: exe}
	h, err := client.FetchAccountUsage(context.Background())
	if err != nil || h.Reports == nil || h.Reports.Plan == nil {
		t.Fatalf("reports unavailable %v", err)
	}
	t.Setenv("CODEXOMETER_FAKE_USAGE_ERROR", "1")
	h, err = client.FetchAccountUsage(context.Background())
	if err != nil || h.TokenStatus != "UNAVAILABLE" || h.Reports == nil || h.Reports.Plan == nil {
		t.Fatal("token endpoint failure hid separate report")
	}
	t.Setenv("CODEXOMETER_FAKE_USAGE_ERROR", "")
	t.Setenv("CODEXOMETER_FAKE_USAGE_AUTH_AFTER", tokenFor("b"))
	h, err = client.FetchAccountUsage(context.Background())
	if err != nil || h.Reports != nil || len(h.DailyUsageBuckets) == 0 {
		t.Fatal("workspace change failed closed incorrectly")
	}
}

func TestPlanReportUnknownAndValidation(t *testing.T) {
	var p PlanUsageReport
	if err := json.Unmarshal([]byte(planReportFixture), &p); err != nil {
		t.Fatal(err)
	}
	p.Periods[0].UsedBasisPoints = nil
	if !validatePlanReport(&p) || p.Periods[0].UsedBasisPoints != nil {
		t.Fatal("unknown became zero")
	}
	negative := -25.0
	p.Periods[0].UsedBasisPoints = &negative
	p.Periods[0].Breakdowns[0].Rows[0].BasisPoints = negative
	if !validatePlanReport(&p) {
		t.Fatal("signed correction rejected")
	}
	p.Periods[0].Breakdowns = append(p.Periods[0].Breakdowns, p.Periods[0].Breakdowns[0])
	if validatePlanReport(&p) {
		t.Fatal("duplicate dimensions double count")
	}
	_ = json.Unmarshal([]byte(planReportFixture), &p)
	p.Periods[0].EndsAt = p.Periods[0].StartsAt
	if validatePlanReport(&p) {
		t.Fatal("invalid boundaries accepted")
	}
}

func TestUsageReportsRemoveCorrectedPeriodsAndPruneStale(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	stored := &storedUsageReports{Identity: "a", Daily: &DailyUsageReport{Days: []UsageBreakdownDay{{Date: "2020-01-01"}}}, Plan: &PlanUsageReport{Periods: []PlanUsagePeriod{
		{ID: "older", StartsAt: "2026-08-01T00:00:00Z", EndsAt: "2026-08-08T00:00:00Z"},
		{ID: "removed", StartsAt: "2026-10-01T00:00:00Z", EndsAt: "2026-10-08T00:00:00Z"},
	}}}
	incoming := &UsageReports{Identity: "a", DailyStatus: "UNAVAILABLE", PlanStatus: "OPENAI", Plan: &PlanUsageReport{CoverageStart: "2026-09-26T00:00:00Z", Periods: []PlanUsagePeriod{}}}
	r := reconcileUsageReports(&stored, incoming, now)
	if len(r.Plan.Periods) != 1 || r.Plan.Periods[0].ID != "older" || len(r.Daily.Days) != 0 || r.Daily.Days == nil {
		t.Fatal("bad retention/correction")
	}
}

func TestHistorySchemaOneMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"accounts":{"a":{"summary":{"lifetimeTokens":42},"days":{}}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	h, err := (&HistoryStore{Path: path}).Reconcile(AccountUsage{AccountFingerprint: "a"}, nil)
	if err != nil || h.Summary.LifetimeTokens == nil || *h.Summary.LifetimeTokens != 42 {
		t.Fatalf("migration lost history %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(raw), `"version": 2`) {
		t.Fatal("schema not migrated")
	}
}

func TestUsageReportsPersistenceIsolationAndCorrections(t *testing.T) {
	now := time.Now().UTC()
	date := now.Format(time.DateOnly)
	store := &HistoryStore{Path: filepath.Join(t.TempDir(), "usage.json")}
	r := AccountUsage{AccountFingerprint: "email-hash", FetchedAt: now, Reports: &UsageReports{Identity: "workspace-a", DailyStatus: "OPENAI", PlanStatus: "UNAVAILABLE", Daily: &DailyUsageReport{Units: "RELATIVE USAGE", From: date, Through: date, FetchedAt: now, Days: []UsageBreakdownDay{{Date: date, Total: 4}}}}}
	first, err := store.Reconcile(r, nil)
	if err != nil || first.Reports.Daily.Days[0].Total != 4 {
		t.Fatal(err)
	}
	r.Reports.Daily.Days[0].Total = 3
	second, err := store.Reconcile(r, nil)
	if err != nil || second.Reports.Daily.Days[0].Total != 3 {
		t.Fatal("updates must replace, not accumulate")
	}
	reopened := &HistoryStore{Path: store.Path}
	r.Reports.Daily = nil
	r.Reports.DailyStatus = "UNAVAILABLE"
	stale, err := reopened.Reconcile(r, nil)
	if err != nil || stale.Reports.Daily.Days[0].Total != 3 || !strings.HasPrefix(stale.Reports.DailyStatus, "STALE") {
		t.Fatal("verified cache not reused")
	}
	r.Reports.Identity = "workspace-b"
	other, err := reopened.Reconcile(r, nil)
	if err != nil || other.Reports.Daily != nil {
		t.Fatal("workspace data crossed identity boundary")
	}
	r.Reports = nil
	missing, err := reopened.Reconcile(r, nil)
	if err != nil || missing.Reports != nil {
		t.Fatal("unverified analytics returned")
	}
}

// Explicit opt-in diagnostic prints report status/counts only, never tokens or
// response bodies. It is not run by CI and does not write the history ledger.
func TestLiveUsageReports(t *testing.T) {
	if os.Getenv("CODEXOMETER_TEST_LIVE_USAGE") != "1" {
		t.Skip("opt-in local integration")
	}
	h, err := (Client{}).FetchAccountUsage(context.Background())
	if err != nil {
		t.Fatal("live account usage failed")
	}
	if h.Reports == nil {
		t.Fatal("analytics auth unavailable")
	}
	t.Logf("daily=%s plan=%s", h.Reports.DailyStatus, h.Reports.PlanStatus)
	if h.Reports.Daily == nil {
		t.Fatal("live daily report unavailable")
	}
	t.Logf("daily buckets=%d units=%s", len(h.Reports.Daily.Days), h.Reports.Daily.Units)
}
