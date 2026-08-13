package codex

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type Snapshot struct {
	RateLimits            RateLimitSnapshot            `json:"rateLimits"`
	RateLimitsByLimitID   map[string]RateLimitSnapshot `json:"rateLimitsByLimitId"`
	RateLimitResetCredits *ResetCredits                `json:"rateLimitResetCredits"`
	AccountFingerprint    string                       `json:"-"`
	FetchedAt             time.Time                    `json:"-"`
}

type RateLimitSnapshot struct {
	LimitID              *string          `json:"limitId"`
	LimitName            *string          `json:"limitName"`
	Primary              *Window          `json:"primary"`
	Secondary            *Window          `json:"secondary"`
	PlanType             *string          `json:"planType"`
	RateLimitReachedType *string          `json:"rateLimitReachedType"`
	SpendControlReached  *bool            `json:"spendControlReached"`
	Credits              *Credits         `json:"credits"`
	IndividualLimit      *IndividualLimit `json:"individualLimit"`
}

type Window struct {
	UsedPercent        int    `json:"usedPercent"`
	WindowDurationMins *int64 `json:"windowDurationMins"`
	ResetsAt           *int64 `json:"resetsAt"`
}

type Credits struct {
	HasCredits bool    `json:"hasCredits"`
	Unlimited  bool    `json:"unlimited"`
	Balance    *string `json:"balance"`
}

type IndividualLimit struct {
	Limit            string `json:"limit"`
	Used             string `json:"used"`
	RemainingPercent int    `json:"remainingPercent"`
	ResetsAt         int64  `json:"resetsAt"`
}

type ResetCredits struct {
	AvailableCount int `json:"availableCount"`
}

type Meter struct {
	Bucket  string
	LimitID string
	Name    string
	Window  Window
	Kind    MeterKind
	Details string
}

type MeterKind int

const (
	MeterQuotaWindow MeterKind = iota
	MeterIndividualLimit
)

// Meters flattens every returned limit bucket and window into display order.
func (s Snapshot) Meters() []Meter {
	buckets := s.RateLimitsByLimitID
	if len(buckets) == 0 {
		buckets = map[string]RateLimitSnapshot{"codex": s.RateLimits}
	}

	keys := make([]string, 0, len(buckets))
	for key := range buckets {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i] == "codex" {
			return true
		}
		if keys[j] == "codex" {
			return false
		}
		return keys[i] < keys[j]
	})

	meters := make([]Meter, 0, len(keys)*3)
	for _, key := range keys {
		bucket := buckets[key]
		name := key
		if bucket.LimitName != nil && *bucket.LimitName != "" {
			name = *bucket.LimitName
		}
		if bucket.Primary != nil {
			meters = append(meters, Meter{Bucket: name, LimitID: key, Name: windowName(*bucket.Primary), Window: *bucket.Primary})
		}
		if bucket.Secondary != nil {
			meters = append(meters, Meter{Bucket: name, LimitID: key, Name: windowName(*bucket.Secondary), Window: *bucket.Secondary})
		}
		if bucket.IndividualLimit != nil {
			limit := bucket.IndividualLimit
			var reset *int64
			if limit.ResetsAt > 0 {
				value := limit.ResetsAt
				reset = &value
			}
			details := ""
			if strings.TrimSpace(limit.Used) != "" && strings.TrimSpace(limit.Limit) != "" {
				details = fmt.Sprintf("%s OF %s CREDITS USED", strings.TrimSpace(limit.Used), strings.TrimSpace(limit.Limit))
			}
			meters = append(meters, Meter{
				Bucket:  name,
				LimitID: key,
				Name:    "MONTHLY CREDIT LIMIT",
				Window:  Window{UsedPercent: 100 - limit.RemainingPercent, ResetsAt: reset},
				Kind:    MeterIndividualLimit,
				Details: details,
			})
		}
	}
	return meters
}

// CreditStatus returns the first meaningful account credit balance, preferring
// the primary Codex bucket when the server returns multiple limit buckets.
func (s Snapshot) CreditStatus() (Credits, bool) {
	if len(s.RateLimitsByLimitID) == 0 {
		return meaningfulCredits(s.RateLimits.Credits)
	}
	if bucket, ok := s.RateLimitsByLimitID["codex"]; ok {
		if credits, ok := meaningfulCredits(bucket.Credits); ok {
			return credits, true
		}
	}
	keys := make([]string, 0, len(s.RateLimitsByLimitID))
	for key := range s.RateLimitsByLimitID {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if credits, ok := meaningfulCredits(s.RateLimitsByLimitID[key].Credits); ok {
			return credits, true
		}
	}
	return meaningfulCredits(s.RateLimits.Credits)
}

func meaningfulCredits(credits *Credits) (Credits, bool) {
	hasBalance := credits != nil && credits.Balance != nil && strings.TrimSpace(*credits.Balance) != ""
	if credits == nil || (!credits.HasCredits && !credits.Unlimited && !hasBalance) {
		return Credits{}, false
	}
	return *credits, true
}

func windowName(window Window) string {
	if window.WindowDurationMins == nil {
		return "QUOTA WINDOW"
	}
	minutes := *window.WindowDurationMins
	switch {
	case minutes%10_080 == 0:
		return plural(minutes/10_080, "WEEK")
	case minutes%1_440 == 0:
		return plural(minutes/1_440, "DAY")
	case minutes%60 == 0:
		return plural(minutes/60, "HOUR")
	default:
		return plural(minutes, "MINUTE")
	}
}

func plural(value int64, unit string) string {
	if value == 1 {
		return fmt.Sprintf("1 %s", unit)
	}
	return fmt.Sprintf("%d %sS", value, unit)
}

func DemoSnapshot() Snapshot {
	primaryMinutes := int64(300)
	secondaryMinutes := int64(10_080)
	primaryReset := time.Now().Add(2*time.Hour + 17*time.Minute).Unix()
	secondaryReset := time.Now().Add(4*24*time.Hour + 9*time.Hour).Unix()
	limitID := "codex"
	plan := "plus"
	return Snapshot{
		RateLimits: RateLimitSnapshot{
			LimitID:  &limitID,
			PlanType: &plan,
			Primary:  &Window{UsedPercent: 62, WindowDurationMins: &primaryMinutes, ResetsAt: &primaryReset},
			Secondary: &Window{
				UsedPercent: 37, WindowDurationMins: &secondaryMinutes, ResetsAt: &secondaryReset,
			},
		},
		RateLimitResetCredits: &ResetCredits{AvailableCount: 1},
		FetchedAt:             time.Now(),
	}
}

func DisplayName(value string) string {
	return strings.ToUpper(strings.ReplaceAll(value, "_", " "))
}
