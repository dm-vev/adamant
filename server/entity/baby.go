package entity

// Baby reports whether the mob is a baby (Java AgeableMob.isBaby or the baby
// flag of zombies and piglins). Loot predicates and the death drops use it.
func (m *HostileMob) Baby() bool      { return m.state().baby }
func (p *Piglin) Baby() bool          { return p.p().baby }
func (z *ZombifiedPiglin) Baby() bool { return z.z().baby }
func (h *Hoglin) Baby() bool          { return h.beast().age < 0 }
func (z *Zoglin) Baby() bool          { return z.beast().age < 0 }
func (s *Strider) Baby() bool         { return s.beast().age < 0 }
func (m *Mount) Baby() bool           { return m.b().ageTicks < 0 }
func (f *Fox) Baby() bool             { return f.b().age < 0 }
func (g *Goat) Baby() bool            { return g.behaviour().age < 0 }
func (p *Panda) Baby() bool           { return p.b().age < 0 }
func (p *PolarBear) Baby() bool       { return p.b().age < 0 }
func (r *Rabbit) Baby() bool          { return r.behaviour().age < 0 }
func (s *Sniffer) Baby() bool         { return s.behaviour().age < 0 }
func (a *Armadillo) Baby() bool       { return a.behaviour().age < 0 }
func (m *Mooshroom) Baby() bool       { return m.behaviour().age < 0 }
func (w *Wolf) Baby() bool            { return w.behaviour().ageTicks < 0 }
func (s *SulfurCube) Baby() bool      { return s.b().age < 0 }
func (p *Pig) Baby() bool             { return p.behaviour().Baby() }
func (c *Cat) Baby() bool             { return c.b().Baby() }
