package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/google/uuid"
)

type permissionGroup struct {
	Parents []string        `json:"parents,omitempty"`
	Nodes   map[string]bool `json:"nodes,omitempty"`
}

type permissionSubject struct {
	Groups []string        `json:"groups,omitempty"`
	Nodes  map[string]bool `json:"nodes,omitempty"`
}

type permissionSnapshot struct {
	DefaultGroup string                        `json:"default_group,omitempty"`
	Groups       map[string]*permissionGroup   `json:"groups,omitempty"`
	Subjects     map[string]*permissionSubject `json:"subjects,omitempty"`
}

// PermissionStore manages hierarchical plugin permissions, RBAC groups with
// inheritance, wildcard grants ("*", "myplugin.*"), explicit negations
// ("-myplugin.admin"), and integration with Adamant's server operators.
type PermissionStore struct {
	mu           sync.RWMutex
	srv          *server.Server
	defaultGroup string
	groups       map[string]*permissionGroup
	subjects     map[string]*permissionSubject
}

// NewPermissionStore creates a PermissionStore optionally linked to srv for
// operator permission checks.
func NewPermissionStore(srv *server.Server) *PermissionStore {
	return &PermissionStore{
		srv:          srv,
		defaultGroup: "default",
		groups:       map[string]*permissionGroup{"default": {Nodes: make(map[string]bool)}},
		subjects:     make(map[string]*permissionSubject),
	}
}

// SetDefaultGroup sets the group inherited by every subject automatically.
func (s *PermissionStore) SetDefaultGroup(group string) {
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

func (s *PermissionStore) ensureGroupLocked(name string) *permissionGroup {
	name = strings.ToLower(strings.TrimSpace(name))
	g, ok := s.groups[name]
	if !ok {
		g = &permissionGroup{Nodes: make(map[string]bool)}
		s.groups[name] = g
	}
	if g.Nodes == nil {
		g.Nodes = make(map[string]bool)
	}
	return g
}

func (s *PermissionStore) ensureSubjectLocked(id string) *permissionSubject {
	sub, ok := s.subjects[id]
	if !ok {
		sub = &permissionSubject{Nodes: make(map[string]bool)}
		s.subjects[id] = sub
	}
	if sub.Nodes == nil {
		sub.Nodes = make(map[string]bool)
	}
	return sub
}

func normalizePermNode(node string) (key string, allow bool) {
	node = strings.ToLower(strings.TrimSpace(node))
	if key, negated := strings.CutPrefix(node, "-"); negated {
		return key, false
	}
	return node, true
}

// GrantGroup grants (or negates with leading "-") a permission node on group.
func (s *PermissionStore) GrantGroup(group, node string) {
	if s == nil || group == "" || node == "" {
		return
	}
	key, allow := normalizePermNode(node)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureGroupLocked(group).Nodes[key] = allow
}

// RevokeGroup removes a permission node from group.
func (s *PermissionStore) RevokeGroup(group, node string) {
	if s == nil || group == "" || node == "" {
		return
	}
	key, _ := normalizePermNode(node)
	s.mu.Lock()
	defer s.mu.Unlock()
	if g, ok := s.groups[strings.ToLower(strings.TrimSpace(group))]; ok {
		delete(g.Nodes, key)
	}
}

// AddGroupParent makes group inherit all permissions from parent.
func (s *PermissionStore) AddGroupParent(group, parent string) {
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

// Grant grants (or negates with leading "-") node directly on subject id.
func (s *PermissionStore) Grant(subjectID, node string) {
	if s == nil || subjectID == "" || node == "" {
		return
	}
	key, allow := normalizePermNode(node)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureSubjectLocked(subjectID).Nodes[key] = allow
}

// Revoke removes a directly assigned permission node from subject id.
func (s *PermissionStore) Revoke(subjectID, node string) {
	if s == nil || subjectID == "" || node == "" {
		return
	}
	key, _ := normalizePermNode(node)
	s.mu.Lock()
	defer s.mu.Unlock()
	if sub, ok := s.subjects[subjectID]; ok {
		delete(sub.Nodes, key)
	}
}

// GrantPlayer grants (or negates with leading "-") node for player p's UUID.
func (s *PermissionStore) GrantPlayer(p *player.Player, node string) {
	if p != nil {
		s.Grant(p.UUID().String(), node)
	}
}

// RevokePlayer removes a directly assigned node from player p's UUID.
func (s *PermissionStore) RevokePlayer(p *player.Player, node string) {
	if p != nil {
		s.Revoke(p.UUID().String(), node)
	}
}

// AddPlayerGroup assigns group to subject id.
func (s *PermissionStore) AddPlayerGroup(subjectID, group string) {
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

// RemovePlayerGroup unassigns group from subject id.
func (s *PermissionStore) RemovePlayerGroup(subjectID, group string) {
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

// PlayerGroups returns the groups directly assigned to subjectID.
func (s *PermissionStore) PlayerGroups(subjectID string) []string {
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

// Has evaluates whether subjectID holds permission perm, supporting exact
// nodes, prefix wildcards ("myplugin.*", "*"), group inheritance, and explicit
// negation ("-perm").
func (s *PermissionStore) Has(subjectID, perm string) bool {
	if s == nil || perm == "" {
		return false
	}
	perm = strings.ToLower(strings.TrimSpace(perm))
	s.mu.RLock()
	defer s.mu.RUnlock()
	val, decided := s.evalSubjectLocked(subjectID, perm)
	return decided && val
}

// PlayerHas evaluates whether player p holds perm. Server operators are granted
// all permissions by default unless explicitly negated on the player or their
// groups (e.g. "-myplugin.restricted").
func (s *PermissionStore) PlayerHas(p *player.Player, perm string) bool {
	if s == nil || p == nil || perm == "" {
		return false
	}
	perm = strings.ToLower(strings.TrimSpace(perm))
	id := p.UUID()
	s.mu.RLock()
	val, decided := s.evalSubjectLocked(id.String(), perm)
	srv := s.srv
	s.mu.RUnlock()
	if decided {
		return val
	}
	if srv != nil && (srv.PlayerIsOperator(id) || srv.IsOperator(p.Name())) {
		return true
	}
	return false
}

// RequirePermission returns a CommandSpec.Allow filter requiring players to
// hold perm in store (console/non-player sources are allowed).
func RequirePermission(store *PermissionStore, perm string) func(src cmd.Source) bool {
	return func(src cmd.Source) bool {
		p, ok := src.(*player.Player)
		if !ok {
			return true
		}
		return store.PlayerHas(p, perm)
	}
}

func (s *PermissionStore) evalSubjectLocked(subjectID, perm string) (bool, bool) {
	if sub, ok := s.subjects[subjectID]; ok {
		if val, ok := matchNodeMap(sub.Nodes, perm); ok {
			return val, true
		}
		visited := make(map[string]bool)
		if val, ok := s.evalGroupsLocked(sub.Groups, perm, visited); ok {
			return val, true
		}
	}
	if s.defaultGroup != "" {
		visited := make(map[string]bool)
		if val, ok := s.evalGroupsLocked([]string{s.defaultGroup}, perm, visited); ok {
			return val, true
		}
	}
	return false, false
}

func (s *PermissionStore) evalGroupsLocked(groups []string, perm string, visited map[string]bool) (bool, bool) {
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
	if hasAllow {
		return true, true
	}
	return false, false
}

// matchNodeMap checks most-specific to least-specific candidates:
// exact "a.b.c" -> "a.b.*" -> "a.*" -> "*".
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
func (s *PermissionStore) SaveFile(path string) error {
	s.mu.RLock()
	snap := permissionSnapshot{
		DefaultGroup: s.defaultGroup,
		Groups:       s.groups,
		Subjects:     s.subjects,
	}
	b, err := json.MarshalIndent(snap, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return NewStorage(filepath.Dir(path)).WriteBytes(filepath.Base(path), append(b, '\n'))
}

// LoadFile replaces store state from a JSON file created by SaveFile.
func (s *PermissionStore) LoadFile(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var snap permissionSnapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.defaultGroup = snap.DefaultGroup
	if snap.Groups != nil {
		s.groups = snap.Groups
	} else {
		s.groups = make(map[string]*permissionGroup)
	}
	if snap.Subjects != nil {
		s.subjects = snap.Subjects
	} else {
		s.subjects = make(map[string]*permissionSubject)
	}
	return nil
}

// PlayerUUIDString returns the canonical subject ID string for a player UUID.
func PlayerUUIDString(id uuid.UUID) string { return id.String() }

// Groups returns a sorted slice of all defined group names.
func (s *PermissionStore) Groups() []string {
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
