package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// AccountBalance represents one account entry returned by Economy.TopBalances.
type AccountBalance struct {
	Account string
	Balance int64
}

type economySnapshot struct {
	Currency       string           `json:"currency"`
	DefaultBalance int64            `json:"default_balance"`
	Balances       map[string]int64 `json:"balances"`
}

// Economy is a concurrency-safe integer-unit currency ledger (e.g. cents or
// coins) built for plugins, shops, and mini-game rewards.
type Economy struct {
	mu             sync.RWMutex
	currency       string
	defaultBalance int64
	balances       map[string]int64
	onChange       func(account string, before, after int64)
}

// NewEconomy creates a standalone Economy ledger with currency label and
// default starting balance for unseen accounts.
func NewEconomy(currency string, defaultBalance int64) *Economy {
	if currency == "" {
		currency = "coins"
	}
	return &Economy{
		currency:       currency,
		defaultBalance: defaultBalance,
		balances:       make(map[string]int64),
	}
}

// Currency returns the configured currency name.
func (e *Economy) Currency() string {
	if e == nil {
		return ""
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.currency
}

// SetCurrency updates the currency display name.
func (e *Economy) SetCurrency(name string) {
	if e == nil || name == "" {
		return
	}
	e.mu.Lock()
	e.currency = name
	e.mu.Unlock()
}

// SetDefaultBalance sets the initial balance for accounts not yet recorded.
func (e *Economy) SetDefaultBalance(amount int64) {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.defaultBalance = amount
	e.mu.Unlock()
}

// OnChange installs a callback invoked whenever an account's balance changes.
func (e *Economy) OnChange(fn func(account string, before, after int64)) {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.onChange = fn
	e.mu.Unlock()
}

func (e *Economy) balanceLocked(account string) int64 {
	if v, ok := e.balances[account]; ok {
		return v
	}
	return e.defaultBalance
}

// Balance returns account's current balance (or defaultBalance if unseen).
func (e *Economy) Balance(account string) int64 {
	if e == nil || account == "" {
		return 0
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.balanceLocked(account)
}

// SetBalance sets account's balance to amount (clamped to >= 0).
func (e *Economy) SetBalance(account string, amount int64) {
	if e == nil || account == "" {
		return
	}
	amount = max(amount, 0)
	e.mu.Lock()
	before := e.balanceLocked(account)
	e.balances[account] = amount
	cb := e.onChange
	e.mu.Unlock()
	if cb != nil && before != amount {
		cb(account, before, amount)
	}
}

// Deposit adds amount (> 0) to account and returns the updated balance.
func (e *Economy) Deposit(account string, amount int64) int64 {
	if e == nil || account == "" || amount <= 0 {
		return e.Balance(account)
	}
	e.mu.Lock()
	before := e.balanceLocked(account)
	after := before + amount
	e.balances[account] = after
	cb := e.onChange
	e.mu.Unlock()
	if cb != nil {
		cb(account, before, after)
	}
	return after
}

// Withdraw subtracts amount (> 0) from account if sufficient funds exist.
// Returns false without modifying the balance if funds are insufficient.
func (e *Economy) Withdraw(account string, amount int64) bool {
	if e == nil || account == "" || amount <= 0 {
		return false
	}
	e.mu.Lock()
	before := e.balanceLocked(account)
	if before < amount {
		e.mu.Unlock()
		return false
	}
	after := before - amount
	e.balances[account] = after
	cb := e.onChange
	e.mu.Unlock()
	if cb != nil {
		cb(account, before, after)
	}
	return true
}

// Transfer atomically moves amount (> 0) from src to dst if src has sufficient
// balance. Returns true on success.
func (e *Economy) Transfer(src, dst string, amount int64) bool {
	if e == nil || src == "" || dst == "" || src == dst || amount <= 0 {
		return false
	}
	e.mu.Lock()
	srcBefore := e.balanceLocked(src)
	if srcBefore < amount {
		e.mu.Unlock()
		return false
	}
	dstBefore := e.balanceLocked(dst)
	srcAfter := srcBefore - amount
	dstAfter := dstBefore + amount
	e.balances[src] = srcAfter
	e.balances[dst] = dstAfter
	cb := e.onChange
	e.mu.Unlock()
	if cb != nil {
		cb(src, srcBefore, srcAfter)
		cb(dst, dstBefore, dstAfter)
	}
	return true
}

// TopBalances returns up to limit accounts sorted by balance descending (ties
// broken lexicographically by account ID).
func (e *Economy) TopBalances(limit int) []AccountBalance {
	if e == nil || limit <= 0 {
		return nil
	}
	e.mu.RLock()
	out := make([]AccountBalance, 0, len(e.balances))
	for acc, bal := range e.balances {
		out = append(out, AccountBalance{Account: acc, Balance: bal})
	}
	e.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Balance != out[j].Balance {
			return out[i].Balance > out[j].Balance
		}
		return out[i].Account < out[j].Account
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// SaveFile atomically writes the economy ledger as indented JSON to path.
func (e *Economy) SaveFile(path string) error {
	e.mu.RLock()
	snap := economySnapshot{
		Currency:       e.currency,
		DefaultBalance: e.defaultBalance,
		Balances:       e.balances,
	}
	b, err := json.MarshalIndent(snap, "", "  ")
	e.mu.RUnlock()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return NewStorage(filepath.Dir(path)).WriteBytes(filepath.Base(path), append(b, '\n'))
}

// LoadFile loads economy state from a JSON file written by SaveFile.
func (e *Economy) LoadFile(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var snap economySnapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if snap.Currency != "" {
		e.currency = snap.Currency
	}
	e.defaultBalance = snap.DefaultBalance
	if snap.Balances != nil {
		e.balances = snap.Balances
	} else {
		e.balances = make(map[string]int64)
	}
	return nil
}
