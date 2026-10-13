package scoreboard

import (
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Criteria identifies the source used to update an objective's scores.
type Criteria string

const (
	Dummy           Criteria = "dummy"
	Trigger         Criteria = "trigger"
	Health          Criteria = "health"
	DeathCount      Criteria = "deathCount"
	PlayerKillCount Criteria = "playerKillCount"
	TotalKillCount  Criteria = "totalKillCount"
	Level           Criteria = "level"
	Food            Criteria = "food"
	XP              Criteria = "xp"
	Air             Criteria = "air"
	Armor           Criteria = "armor"
)

var CriteriaNames = []Criteria{Dummy, Trigger, Health, DeathCount, PlayerKillCount, TotalKillCount, Level, Food, XP, Air, Armor}

// Objective is a named collection of entity scores.
type Objective struct {
	Name        string         `json:"name"`
	DisplayName string         `json:"display_name"`
	Criteria    Criteria       `json:"criteria"`
	Scores      map[string]int `json:"scores"`
}

// Team contains team membership and display rules.
type Team struct {
	Name              string          `json:"name"`
	DisplayName       string          `json:"display_name"`
	Prefix            string          `json:"prefix"`
	Suffix            string          `json:"suffix"`
	Color             string          `json:"color"`
	FriendlyFire      bool            `json:"friendly_fire"`
	NameTagVisibility string          `json:"nametag_visibility"`
	Collision         string          `json:"collision"`
	Members           map[string]bool `json:"members"`
}

// BossBar is a server-wide named boss bar.
type BossBar struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Value   float64  `json:"value"`
	Max     float64  `json:"max"`
	Color   string   `json:"color"`
	Visible bool     `json:"visible"`
	Players []string `json:"players,omitempty"`
}

// Model stores objectives, teams and boss bars. Its methods are safe for concurrent use.
type Model struct {
	mu             sync.RWMutex
	Objectives     map[string]*Objective `json:"objectives"`
	Teams          map[string]*Team      `json:"teams"`
	Displays       map[string]string     `json:"displays"`
	BossBars       map[string]*BossBar   `json:"bossbars"`
	TriggerEnabled map[string]bool       `json:"trigger_enabled"`
}

func NewModel() *Model {
	return &Model{Objectives: map[string]*Objective{}, Teams: map[string]*Team{}, Displays: map[string]string{}, BossBars: map[string]*BossBar{}, TriggerEnabled: map[string]bool{}}
}

var ErrExists = errors.New("already exists")
var ErrNotFound = errors.New("not found")
var ErrInvalid = errors.New("invalid value")

func (m *Model) AddObjective(name, display string, criteria Criteria) error {
	if name == "" || len(name) > 16 || display == "" {
		return ErrInvalid
	}
	valid := false
	for _, c := range CriteriaNames {
		if criteria == c {
			valid = true
			break
		}
	}
	if !valid {
		return ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Objectives[name] != nil {
		return ErrExists
	}
	m.Objectives[name] = &Objective{Name: name, DisplayName: display, Criteria: criteria, Scores: map[string]int{}}
	return nil
}
func (m *Model) RemoveObjective(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Objectives[name] == nil {
		return false
	}
	delete(m.Objectives, name)
	for slot, objective := range m.Displays {
		if objective == name {
			delete(m.Displays, slot)
		}
	}
	return true
}
func (m *Model) Objective(name string) (Objective, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	o := m.Objectives[name]
	if o == nil {
		return Objective{}, false
	}
	copy := *o
	copy.Scores = make(map[string]int, len(o.Scores))
	for n, s := range o.Scores {
		copy.Scores[n] = s
	}
	return copy, true
}
func (m *Model) SetScore(objective, entity string, value int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	o := m.Objectives[objective]
	if o == nil {
		return ErrNotFound
	}
	if entity == "" {
		return ErrInvalid
	}
	o.Scores[entity] = value
	return nil
}
func (m *Model) Score(objective, entity string) (int, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	o := m.Objectives[objective]
	if o == nil {
		return 0, false
	}
	v, ok := o.Scores[entity]
	return v, ok
}
func (m *Model) AddScore(objective, entity string, delta int) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o := m.Objectives[objective]
	if o == nil {
		return 0, ErrNotFound
	}
	if entity == "" {
		return 0, ErrInvalid
	}
	o.Scores[entity] += delta
	return o.Scores[entity], nil
}
func (m *Model) ResetScore(objective, entity string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	o := m.Objectives[objective]
	if o == nil {
		return false
	}
	if entity == "" {
		clear(o.Scores)
		return true
	}
	_, ok := o.Scores[entity]
	delete(o.Scores, entity)
	return ok
}
func (m *Model) SetDisplay(slot, objective string) error {
	if slot != "list" && slot != "sidebar" && slot != "belowName" {
		return ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if objective != "" && m.Objectives[objective] == nil {
		return ErrNotFound
	}
	if objective == "" {
		delete(m.Displays, slot)
	} else {
		m.Displays[slot] = objective
	}
	return nil
}
func (m *Model) AddTeam(name, display string) error {
	if name == "" || len(name) > 16 {
		return ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Teams[name] != nil {
		return ErrExists
	}
	m.Teams[name] = &Team{Name: name, DisplayName: display, Color: "white", FriendlyFire: true, NameTagVisibility: "always", Collision: "always", Members: map[string]bool{}}
	return nil
}
func (m *Model) RemoveTeam(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Teams[name] == nil {
		return false
	}
	delete(m.Teams, name)
	return true
}
func (m *Model) JoinTeam(team string, members ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.Teams[team]
	if t == nil {
		return ErrNotFound
	}
	for _, mbr := range members {
		for _, other := range m.Teams {
			delete(other.Members, mbr)
		}
		t.Members[mbr] = true
	}
	return nil
}
func (m *Model) LeaveTeam(members ...string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, t := range m.Teams {
		for _, mbr := range members {
			if t.Members[mbr] {
				delete(t.Members, mbr)
				n++
			}
		}
	}
	return n
}
func (m *Model) TeamFor(member string) (Team, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, t := range m.Teams {
		if t.Members[member] {
			c := *t
			c.Members = make(map[string]bool, len(t.Members))
			for k, v := range t.Members {
				c.Members[k] = v
			}
			return c, true
		}
	}
	return Team{}, false
}
func (m *Model) SetTriggerEnabled(entity string, enabled bool) {
	m.mu.Lock()
	m.TriggerEnabled[entity] = enabled
	m.mu.Unlock()
}
func (m *Model) TriggerIsEnabled(entity string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.TriggerEnabled[entity]
}
func (m *Model) TriggerUse(objective, entity string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	o := m.Objectives[objective]
	if o == nil {
		return ErrNotFound
	}
	if o.Criteria != Trigger {
		return ErrInvalid
	}
	if !m.TriggerEnabled[entity] {
		return ErrInvalid
	}
	o.Scores[entity]++
	m.TriggerEnabled[entity] = false
	return nil
}

func (m *Model) MarshalJSON() ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	type model Model
	return json.Marshal((*model)(m))
}
func (m *Model) UnmarshalJSON(data []byte) error {
	type model Model
	var saved model
	if err := json.Unmarshal(data, &saved); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Objectives = saved.Objectives
	m.Teams = saved.Teams
	m.Displays = saved.Displays
	m.BossBars = saved.BossBars
	m.TriggerEnabled = saved.TriggerEnabled
	if m.Objectives == nil {
		m.Objectives = map[string]*Objective{}
	}
	if m.Teams == nil {
		m.Teams = map[string]*Team{}
	}
	if m.Displays == nil {
		m.Displays = map[string]string{}
	}
	if m.BossBars == nil {
		m.BossBars = map[string]*BossBar{}
	}
	if m.TriggerEnabled == nil {
		m.TriggerEnabled = map[string]bool{}
	}
	return nil
}
func (m *Model) ObjectiveNames() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.Objectives))
	for n := range m.Objectives {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// AddBossBar registers a new boss bar.
func (m *Model) AddBossBar(bar BossBar) error {
	if bar.ID == "" || bar.Name == "" || bar.Max <= 0 || bar.Value < 0 || bar.Value > bar.Max {
		return ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.BossBars[bar.ID] != nil {
		return ErrExists
	}
	copy := bar
	m.BossBars[bar.ID] = &copy
	return nil
}

// RemoveBossBar removes a boss bar by identifier.
func (m *Model) RemoveBossBar(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.BossBars[id] == nil {
		return false
	}
	delete(m.BossBars, id)
	return true
}

// BossBar returns a copy of a boss bar.
func (m *Model) BossBar(id string) (BossBar, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	bar := m.BossBars[id]
	if bar == nil {
		return BossBar{}, false
	}
	return *bar, true
}

// Operate applies an operation atomically to two stored scores.
func (m *Model) Operate(targetObjective, target, operator, sourceObjective, source string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	dst, src := m.Objectives[targetObjective], m.Objectives[sourceObjective]
	if dst == nil || src == nil {
		return ErrNotFound
	}
	a, aOK := dst.Scores[target]
	b, bOK := src.Scores[source]
	if !aOK || !bOK {
		return ErrNotFound
	}
	switch operator {
	case "=":
		dst.Scores[target] = b
	case "+=":
		dst.Scores[target] = a + b
	case "-=":
		dst.Scores[target] = a - b
	case "*=":
		dst.Scores[target] = a * b
	case "/=":
		if b == 0 {
			return ErrInvalid
		}
		dst.Scores[target] = a / b
	case "%=":
		if b == 0 {
			return ErrInvalid
		}
		dst.Scores[target] = a % b
	case "<":
		if b < a {
			dst.Scores[target] = b
		}
	case ">":
		if b > a {
			dst.Scores[target] = b
		}
	case "><":
		dst.Scores[target], src.Scores[source] = b, a
	default:
		return ErrInvalid
	}
	return nil
}

// DisplayObjective returns the objective assigned to a display slot.
func (m *Model) DisplayObjective(slot string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	name, ok := m.Displays[slot]
	return name, ok
}

// ModifyTeam updates one of the team display and interaction rules.
func (m *Model) ModifyTeam(name, property, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	team := m.Teams[name]
	if team == nil {
		return ErrNotFound
	}
	switch property {
	case "displayName":
		team.DisplayName = value
	case "prefix":
		team.Prefix = value
	case "suffix":
		team.Suffix = value
	case "color":
		switch value {
		case "black", "dark_blue", "dark_green", "dark_aqua", "dark_red", "dark_purple", "gold", "gray", "dark_gray", "blue", "green", "aqua", "red", "light_purple", "yellow", "white", "reset":
			team.Color = value
		default:
			return ErrInvalid
		}
	case "friendlyFire":
		switch value {
		case "true":
			team.FriendlyFire = true
		case "false":
			team.FriendlyFire = false
		default:
			return ErrInvalid
		}
	case "nametagVisibility":
		switch value {
		case "always", "never", "hideForOtherTeams", "hideForOwnTeam":
			team.NameTagVisibility = value
		default:
			return ErrInvalid
		}
	case "collisionRule":
		switch value {
		case "always", "never", "pushOtherTeams", "pushOwnTeam":
			team.Collision = value
		default:
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

// ModifyBossBar updates a named boss bar property.
func (m *Model) ModifyBossBar(id, property, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	bar := m.BossBars[id]
	if bar == nil {
		return ErrNotFound
	}
	switch property {
	case "name":
		if value == "" {
			return ErrInvalid
		}
		bar.Name = value
	case "value":
		v, err := strconv.ParseFloat(value, 64)
		if err != nil || v < 0 || v > bar.Max {
			return ErrInvalid
		}
		bar.Value = v
	case "max":
		v, err := strconv.ParseFloat(value, 64)
		if err != nil || v <= 0 || v < bar.Value {
			return ErrInvalid
		}
		bar.Max = v
	case "color":
		switch value {
		case "pink", "blue", "red", "green", "yellow", "purple", "white":
			bar.Color = value
		default:
			return ErrInvalid
		}
	case "visible":
		v, err := strconv.ParseBool(value)
		if err != nil {
			return ErrInvalid
		}
		bar.Visible = v
	case "players":
		if value == "" || value == "*" {
			bar.Players = nil
		} else {
			bar.Players = strings.Split(value, ",")
		}
	default:
		return ErrInvalid
	}
	return nil
}

// Team returns a copy of a named team.
func (m *Model) Team(name string) (Team, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	team := m.Teams[name]
	if team == nil {
		return Team{}, false
	}
	copy := *team
	copy.Members = make(map[string]bool, len(team.Members))
	for member, present := range team.Members {
		copy.Members[member] = present
	}
	return copy, true
}
