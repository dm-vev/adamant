// Package perm provides an optional hierarchical permission and RBAC group
// store with wildcard grants ("*", "myplugin.*"), explicit negations
// ("-myplugin.admin"), group inheritance, and operator fallback.
//
// Register a *perm.Store via plugin.Provide(app, store) to share it across
// plugins without coupling permissions to the core App.
package perm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/dm-vev/adamant/server"
	"github.com/dm-vev/adamant/server/cmd"
	"github.com/dm-vev/adamant/server/player"
)

type groupData struct {
	Parents []string        `json:"parents,omitempty"`
	Nodes   map[string]bool `json:"nodes,omitempty"`
}

type subjectData struct {
	Groups []string        `json:"groups,omitempty"`
	Nodes  map[string]bool `json:"nodes,omitempty"`
}

type snapshot struct {
	DefaultGroup string                  `json:"default_group,omitempty"`
	Groups       map[string]*groupData   `json:"groups,omitempty"`
	Subjects     map[string]*subjectData `json:"subjects,omitempty"`
}

// Store evaluates and persists hierarchical permissions and RBAC groups.
type Store struct {
	mu           sync.RWMutex
	srv          *server.Server
	defaultGroup string
	groups       map[string]*groupData
	subjects     map[string]*subjectData
}

// New creates a Store optionally linked to srv for operator fallback checks.
func New(srv *server.Server) *Store {
	return &Store{
		srv:          srv,
		defaultGroup: "default",
		groups:       map[string]*groupData{"default": {Nodes: make(map[string]bool)}},
		subjects:     make(map[string]*subjectData),
	}
}

// SetDefaultGroup sets the group inherited automatically by every subject.
func (s *Store) SetDefaultGroup(group string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.defaultGroup = strings.ToLower(strings.TrimSpace(group))
	if s.defaultGroup != "" {
		s.ensureGroupLocked(s.defaultGroup)
	}
}

func (s *Store) ensureGroupLocked(name string) *groupData {
	name = strings.ToLower(strings.TrimSpace(name))
	g, ok := s.groups[name]
	if !ok {
		g = &groupData{Nodes: make(map[string]bool)}
		s.groups[name] = g
	}
	if g.Nodes == nil {
		g.Nodes = make(map[string]bool)
	}
	return g
}

func (s *Store) ensureSubjectLocked(id string) *subjectData {
	sub, ok := s.subjects[id]
	if !ok {
		sub = &subjectData{Nodes: make(map[string]bool)}
		s.subjects[id] = sub
	}
	if sub.Nodes == nil {
		sub.Nodes = make(map[string]bool)
	}
	return sub
}

func normalizeNode(node string) (key string, allow bool) {
	node = strings.ToLower(strings.TrimSpace(node))
	if key, negated := strings.CutPrefix(node, "-"); negated {
		return key, false
	}
	return node, true
}

// GrantGroup grants (or negates with leading "-") node on group.
func (s *Store) GrantGroup(group, node string) {
	if s == nil || group == "" || node == "" {
		return
	}
	key, allow := normalizeNode(node)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureGroupLocked(group).Nodes[key] = allow
}

// RevokeGroup removes node from group.
func (s *Store) RevokeGroup(group, node string) {
	if s == nil || group == "" || node == "" {
		return
	}
	key, _ := normalizeNode(node)
	s.mu.Lock()
	defer s.mu.Unlock()
	if g, ok := s.groups[strings.ToLower(strings.TrimSpace(group))]; ok {
		delete(g.Nodes, key)
	}
}

// Inherit makes group inherit permissions from parent.
func (s *Store) Inherit(group, parent string) {
	if s == nil || group == "" || parent == "" {
		return
	}
	group = strings.ToLower(strings.TrimSpace(group))
	parent = strings.ToLower(strings.TrimSpace(parent))
	if group == parent {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.ensureGroupLocked(group)
	s.ensureGroupLocked(parent)
	if !slices.Contains(g.Parents, parent) {
		g.Parents = append(g.Parents, parent)
	}
}

// Grant grants (or negates with leading "-") node directly on subjectID.
func (s *Store) Grant(subjectID, node string) {
	if s == nil || subjectID == "" || node == "" {
		return
	}
	key, allow := normalizeNode(node)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureSubjectLocked(subjectID).Nodes[key] = allow
}

// Revoke removes a directly assigned node from subjectID.
func (s *Store) Revoke(subjectID, node string) {
	if s == nil || subjectID == "" || node == "" {
		return
	}
	key, _ := normalizeNode(node)
	s.mu.Lock()
	defer s.mu.Unlock()
	if sub, ok := s.subjects[subjectID]; ok {
		delete(sub.Nodes, key)
	}
}

// GrantPlayer grants (or negates) node on player p's UUID.
func (s *Store) GrantPlayer(p *player.Player, node string) {
	if p != nil {
		s.Grant(p.UUID().String(), node)
	}
}

// RevokePlayer removes a directly assigned node from player p's UUID.
func (s *Store) RevokePlayer(p *player.Player, node string) {
	if p != nil {
		s.Revoke(p.UUID().String(), node)
	}
}

// AddGroup assigns group to subjectID.
func (s *Store) AddGroup(subjectID, group string) {
	if s == nil || subjectID == "" || group == "" {
		return
	}
	group = strings.ToLower(strings.TrimSpace(group))
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureGroupLocked(group)
	sub := s.ensureSubjectLocked(subjectID)
	if !slices.Contains(sub.Groups, group) {
		sub.Groups = append(sub.Groups, group)
	}
}

// RemoveGroup unassigns group from subjectID.
func (s *Store) RemoveGroup(subjectID, group string) {
	if s == nil || subjectID == "" || group == "" {
		return
	}
	group = strings.ToLower(strings.TrimSpace(group))
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.subjects[subjectID]
	if !ok {
		return
	}
	sub.Groups = slices.DeleteFunc(sub.Groups, func(g string) bool { return g == group })
}

// GroupsOf returns the groups directly assigned to subjectID.
func (s *Store) GroupsOf(subjectID string) []string {
	if s == nil || subjectID == "" {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	sub, ok := s.subjects[subjectID]
	if !ok {
		return nil
	}
	return slices.Clone(sub.Groups)
}

// Groups returns a sorted list of all defined group names.
func (s *Store) Groups() []string {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.groups))
	for k := range s.groups {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// Has evaluates whether subjectID holds permission node.
func (s *Store) Has(subjectID, node string) bool {
	if s == nil || node == "" {
		return false
	}
	node = strings.ToLower(strings.TrimSpace(node))
	s.mu.RLock()
	defer s.mu.RUnlock()
	val, decided := s.evalSubjectLocked(subjectID, node)
	return decided && val
}

// PlayerHas evaluates whether player p holds node. Server operators default to
// true unless explicitly negated via "-node".
func (s *Store) PlayerHas(p *player.Player, node string) bool {
	if s == nil || p == nil || node == "" {
		return false
	}
	node = strings.ToLower(strings.TrimSpace(node))
	id := p.UUID()
	s.mu.RLock()
	val, decided := s.evalSubjectLocked(id.String(), node)
	srv := s.srv
	s.mu.RUnlock()
	if decided {
		return val
	}
	return srv != nil && (srv.PlayerIsOperator(id) || srv.IsOperator(p.Name()))
}

// Require returns a command Allow filter requiring players to hold node.
func (s *Store) Require(node string) func(cmd.Source) bool {
	return func(src cmd.Source) bool {
		p, ok := src.(*player.Player)
		if !ok {
			return true
		}
		return s.PlayerHas(p, node)
	}
}

func (s *Store) evalSubjectLocked(subjectID, perm string) (bool, bool) {
	if sub, ok := s.subjects[subjectID]; ok {
		if val, ok := matchNodeMap(sub.Nodes, perm); ok {
			return val, true
		}
		if val, ok := s.evalGroupsLocked(sub.Groups, perm, make(map[string]bool)); ok {
			return val, true
		}
	}
	if s.defaultGroup != "" {
		if val, ok := s.evalGroupsLocked([]string{s.defaultGroup}, perm, make(map[string]bool)); ok {
			return val, true
		}
	}
	return false, false
}

func (s *Store) evalGroupsLocked(groups []string, perm string, visited map[string]bool) (bool, bool) {
	hasAllow := false
	for _, name := range groups {
		if visited[name] {
			continue
		}
		visited[name] = true
		g, ok := s.groups[name]
		if !ok {
			continue
		}
		if val, matched := matchNodeMap(g.Nodes, perm); matched {
			if !val {
				return false, true
			}
			hasAllow = true
		}
		if len(g.Parents) > 0 {
			if val, matched := s.evalGroupsLocked(g.Parents, perm, visited); matched {
				if !val {
					return false, true
				}
				hasAllow = true
			}
		}
	}
	return hasAllow, hasAllow
}

func matchNodeMap(nodes map[string]bool, perm string) (bool, bool) {
	if len(nodes) == 0 {
		return false, false
	}
	if val, ok := nodes[perm]; ok {
		return val, true
	}
	parts := strings.Split(perm, ".")
	for i := len(parts) - 1; i >= 1; i-- {
		wild := strings.Join(parts[:i], ".") + ".*"
		if val, ok := nodes[wild]; ok {
			return val, true
		}
	}
	if val, ok := nodes["*"]; ok {
		return val, true
	}
	return false, false
}

// SaveFile atomically writes the permission store as indented JSON to path.
func (s *Store) SaveFile(path string) error {
	s.mu.RLock()
	snap := snapshot{
		DefaultGroup: s.defaultGroup,
		Groups:       s.groups,
		Subjects:     s.subjects,
	}
	b, err := json.MarshalIndent(snap, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

// LoadFile replaces store state from a JSON file written by SaveFile.
func (s *Store) LoadFile(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var snap snapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.defaultGroup = snap.DefaultGroup
	if snap.Groups != nil {
		s.groups = snap.Groups
	} else {
		s.groups = make(map[string]*groupData)
	}
	if snap.Subjects != nil {
		s.subjects = snap.Subjects
	} else {
		s.subjects = make(map[string]*subjectData)
	}
	return nil
}
