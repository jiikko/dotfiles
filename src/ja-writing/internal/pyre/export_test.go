package pyre

// Expand は Match.expand (置換の書式を一致で埋める)。
func (m *Match) Expand(template string) (string, error) {
	t, err := m.re.ParseTemplate(template)
	if err != nil {
		return "", err
	}
	return t.expand(m), nil
}
