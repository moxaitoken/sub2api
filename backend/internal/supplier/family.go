package supplier

import "strings"

// Model families shown on the supplier dashboard.
const (
	FamilyOpus   = "opus"
	FamilyFable  = "fable"
	FamilySonnet = "sonnet"
	FamilyHaiku  = "haiku"
	FamilyOther  = "other"
)

var familyOrder = []string{FamilyOpus, FamilyFable, FamilySonnet, FamilyHaiku, FamilyOther}

// ModelFamily maps a model id such as "claude-opus-5-5" or "claude-fable-5-1-20261001"
// to its family. Unknown models fall into FamilyOther.
func ModelFamily(model string) string {
	m := strings.ToLower(model)
	for _, family := range familyOrder[:len(familyOrder)-1] {
		if strings.Contains(m, family) {
			return family
		}
	}
	return FamilyOther
}

// groupByFamily folds per-model rows into one row per family, in familyOrder.
func groupByFamily(rows []UsageRow) []UsageRow {
	totals := make(map[string]*UsageRow, len(familyOrder))
	for _, row := range rows {
		family := ModelFamily(row.Model)
		bucket, ok := totals[family]
		if !ok {
			bucket = &UsageRow{Family: family}
			totals[family] = bucket
		}
		bucket.Add(row.UsageTotals)
	}
	out := make([]UsageRow, 0, len(totals))
	for _, family := range familyOrder {
		if bucket, ok := totals[family]; ok {
			out = append(out, *bucket)
		}
	}
	return out
}
