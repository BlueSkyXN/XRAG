package store

import (
	"context"
	"encoding/json"
	"math"
	"time"
	"github.com/BlueSkyXN/XRAG/core"
	db "github.com/BlueSkyXN/XRAG/internal/sqlite"
)

type Rate struct {
	Input  int64 `json:"input_micro_per_million"`
	Output int64 `json:"output_micro_per_million"`
}
type BudgetPolicy struct {
	MaxCalls    int64           `json:"max_calls_per_run,omitempty"`
	MaxTokens   int64           `json:"max_tokens_per_run,omitempty"`
	DailyTokens int64           `json:"max_tokens_per_day,omitempty"`
	MaxCost     int64           `json:"max_cost_micro_per_run,omitempty"`
	DailyCost   int64           `json:"max_cost_micro_per_day,omitempty"`
	Rates       map[string]Rate `json:"rates,omitempty"`
}

func ValidateBudget(p BudgetPolicy) error {
	if p.MaxCalls < 0 || p.MaxTokens < 0 || p.DailyTokens < 0 || p.MaxCost < 0 || p.DailyCost < 0 {
		return core.Err("INVALID_ARGUMENT", "budgets cannot be negative", 400)
	}
	for _, r := range p.Rates {
		if r.Input < 0 || r.Output < 0 || r.Input > 1000000000 || r.Output > 1000000000 {
			return core.Err("INVALID_ARGUMENT", "invalid model price", 400)
		}
	}
	return nil
}

type Meter struct {
	Store          *Store
	KB, Run, Owner string
	Policy         BudgetPolicy
}

func (m *Meter) Reserve(ctx context.Context, op string, inputBytes, maxOutput int) (string, error) {
	id := core.ID("call_")
	p := m.Policy
	if p.MaxCalls == 0 {
		p.MaxCalls = 200
	}
	in, out := int64(inputBytes), int64(maxOutput)
	tokens := in + out
	rate, priced := p.Rates[op]
	if (p.MaxCost > 0 || p.DailyCost > 0) && !priced {
		return "", core.Err("BUDGET_PRICE_MISSING", "configured cost limit requires an explicit price for "+op, 422)
	}
	cost := (in*rate.Input + out*rate.Output + 999999) / 1000000
	now := time.Now().Unix()
	day := time.Now().UTC().Format("2006-01-02")
	e := m.Store.DB.Tx(ctx, true, func(c *db.Conn) error {
		r, e := c.Query("SELECT COALESCE(SUM(n),0) n,COALESCE(SUM(t),0) t,COALESCE(SUM(c),0) c FROM(SELECT COUNT(*) n,SUM(charged_tokens) t,SUM(charged_cost) c FROM budget_call WHERE run=? AND kb=? UNION ALL SELECT SUM(calls),SUM(tokens),SUM(cost) FROM budget_rollup WHERE run=? AND kb=?)", m.Run, m.KB, m.Run, m.KB)
		if e != nil {
			return e
		}
		if db.Int(r[0], "n") >= p.MaxCalls || p.MaxTokens > 0 && db.Int(r[0], "t")+tokens > p.MaxTokens || p.MaxCost > 0 && db.Int(r[0], "c")+cost > p.MaxCost {
			return core.Err("BUDGET_EXCEEDED", "run budget would be exceeded; call was not sent", 429)
		}
		r, e = c.Query("SELECT COALESCE(SUM(t),0) t,COALESCE(SUM(c),0) c FROM(SELECT SUM(charged_tokens) t,SUM(charged_cost) c FROM budget_call WHERE kb=? AND day=? UNION ALL SELECT SUM(tokens),SUM(cost) FROM budget_rollup WHERE kb=? AND day=?)", m.KB, day, m.KB, day)
		if e != nil {
			return e
		}
		if p.DailyTokens > 0 && db.Int(r[0], "t")+tokens > p.DailyTokens || p.DailyCost > 0 && db.Int(r[0], "c")+cost > p.DailyCost {
			return core.Err("BUDGET_EXCEEDED", "daily budget would be exceeded; call was not sent", 429)
		}
		return c.Exec("INSERT INTO budget_call(id,kb,run,owner,day,operation,state,reserved_input,reserved_output,charged_tokens,charged_cost,rate,data,created) VALUES(?,?,?,?,?,?,'sent',?,?,?,?,?,'{}',?)", id, m.KB, m.Run, m.Owner, day, op, in, out, tokens, cost, core.JSON(rate), now)
	})
	return id, e
}
func usageInt(m map[string]any, k string) int64 {
	if f, ok := m[k].(float64); ok && f >= 0 && f <= 1000000000 && !math.IsNaN(f) && !math.IsInf(f, 0) {
		return int64(f)
	}
	if f, ok := m[k].(int); ok && f >= 0 {
		return int64(f)
	}
	return 0
}
func (m *Meter) Complete(ctx context.Context, id string, u map[string]any, success bool) error {
	return m.Store.DB.Tx(ctx, true, func(c *db.Conn) error {
		r, e := c.Query("SELECT * FROM budget_call WHERE id=?", id)
		if e != nil {
			return e
		}
		if len(r) != 1 {
			return core.Err("NOT_FOUND", "call reservation missing", 404)
		}
		if db.String(r[0], "state") != "sent" {
			return nil
		}
		state := "outcome_unknown"
		tok, cost := db.Int(r[0], "charged_tokens"), db.Int(r[0], "charged_cost")
		in, out := usageInt(u, "prompt_tokens"), usageInt(u, "completion_tokens")
		if in == 0 {
			in = usageInt(u, "input_tokens")
		}
		if out == 0 {
			out = usageInt(u, "output_tokens")
		}
		total := usageInt(u, "total_tokens")
		if success {
			state = "usage_unknown"
			if in+out > 0 {
				tok = in + out
				var rate Rate
				_ = json.Unmarshal([]byte(db.String(r[0], "rate")), &rate)
				cost = (in*rate.Input + out*rate.Output + 999999) / 1000000
				state = "completed"
			} else if total > 0 {
				tok = total
				state = "cost_unknown"
			}
		}
		// If the provider reports usage above our conservative reservation, record it rather than pretending the cap was guaranteed.
		return c.Exec("UPDATE budget_call SET state=?,charged_tokens=?,charged_cost=?,data=? WHERE id=? AND state='sent'", state, tok, cost, core.JSON(u), id)
	})
}
func (s *Store) BudgetReport(ctx context.Context, kb string) (any, error) {
	out := map[string]any{"kb": kb, "unit": "configured_currency_micro", "reservation_basis": "UTF-8 request bytes as conservative token allowance plus configured output cap; not tokenizer measurements"}
	e := s.DB.Do(ctx, func(c *db.Conn) error {
		r, e := c.Query("SELECT operation,state,SUM(calls) calls,SUM(tokens) tokens,SUM(cost_micro) cost_micro FROM(SELECT operation,state,1 calls,charged_tokens tokens,charged_cost cost_micro FROM budget_call WHERE kb=? UNION ALL SELECT operation,state,calls,tokens,cost FROM budget_rollup WHERE kb=?) GROUP BY operation,state", kb, kb)
		out["totals"] = r
		if e != nil {
			return e
		}
		r, e = c.Query("SELECT id,run,operation,state,reserved_input,reserved_output,charged_tokens,charged_cost,created FROM budget_call WHERE kb=? ORDER BY created DESC,id LIMIT 100", kb)
		out["recent"] = r
		out["totals_include_archived_rollups"] = true
		return e
	})
	return out, e
}
