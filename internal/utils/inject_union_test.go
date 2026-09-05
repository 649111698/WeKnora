package utils

import (
	"testing"
)

// 默认策略不变：未开 WithAllowCompoundQueries 时 UNION 仍被拒。
func TestValidateSQL_CompoundStillRejectedByDefault(t *testing.T) {
	_, res := ValidateSQL("SELECT 1 AS a FROM t1 UNION ALL SELECT 2 FROM t1",
		WithAllowedTables("t1"), WithSingleStatement(), WithNoDangerousFunctions())
	if res.Valid {
		t.Fatal("compound query must stay rejected without the explicit option")
	}
}
