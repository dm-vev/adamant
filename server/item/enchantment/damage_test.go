package enchantment

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
)

type plainDamage struct{}

func (plainDamage) ReducedByArmour() bool     { return true }
func (plainDamage) ReducedByResistance() bool { return true }
func (plainDamage) Fire() bool                { return false }
func (plainDamage) IgnoreTotem() bool         { return false }

type bypassingDamage struct{ plainDamage }

func (bypassingDamage) BypassesEnchantments() bool { return true }

// TestProtectionSkipsBypassingDamage checks LivingEntity.getDamageAfterMagicAbsorb:
// Protection reduces ordinary damage but not damage of the
// bypasses_enchantments tag, such as a warden's sonic boom.
func TestProtectionSkipsBypassingDamage(t *testing.T) {
	ench := []item.Enchantment{item.NewEnchantment(Protection, 4)}
	if f := ProtectionFactor(plainDamage{}, ench); f <= 0 {
		t.Fatalf("Protection IV reduces ordinary damage by %v, want more than nothing", f)
	}
	if f := ProtectionFactor(bypassingDamage{}, ench); f != 0 {
		t.Fatalf("Protection IV reduces damage that bypasses enchantments by %v, want 0", f)
	}
}
