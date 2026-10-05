package authority

// WithOwnerContexts keeps trusted binding state locked until the action commits.
func (s *Store) WithOwnerContexts(paths []string, action func([]Context) error) error {
	return s.read(func(st *state) error {
		contexts := make([]Context, 0, len(paths))
		for _, path := range paths {
			b, err := bindingFor(st, path)
			if err != nil {
				return ErrDenied
			}
			sp, err := findSpace(st, b.SpaceID)
			if err != nil {
				return ErrDenied
			}
			contexts = append(contexts, Context{OwnerID: st.OwnerID, SpaceID: sp.ID, SpaceName: sp.Name, RepositoryID: b.RepositoryID, Checkout: b.Checkout})
		}
		return action(contexts)
	})
}
